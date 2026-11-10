package gorun

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/crypto/ssh"

	"github.com/jenmud/gorun/internal/nats"
)

var (
	tracer = otel.Tracer("gorun")
	meter  = otel.Meter("gorun")

	taskExecutions, _ = meter.Int64Counter("gorun.task.executions",
		metric.WithDescription("Number of task executions"), metric.WithUnit("{execution}"))
	taskDuration, _ = meter.Float64Histogram("gorun.task.duration",
		metric.WithDescription("Task execution duration"), metric.WithUnit("s"))
	taskSkipped, _ = meter.Int64Counter("gorun.task.skipped",
		metric.WithDescription("Number of tasks skipped due to a failed dependency"), metric.WithUnit("{task}"))
)

// Run runs the given servers' tasks, or every server's when none are given. Servers run at the
// same time, up to MaxParallel. It returns an error when any task failed or no server had a
// task to run (ErrNoTasks). Progress is reported as events; see Config.OnEvent.
func (c *Config) Run(ctx context.Context, server ...Server) error {
	if c.MaxParallel < 0 {
		return errors.New("max_parallel must not be negative")
	}
	if c.Force && c.CheckOnly {
		return errors.New("force and check-only can't be used together")
	}
	hostname, _ := os.Hostname()
	if c.GorunUUID == "" {
		c.GorunUUID = fmt.Sprintf("%s-%d-%d", hostname, os.Getpid(), time.Now().UnixNano())
	}
	started := time.Now()

	servers, err := c.plan(server)
	if err != nil {
		return err
	}
	names := make([]string, len(servers))
	total := 0
	for i, s := range servers {
		names[i] = s.Name
		total += len(s.Tasks)
	}

	c.emit(ProcessStartedEvent{
		Type:      "process_started",
		GorunUUID: c.GorunUUID,
		Title:     c.Title,
		Servers:   names,
		StartedAt: started,
		Hostname:  hostname,
		PID:       os.Getpid(),
		EnvVars:   c.EnvVars,
		Vars:      c.Vars,
		Includes:  c.Includes,
		Schedule:  c.Schedule,
		CheckOnly: c.CheckOnly,
	})
	// every server's plan is announced before any task starts
	for _, s := range servers {
		plan := PlanEvent{Type: "plan", GorunUUID: c.GorunUUID, Server: s.Name, Address: s.Address(), Tasks: []string{}, EnvVars: s.EnvVars, Vars: s.Vars}
		for _, t := range s.Tasks {
			plan.Tasks = append(plan.Tasks, t.Name)
			plan.TaskMeta = append(plan.TaskMeta, TaskMeta{Name: t.Name, Description: t.Description, Details: t.Details, Rerun: t.Rerun,
				Check: t.Check, CheckCopies: t.CheckCopies, Verify: t.Verify})
		}
		c.emit(plan)
	}

	r := &run{cfg: c}
	limit := len(servers)
	if c.MaxParallel > 0 {
		limit = min(limit, c.MaxParallel)
	}
	slots := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for _, s := range servers {
		wg.Go(func() {
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
				r.runServer(ctx, s)
			case <-ctx.Done():
			}
		})
	}
	wg.Wait()

	failed := int(r.failed.Load())
	c.emit(ProcessFinishedEvent{
		Type:       "process_finished",
		GorunUUID:  c.GorunUUID,
		Success:    failed == 0,
		Duration:   time.Since(started),
		TotalTasks: total,
		Failed:     failed,
		Skipped:    int(r.skipped.Load()),
		Unchanged:  int(r.unchanged.Load()),
		WouldRun:   int(r.wouldRun.Load()),
		FinishedAt: time.Now(),
	})
	if failed > 0 {
		return errors.New("one or more tasks failed")
	}
	return nil
}

