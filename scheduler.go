package gorun

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"
)

// ScheduleEventKind says what the scheduler did about one occurrence of a schedule.
type ScheduleEventKind string

const (
	ScheduleFired   ScheduleEventKind = "fired"   // the target started
	ScheduleFailed  ScheduleEventKind = "failed"  // the target's run returned an error
	ScheduleSkipped ScheduleEventKind = "skipped" // not run: see Reason
	ScheduleMissed  ScheduleEventKind = "missed"  // fell due while paused; held for Resume
)

// ScheduleEvent reports one scheduler decision. Skipped and missed occurrences are reported
// too, so history can show them.
type ScheduleEvent struct {
	Schedule string
	Kind     ScheduleEventKind
	At       time.Time // the occurrence it concerns, in UTC
	Reason   string    // why it was skipped or missed, or the error for a failed run
}

// ScheduleStatus is the scheduler's view of one schedule.
type ScheduleStatus struct {
	Name     string
	Target   string
	Paused   bool
	Running  bool      // a run started by this schedule is still going
	SkipNext bool      // the next occurrence will be skipped
	Missed   int       // occurrences that fell due while paused, not yet resumed
	Next     time.Time // the next occurrence in UTC; zero when none is left
}

// Scheduler fires a Config's schedules as they come due. It does nothing until Start, so a
// plain run never fires them; the web server is the long-running host that starts one.
// State lives in memory: pauses, skips and missed runs are lost when the process exits.
type Scheduler struct {
	cfg     *Config
	onEvent func(ScheduleEvent)
	now     func() time.Time

	mu     sync.Mutex
	states map[string]*scheduleState
	ctx    context.Context // set by Start, the parent of every run
	cancel context.CancelFunc
	done   chan struct{}
	wake   chan struct{} // nudges the loop after a control call changes what is due
	runs   sync.WaitGroup
}

type scheduleState struct {
	sched    Schedule
	next     time.Time
	hasNext  bool
	paused   bool
	skipNext bool
	missed   int
	running  bool
}

// NewScheduler returns a scheduler for the config's schedules. onEvent, when set, receives
// every ScheduleEvent; it may be called from the scheduler's goroutines.
func NewScheduler(c *Config, onEvent func(ScheduleEvent)) *Scheduler {
	s := &Scheduler{
		cfg:     c,
		onEvent: onEvent,
		now:     time.Now,
		states:  map[string]*scheduleState{},
		wake:    make(chan struct{}, 1),
	}
	for _, sched := range c.Schedules {
		s.states[sched.Name] = &scheduleState{sched: sched}
	}
	// the config is validated on load, so priming only fails for one built by hand; Start
	// reports that error
	_ = s.prime(s.now())
	return s
}

// Start begins firing schedules until Stop or until ctx is done. Runs in progress are
// cancelled by the same context.
func (s *Scheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return errors.New("scheduler already started")
	}
	if err := s.prime(s.now()); err != nil {
		s.mu.Unlock()
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	s.ctx, s.cancel = ctx, cancel
	s.done = make(chan struct{})
	done := s.done
	s.mu.Unlock()

	go s.loop(ctx, done)
	return nil
}

// Stop stops the scheduler, cancels runs in progress and waits for them to return.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	cancel, done := s.cancel, s.done
	s.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
	s.runs.Wait()

	s.mu.Lock()
	s.cancel, s.done, s.ctx = nil, nil, nil
	s.mu.Unlock()
}

// Pause holds the schedule: occurrences that fall due are reported as missed until Resume.
func (s *Scheduler) Pause(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.state(name)
	if err != nil {
		return err
	}
	st.paused = true
	return nil
}

// Resume lifts a pause. The schedule's missed setting then decides the held occurrences:
// "run" starts the target once to catch up, "skip" reports them and drops them.
func (s *Scheduler) Resume(name string) error {
	s.mu.Lock()
	st, err := s.state(name)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	if !st.paused {
		s.mu.Unlock()
		return nil
	}
	st.paused = false
	missed := st.missed
	st.missed = 0

	var events []ScheduleEvent
	switch {
	case missed == 0:
	case st.sched.Missed != MissedRun:
		events = append(events, ScheduleEvent{Schedule: name, Kind: ScheduleSkipped, At: s.now().UTC(),
			Reason: fmt.Sprintf("%d missed while paused, missed is skip", missed)})
	default:
		events = append(events, s.fireLocked(st, s.now().UTC(), fmt.Sprintf("catching up on %d missed while paused", missed))...)
	}
	s.mu.Unlock()

	s.emit(events)
	s.nudge()
	return nil
}

// Skip cancels the next occurrence of the schedule only. It is not carried forward.
func (s *Scheduler) Skip(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.state(name)
	if err != nil {
		return err
	}
	if !st.hasNext {
		return fmt.Errorf("schedule %q has no upcoming run to skip", name)
	}
	st.skipNext = true
	return nil
}

// Status reports every schedule, sorted by name.
func (s *Scheduler) Status() []ScheduleStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ScheduleStatus, 0, len(s.states))
	for _, name := range s.names() {
		st := s.states[name]
		var next time.Time
		if st.hasNext {
			next = st.next
		}
		out = append(out, ScheduleStatus{
			Name:     name,
			Target:   st.sched.Target,
			Paused:   st.paused,
			Running:  st.running,
			SkipNext: st.skipNext,
			Missed:   st.missed,
			Next:     next,
		})
	}
	return out
}

