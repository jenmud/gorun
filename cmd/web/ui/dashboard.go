package ui

import (
	"cmp"
	"slices"
	"strconv"

	"github.com/jenmud/gorun/internal/store"
)

type TaskInfo struct {
	Name        string
	Description string
	Details     string
	Rerun       string
	State       int
	Duration    int64
	ExitCode    int
	Attempt     int
	Attempts    int
	Command     string
	Stdout      string
	Stderr      string
	Error       string    // why it was skipped
	Copy        *CopyInfo // the file it is copying, while it copies one
}

// CopyInfo is how far a task's copy has got.
type CopyInfo struct {
	File           string
	Bytes          int64
	Total          int64
	Percent        float64
	BytesPerSecond float64
}

type ServerInfo struct {
	Name    string
	Address string
	Error   string // why the server couldn't run its tasks at all
	Order   []string
	Tasks   map[string]*TaskInfo
}

type ProcessState struct {
	UUID        string
	Title       string
	Servers     map[string]*ServerInfo
	ServerOrder []string
	Finished    bool
	Success     bool
	TotalTasks  int
	Failed      int
	Skipped     int
	Duration    int64
	StartedAt   string
	Schedule    string // the schedule that started the run, or empty
}

type DashboardState struct {
	Processes    map[string]*ProcessState
	ProcessOrder []string
}

func (s *DashboardState) process(uuid string) *ProcessState {
	if s.Processes == nil {
		s.Processes = make(map[string]*ProcessState)
	}
	if p, ok := s.Processes[uuid]; ok {
		return p
	}
	p := &ProcessState{UUID: uuid, Servers: make(map[string]*ServerInfo)}
	s.Processes[uuid] = p
	s.ProcessOrder = append(s.ProcessOrder, uuid)
	return p
}

// ClearFinished removes all finished (non-running) processes from the dashboard.
func (s *DashboardState) ClearFinished() {
	for _, uuid := range s.ProcessOrder {
		if p, ok := s.Processes[uuid]; ok && p.Finished {
			delete(s.Processes, uuid)
		}
	}
	filtered := s.ProcessOrder[:0]
	for _, uuid := range s.ProcessOrder {
		if _, ok := s.Processes[uuid]; ok {
			filtered = append(filtered, uuid)
		}
	}
	s.ProcessOrder = filtered
}

