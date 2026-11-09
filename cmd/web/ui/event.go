package ui

// TaskMeta carries the documentation attached to a task in a plan event.
type TaskMeta struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Details     string `json:"details,omitempty"`
	Rerun       string `json:"rerun,omitempty"`
}

type Event struct {
	Type       string     `json:"type"`
	GorunUUID  string     `json:"gorun_uuid"`
	Title      string     `json:"title"`
	Servers    []string   `json:"servers"`
	StartedAt  string     `json:"started_at"`
	Hostname   string     `json:"hostname"`
	PID        int        `json:"pid"`
	Success    bool       `json:"success"`
	Duration   int64      `json:"duration"`
	TotalTasks int        `json:"total_tasks"`
	Failed     int        `json:"failed"`
	Skipped    int        `json:"skipped"`
	FinishedAt string     `json:"finished_at"`
	Server     string     `json:"server"`
	Address    string     `json:"address"`
	Tasks      []string   `json:"tasks"`
	TaskMeta   []TaskMeta `json:"task_meta,omitempty"`
	Task       string     `json:"task"`
	State      int        `json:"state"`
	Stdout     string     `json:"stdout"`
	Stderr     string     `json:"stderr"`
	Command    string     `json:"command"`
	ExitCode   int        `json:"exit_code"`
	Attempt    int        `json:"attempt,omitempty"`
	Attempts   int        `json:"attempts,omitempty"`
	Error      string     `json:"error,omitempty"`
	Schedule   string     `json:"schedule,omitempty"` // the schedule that started the run

	// copy_progress
	File           string  `json:"file,omitempty"`
	Bytes          int64   `json:"bytes,omitempty"`
	Total          int64   `json:"total,omitempty"`
	Percent        float64 `json:"percent,omitempty"`
	BytesPerSecond float64 `json:"bytes_per_second,omitempty"`
	Elapsed        int64   `json:"elapsed,omitempty"`
	Done           bool    `json:"done,omitempty"`
}
