package ui

import "strconv"

type TaskInfo struct {
	Name     string
	State    int
	Duration int64
	ExitCode int
	Stderr   string
}

type ServerInfo struct {
	Name    string
	Address string
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

func (s *DashboardState) Apply(ev Event) {
	uuid := ev.GorunUUID
	if uuid == "" {
		uuid = "unknown"
	}
	p := s.process(uuid)

	switch ev.Type {
	case "process_started":
		p.Title = ev.Title
		p.StartedAt = ev.StartedAt
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
		for _, name := range ev.Tasks {
			if _, ok := srv.Tasks[name]; !ok {
				srv.Tasks[name] = &TaskInfo{Name: name}
				srv.Order = append(srv.Order, name)
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
		t.Duration = ev.Duration
		t.ExitCode = ev.ExitCode
		t.Stderr = ev.Stderr
	case "process_finished":
		if ev.Success {
			// successful runs are removed from the dashboard
			delete(s.Processes, uuid)
			for i, u := range s.ProcessOrder {
				if u == uuid {
					s.ProcessOrder = append(s.ProcessOrder[:i], s.ProcessOrder[i+1:]...)
					break
				}
			}
			return
		}
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
		for _, t := range p.Servers[name].Tasks {
			if t.State == 3 {
				return true
			}
		}
	}
	return false
}