func (s *DashboardState) Apply(ev store.Event) {
	uuid := ev.GorunUUID
	if uuid == "" {
		uuid = "unknown"
	}
	p := s.process(uuid)

	switch ev.Type {
	case "process_started":
		p.Title = ev.Title
		p.StartedAt = ev.StartedAt
		p.Schedule = ev.Schedule
		p.Servers = make(map[string]*ServerInfo)
		p.ServerOrder = nil
		p.Finished = false
	case "plan":
		if p.Servers == nil {
			p.Servers = make(map[string]*ServerInfo)
		}
		if _, ok := p.Servers[ev.Server]; !ok {
			p.Servers[ev.Server] = &ServerInfo{Name: ev.Server, Address: ev.Address, Tasks: make(map[string]*TaskInfo)}
			p.ServerOrder = append(p.ServerOrder, ev.Server)
		}
		srv := p.Servers[ev.Server]
		srv.Address = ev.Address
		meta := make(map[string]store.TaskMeta, len(ev.TaskMeta))
		for _, m := range ev.TaskMeta {
			meta[m.Name] = m
		}
		for _, name := range ev.Tasks {
			t, ok := srv.Tasks[name]
			if !ok {
				t = &TaskInfo{Name: name}
				srv.Tasks[name] = t
				srv.Order = append(srv.Order, name)
			}
			if m, ok := meta[name]; ok {
				t.Description = m.Description
				t.Details = m.Details
				t.Rerun = m.Rerun
			}
		}
	case "task":
		if p.Servers == nil {
			p.Servers = make(map[string]*ServerInfo)
		}
		srv, ok := p.Servers[ev.Server]
		if !ok {
			srv = &ServerInfo{Name: ev.Server, Tasks: make(map[string]*TaskInfo)}
			p.Servers[ev.Server] = srv
			p.ServerOrder = append(p.ServerOrder, ev.Server)
		}
		t, ok := srv.Tasks[ev.Task]
		if !ok {
			t = &TaskInfo{Name: ev.Task}
			srv.Tasks[ev.Task] = t
			srv.Order = append(srv.Order, ev.Task)
		}
		t.State = ev.State
		t.Copy = nil
		t.Duration = ev.Duration
		t.ExitCode = ev.ExitCode
		t.Attempt = ev.Attempt
		t.Attempts = ev.Attempts
		t.Command = ev.Command
		t.Stdout = ev.Stdout
		t.Stderr = ev.Stderr
		t.Error = ev.Error
	case "copy_progress":
		srv, ok := p.Servers[ev.Server]
		if !ok {
			return
		}
		t, ok := srv.Tasks[ev.Task]
		if !ok || t.State != 1 {
			return
		}
		if ev.Done {
			t.Copy = nil
			return
		}
		t.Copy = &CopyInfo{File: ev.File, Bytes: ev.Bytes, Total: ev.Total, Percent: ev.Percent, BytesPerSecond: ev.BytesPerSecond}
	case "server_error":
		srv, ok := p.Servers[ev.Server]
		if !ok {
			srv = &ServerInfo{Name: ev.Server, Tasks: make(map[string]*TaskInfo)}
			p.Servers[ev.Server] = srv
			p.ServerOrder = append(p.ServerOrder, ev.Server)
		}
		srv.Error = ev.Error
		if srv.Error == "" {
			srv.Error = "the server failed before running its tasks (no reason was reported)"
		}
	case "process_finished":
		// finished runs stay until cleared, so their output can be viewed
		p.Finished = true
		p.Success = ev.Success
		p.TotalTasks = ev.TotalTasks
		p.Failed = ev.Failed
		p.Skipped = ev.Skipped
		p.Duration = ev.Duration
	}
}

func processDone(p *ProcessState) int {
	n := 0
	for _, name := range p.ServerOrder {
		for _, t := range p.Servers[name].Tasks {
			if t.State >= 2 {
				n++
			}
		}
	}
	return n
}

func processTotal(p *ProcessState) int {
	n := 0
	for _, name := range p.ServerOrder {
		n += len(p.Servers[name].Tasks)
	}
	return n
}

func serverProgress(srv *ServerInfo) int {
	total := len(srv.Tasks)
	if total == 0 {
		return 0
	}
	done := 0
	for _, t := range srv.Tasks {
		if t.State >= 2 {
			done++
		}
	}
	return done * 100 / total
}