// plan returns the servers that have something to run, sorted by name, each with its tasks in
// run order and ready to execute: the task filter applied, envvars merged and log levels set.
func (c *Config) plan(servers []Server) ([]Server, error) {
	if len(servers) == 0 {
		servers = c.Servers
	}
	allowed := c.selectedTaskSet()

	planned := map[string]Server{} // by name: a server given twice runs once
	for _, s := range servers {
		s, err := c.ServerPipeline(s)
		if err != nil {
			return nil, fmt.Errorf("error building servers task execution pipeline: %w", err)
		}
		if allowed != nil {
			s.Tasks = slices.DeleteFunc(s.Tasks, func(t Task) bool { return !allowed[t.Name] })
		}
		// a server left with no tasks of its kind has nothing to do, so it isn't connected to
		if len(s.Tasks) == 0 {
			delete(planned, s.Name)
			continue
		}

		// Global vars and envvars are the lowest layer of the server's own, so templates,
		// events and dumps see them alongside the server's.
		s.Vars = withDefaults(s.Vars, c.Vars)
		s.EnvVars = withDefaults(s.EnvVars, c.EnvVars)
		s.LogLevel = cmp.Or(s.LogLevel, c.LogLevel)
		for i := range s.Tasks {
			t := &s.Tasks[i]
			t.LogLevel = cmp.Or(t.LogLevel, s.LogLevel)
			// A task's environment is the global envvars, then the server's (with its
			// groups'), then its own, later ones winning. Names are upper-cased and values
			// expand $VAR from gorun's environment.
			env := map[string]string{}
			for _, layer := range []map[string]string{c.EnvVars, s.EnvVars, t.EnvVars} {
				for k, v := range layer {
					env[strings.ToUpper(k)] = ExpandEnv(v)
				}
			}
			t.EnvVars = env
		}
		planned[s.Name] = s
	}
	if len(planned) == 0 {
		return nil, c.noTasksError()
	}

	out := make([]Server, 0, len(planned))
	for _, s := range planned {
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b Server) int { return cmp.Compare(a.Name, b.Name) })
	return out, nil
}

// withDefaults fills the keys m doesn't set from defaults, creating m if needed.
func withDefaults[V any](m, defaults map[string]V) map[string]V {
	if len(defaults) > 0 && m == nil {
		m = map[string]V{}
	}
	for k, v := range defaults {
		if _, ok := m[k]; !ok {
			m[k] = v
		}
	}
	return m
}

// run is the state shared by every server of one Run.
type run struct {
	cfg                                  *Config
	failed, skipped, unchanged, wouldRun atomic.Int64 // tasks, counted for ProcessFinishedEvent
}

// runServer connects to the server and runs its tasks: one at a time by default, or up to
// max_task_parallel at once, each after the tasks it depends on.
func (r *run) runServer(ctx context.Context, server Server) {
	c := r.cfg
	ctx, span := tracer.Start(ctx, "server.run")
	defer span.End()
	span.SetAttributes(attribute.String("server.name", server.Name), attribute.String("server.address", server.Hostname))
	logger := NewLogger(server.LogLevel, logOutput, false).With(
		slog.String("gorun_uuid", c.GorunUUID),
		slog.Group("server", slog.String("name", server.Name), slog.String("address", server.Hostname)),
	)
	// fail reports a server that can't run at all: every one of its tasks counts as failed
	fail := func(err error) {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		logger.Error("error with running tasks", slog.String("reason", err.Error()))
		r.failed.Add(int64(len(server.Tasks)))
		c.emit(ServerErrorEvent{Type: "server_error", GorunUUID: c.GorunUUID, Server: server.Name, Err: err})
	}

	var client *ssh.Client
	if !server.IsLocal() {
		logger.Debug("connecting to server via SSH")
		var err error
		if client, err = NewSSHClient(ctx, server); err != nil {
			fail(fmt.Errorf("error creating SSH connection: %w", err))
			return
		}
		logger.Debug("connected to server via SSH")
		defer func() { _ = closeSSHClient(client) }()
	}

	tasks, err := NewTaskExec(client, server.Tasks...)
	if err != nil {
		fail(fmt.Errorf("error loading tasks: %w", err))
		return
	}

	limit := cmp.Or(server.MaxTaskParallel, c.MaxTaskParallel, 1)
	if limit < 0 || limit > len(tasks) { // -1 means as many as possible
		limit = len(tasks)
	}
	sr := &serverRun{
		run:     r,
		server:  &server,
		logger:  logger,
		slots:   make(chan struct{}, limit),
		done:    make(map[string]chan struct{}, len(tasks)),
		results: make(map[string]error, len(tasks)),
	}
	for _, t := range tasks {
		t.target = &server
		t.force, t.checkOnly = c.Force, c.CheckOnly
		t.logger = t.logger.With(slog.String("gorun_uuid", c.GorunUUID))
		sr.done[t.task.Name] = make(chan struct{})
	}

	if limit == 1 {
		for _, t := range tasks {
			sr.runTask(ctx, t)
		}
		return
	}
	var wg sync.WaitGroup
	for _, t := range tasks {
		wg.Go(func() { sr.runTask(ctx, t) })
	}
	wg.Wait()
}