// loop checks the schedules at least once a minute, and sooner when one is due.
func (s *Scheduler) loop(ctx context.Context, done chan struct{}) {
	defer close(done)
	for {
		now := s.now()
		s.tick(ctx, now)

		wait := time.Minute
		if d, ok := s.untilNext(now); ok && d < wait {
			wait = d
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		case <-s.wake:
			timer.Stop()
		}
	}
}

// tick handles every occurrence due at or before now.
func (s *Scheduler) tick(ctx context.Context, now time.Time) {
	s.mu.Lock()
	var events []ScheduleEvent
	for _, name := range s.names() {
		st := s.states[name]
		if !st.hasNext || st.next.After(now) {
			continue
		}
		occ := st.next.UTC()
		next, ok, err := nextAfter(st.sched, now)
		st.next, st.hasNext = next, ok
		if err != nil {
			st.hasNext = false
			events = append(events, ScheduleEvent{Schedule: name, Kind: ScheduleFailed, At: occ, Reason: err.Error()})
			continue
		}

		switch {
		case st.paused:
			st.missed++
			events = append(events, ScheduleEvent{Schedule: name, Kind: ScheduleMissed, At: occ, Reason: "paused"})
		case st.skipNext:
			st.skipNext = false
			events = append(events, ScheduleEvent{Schedule: name, Kind: ScheduleSkipped, At: occ, Reason: "skip requested"})
		default:
			events = append(events, s.fireLocked(st, occ, "")...)
		}
	}
	s.mu.Unlock()
	s.emit(events)
}

// fireLocked starts the target for one occurrence, unless a run is still going. Callers
// hold s.mu. The events returned are emitted by the caller, outside the lock.
func (s *Scheduler) fireLocked(st *scheduleState, occ time.Time, note string) []ScheduleEvent {
	name := st.sched.Name
	if st.running {
		return []ScheduleEvent{{Schedule: name, Kind: ScheduleSkipped, At: occ, Reason: "previous run still going"}}
	}
	if s.ctx == nil {
		return []ScheduleEvent{{Schedule: name, Kind: ScheduleSkipped, At: occ, Reason: "scheduler not started"}}
	}
	st.running = true
	s.runs.Add(1)
	go s.run(s.ctx, st, occ, note)
	return nil
}

// run executes the target on every server, the same way -task does, and reports the outcome.
func (s *Scheduler) run(ctx context.Context, st *scheduleState, occ time.Time, note string) {
	defer s.runs.Done()
	name := st.sched.Name
	reason := note
	s.emit([]ScheduleEvent{{Schedule: name, Kind: ScheduleFired, At: occ, Reason: reason}})

	// As -task does: the target runs on every server, whatever its own task list says.
	c := *s.cfg
	names, err := c.ExpandTaskRefs(st.sched.Target)
	if err != nil {
		s.finish(st, occ, err)
		return
	}
	c.SelectedTasks = names
	c.Schedule = name
	c.Servers = slices.Clone(c.Servers)
	for i := range c.Servers {
		c.Servers[i].TaskNames = slices.Clone(names)
	}
	err = c.Run(ctx)
	s.finish(st, occ, err)
}

// finish clears the running flag and reports a failed run.
func (s *Scheduler) finish(st *scheduleState, occ time.Time, err error) {
	name := st.sched.Name

	s.mu.Lock()
	st.running = false
	s.mu.Unlock()
	if err != nil {
		s.emit([]ScheduleEvent{{Schedule: name, Kind: ScheduleFailed, At: occ, Reason: err.Error()}})
	}
}

// prime sets each schedule's next occurrence after now. Callers hold s.mu.
func (s *Scheduler) prime(now time.Time) error {
	for _, st := range s.states {
		next, ok, err := nextAfter(st.sched, now)
		if err != nil {
			return err
		}
		st.next, st.hasNext = next, ok
	}
	return nil
}

// untilNext returns how long until the earliest occurrence, if any is left.
func (s *Scheduler) untilNext(now time.Time) (time.Duration, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var earliest time.Time
	found := false
	for _, st := range s.states {
		if st.hasNext && (!found || st.next.Before(earliest)) {
			earliest, found = st.next, true
		}
	}
	return earliest.Sub(now), found
}

// nudge wakes the loop so it re-reads what is due.
func (s *Scheduler) nudge() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// state returns the named schedule's state. Callers hold s.mu.
func (s *Scheduler) state(name string) (*scheduleState, error) {
	st, ok := s.states[name]
	if !ok {
		return nil, fmt.Errorf("no schedule named %q", name)
	}
	return st, nil
}

// names returns the schedule names, sorted. Callers hold s.mu.
func (s *Scheduler) names() []string {
	return slices.Sorted(maps.Keys(s.states))
}

// emit reports events to the callback. It is called without s.mu held.
func (s *Scheduler) emit(events []ScheduleEvent) {
	if s.onEvent == nil {
		return
	}
	for _, ev := range events {
		s.onEvent(ev)
	}
}

// nextAfter returns the schedule's next occurrence after the given time, and whether one is left.
func nextAfter(sched Schedule, after time.Time) (time.Time, bool, error) {
	return sched.Next(after)
}