func processProgress(p *ProcessState) int {
	total := 0
	done := 0
	for _, name := range p.ServerOrder {
		for _, t := range p.Servers[name].Tasks {
			total++
			if t.State >= 2 {
				done++
			}
		}
	}
	if total == 0 {
		return 0
	}
	return done * 100 / total
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

func serverBoxClass(failed bool) string {
	if failed {
		return "rounded-xl border border-red-900/50 bg-red-950/20 p-3"
	}
	return "rounded-xl border border-zinc-800 bg-zinc-950/40 p-3"
}

func serverHasFailed(srv *ServerInfo) bool {
	if srv.Error != "" {
		return true
	}
	for _, t := range srv.Tasks {
		if t.State == 3 {
			return true
		}
	}
	return false
}

func hasFinished(s DashboardState) bool {
	for _, uuid := range s.ProcessOrder {
		if p, ok := s.Processes[uuid]; ok && p.Finished {
			return true
		}
	}
	return false
}

func processHasFailed(p *ProcessState) bool {
	for _, name := range p.ServerOrder {
		if serverHasFailed(p.Servers[name]) {
			return true
		}
	}
	return false
}

// serversFailed counts the servers that couldn't run their tasks at all.
func serversFailed(p *ProcessState) int {
	n := 0
	for _, name := range p.ServerOrder {
		if p.Servers[name].Error != "" {
			n++
		}
	}
	return n
}

// tasksFailed counts the tasks that failed.
func tasksFailed(p *ProcessState) int {
	n := 0
	for _, name := range p.ServerOrder {
		for _, t := range p.Servers[name].Tasks {
			if t.State == 3 {
				n++
			}
		}
	}
	return n
}

// notRun reports whether a task will never run: its server failed, or the run ended first.
func notRun(p *ProcessState, srv *ServerInfo, t *TaskInfo) bool {
	return t.State < 2 && (srv.Error != "" || p.Finished)
}

// sortedTasks lists a server's tasks in plan order while it runs, and slowest first once the
// run is over, so the tasks worth looking at come first.
func sortedTasks(p *ProcessState, srv *ServerInfo) []*TaskInfo {
	out := make([]*TaskInfo, 0, len(srv.Order))
	for _, name := range srv.Order {
		out = append(out, srv.Tasks[name])
	}
	if p.Finished {
		slices.SortStableFunc(out, func(a, b *TaskInfo) int { return cmp.Compare(b.Duration, a.Duration) })
	}
	return out
}

// serverSummary is the one line a collapsed server shows.
func serverSummary(p *ProcessState, srv *ServerInfo) string {
	if srv.Error != "" {
		return "failed to connect · " + tasksNotRun(len(srv.Tasks))
	}
	done, failed := 0, 0
	for _, t := range srv.Tasks {
		if t.State >= 2 {
			done++
		}
		if t.State == 3 {
			failed++
		}
	}
	line := itoa(done) + "/" + itoa(len(srv.Tasks)) + " tasks"
	if c := serverCopy(srv); c != nil {
		line = "↑ " + copyName(c) + " " + copyPercent(c) + " · " + line
	}
	if failed > 0 {
		line += " · " + itoa(failed) + " failed"
	}
	return line
}

func serverNotRunNote(srv *ServerInfo) string { return notRunNote(len(srv.Tasks)) }

// taskIcon is the mark in front of a task: its state, or a dash when it never ran.
func taskIcon(p *ProcessState, srv *ServerInfo, t *TaskInfo) string {
	if notRun(p, srv, t) {
		return "–"
	}
	return stateIcon(t.State)
}

func taskIconClass(p *ProcessState, srv *ServerInfo, t *TaskInfo) string {
	if notRun(p, srv, t) {
		return "text-zinc-600"
	}
	return stateClass(t.State)
}

// taskStatus is the right-hand text of a task row: how long it took, or why it has no time.
func taskStatus(p *ProcessState, srv *ServerInfo, t *TaskInfo) string {
	if notRun(p, srv, t) {
		return "not run"
	}
	return taskTiming(t)
}

// taskHint explains a task's state in a sentence.
func taskHint(p *ProcessState, srv *ServerInfo, t *TaskInfo) string {
	switch {
	case srv.Error != "" && t.State < 2:
		return "Did not run: the server could not be used. See the error above."
	case notRun(p, srv, t):
		return "Did not run: the run ended before this task started."
	default:
		return hintFor(t.State)
	}
}

// tasksNotRun counts tasks that did not run, e.g. "1 task not run".
func tasksNotRun(n int) string {
	if n == 1 {
		return "1 task not run"
	}
	return itoa(n) + " tasks not run"
}

// errorTitle names what a task's error message explains.
func errorTitle(t *TaskInfo) string {
	if t.State == 4 {
		return "Why it was skipped"
	}
	return "Why it failed"
}

// serverCopy is a copy one of the server's tasks is making, if any, for its summary line.
func serverCopy(srv *ServerInfo) *CopyInfo {
	for _, name := range srv.Order {
		if t := srv.Tasks[name]; t != nil && t.Copy != nil {
			return t.Copy
		}
	}
	return nil
}