// serverRun is the state shared by the tasks of one server in a run.
type serverRun struct {
	*run
	server *Server
	logger *slog.Logger
	slots  chan struct{} // limits how many tasks run at once

	// done has a channel per task, closed when the task finishes (done, failed or skipped),
	// so the tasks that depend on it can wait. results then holds how it went.
	done    map[string]chan struct{}
	mu      sync.Mutex
	results map[string]error
}

// runTask runs one task once the tasks it depends on have finished, retrying it as its retries
// allow. It is skipped when one of those tasks failed or was skipped, or the run is cancelled.
func (sr *serverRun) runTask(ctx context.Context, task *TaskExec) {
	c, server, name := sr.cfg, sr.server, task.task.Name
	logger := sr.logger.With(slog.String("task", name))
	event := func(state TaskState) TaskEvent {
		// envvars as written in the Gorunfile, not expanded, so secrets stay out of events
		env := make(map[string]string, len(server.EnvVars))
		for _, layer := range []map[string]string{server.EnvVars, c.Tasks[name].EnvVars} {
			for k, v := range layer {
				env[strings.ToUpper(k)] = v
			}
		}
		return TaskEvent{Type: "task", GorunUUID: c.GorunUUID, Server: server.Name, Task: name, State: state, EnvVars: env, Vars: task.task.Vars}
	}
	finish := func(err error) {
		sr.mu.Lock()
		sr.results[name] = err
		sr.mu.Unlock()
		close(sr.done[name])
	}
	skip := func(err error) {
		ev := event(TaskSkipped)
		ev.Command, _ = task.task.CommandFor(server)
		ev.Stderr, ev.Err = err.Error(), err
		c.emit(ev)
		sr.skipped.Add(1)
		taskSkipped.Add(ctx, 1, metric.WithAttributes(attribute.String("task.name", name), attribute.String("server.name", server.Name)))
		finish(err)
	}
	cancelled := func() {
		logger.Warn("task skipped due to cancellation", slog.String("reason", ctx.Err().Error()))
		skip(ctx.Err())
	}

	for _, dep := range task.task.DependsOn {
		if ch, ok := sr.done[dep]; ok {
			select {
			case <-ch:
			case <-ctx.Done():
			}
		}
	}
	if ctx.Err() != nil {
		cancelled()
		return
	}
	sr.mu.Lock()
	failedDep := ""
	for _, dep := range task.task.DependsOn {
		if sr.results[dep] != nil {
			failedDep = dep
			break
		}
	}
	sr.mu.Unlock()
	if failedDep != "" {
		err := fmt.Errorf("dependent task (%s) failed, aborting task (%s)", failedDep, name)
		logger.Error("error with running tasks", slog.String("reason", err.Error()))
		skip(err)
		return
	}

	select {
	case sr.slots <- struct{}{}:
		defer func() { <-sr.slots }()
	case <-ctx.Done():
		cancelled()
		return
	}

	task.progress = func(p CopyProgress) {
		c.emit(CopyProgressEvent{
			Type:           "copy_progress",
			GorunUUID:      c.GorunUUID,
			Server:         server.Name,
			Task:           name,
			File:           p.File,
			Bytes:          p.Bytes,
			Total:          p.Total,
			Percent:        p.Percent(),
			BytesPerSecond: p.BytesPerSecond(),
			Elapsed:        p.Elapsed,
			Done:           p.Done,
		})
	}

	// events carry attempt numbers only when the task has retries
	attempts := task.task.Attempts()
	numbered := func(ev *TaskEvent, attempt int) {
		if attempts > 1 {
			ev.Attempt, ev.Attempts = attempt, attempts
		}
	}
	started := time.Now()
	var err error
	attempt := 1
	for ; ; attempt++ {
		// a running event is sent as each phase of the attempt starts: check, run and verify
		var previous TaskEvent
		if attempt > 1 {
			// carry the previous attempt's failure, to explain why the task is running again
			previous.ExitCode, previous.Stderr, previous.Err = task.exitCode, string(task.stderr), err
			task.resetForRetry()
		}
		first := true
		task.onPhase = func(phase Phase) {
			running := event(TaskRunning)
			running.Phase = phase
			switch phase {
			case PhaseCheck:
				running.Command = task.task.Check
				if task.task.CheckCopies {
					running.Command = strings.TrimSuffix("compare copies (md5); "+task.task.Check, "; ")
				}
			case PhaseVerify:
				running.Command = task.task.Verify
			default:
				running.Command, _ = task.task.CommandFor(server)
			}
			if phase != PhaseCheck {
				running.Check = task.check // the check has run: views can show how it went
			}
			if phase == PhaseVerify {
				running.Stdout, running.Stderr = string(task.stdout), string(task.stderr) // and so has the command
			}
			numbered(&running, attempt)
			if first {
				running.ExitCode, running.Stderr, running.Err = previous.ExitCode, previous.Stderr, previous.Err
				first = false
			}
			c.emit(running)
		}

		err = task.Execute(ctx)
		if err == nil || task.stopped || ctx.Err() != nil || attempt == attempts {
			break
		}
		retry := []any{slog.Int("attempt", attempt), slog.Int("attempts", attempts), slog.String("reason", err.Error())}
		if !task.task.CanRetry() {
			task.appendStderr("not retried: task is marked unsafe to rerun (set retry_unsafe = true to override)")
			logger.Warn("task failed; not retrying because it is marked unsafe to rerun", retry...)
			break
		}
		logger.Warn("task failed, retrying", append(retry, slog.Duration("delay", task.task.RetryDelayDuration()))...)
		if !task.waitForRetry(ctx) {
			break
		}
	}

	ev := event(TaskDone)
	numbered(&ev, attempt)
	switch {
	case err != nil:
		sr.failed.Add(1)
		ev.State, ev.Err = TaskFailed, err
	case task.unchanged:
		sr.unchanged.Add(1)
		ev.State = TaskUnchanged
	case task.wouldRun:
		sr.wouldRun.Add(1)
		ev.State = TaskWouldRun
	}
	ev.Phase, ev.Check, ev.Verify = task.phase, task.check, task.verify
	if ev.Command = task.command; ev.Command == "" {
		ev.Command, _ = task.task.CommandFor(server)
	}
	ev.Stdout, ev.Stderr, ev.ExitCode = string(task.stdout), string(task.stderr), task.exitCode
	ev.Duration = time.Since(started)
	c.emit(ev)
	finish(err)
}

// emit sends an event to the OnEvent callback and the Recorder, if they are set, and publishes
// it to NATS when a publisher is available.
func (c *Config) emit(event any) {
	if c.OnEvent != nil {
		c.OnEvent(event)
	}
	if c.Recorder != nil {
		c.Recorder.Record(event)
	}
	if publisher != nil {
		if data, err := json.Marshal(event); err == nil {
			_ = publisher.Publish(nats.Topic(), data)
			_ = publisher.Flush()
		}
	}
}
