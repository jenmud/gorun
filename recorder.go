package gorun

// Recorder keeps a run's events, for example as history in a database. Set Config.Recorder to
// use one. The history package's Store is the built-in SQLite implementation, the one the gorun
// CLI and web UI read; implement Recorder yourself to keep runs anywhere else.
//
// Run calls Record with every event, right after Config.OnEvent, and waits for it to return, so
// keep it quick. It is called from one goroutine per server (and per task when a server runs tasks
// in parallel), so it must be safe for concurrent use. A run's events come in this order:
//
//	ProcessStartedEvent                  once, first
//	PlanEvent                            once per server, all before any task starts
//	ServerErrorEvent                     instead of tasks, for a server that can't run
//	TaskEvent (TaskRunning)              when a task starts, and again before each retry
//	TaskEvent (done, failed or skipped)  once per task, when it finishes
//	ProcessFinishedEvent                 once, last, even when tasks fail or the run is cancelled
//
// GorunUUID identifies the run on every event; GorunUUID, Server and Task together identify a
// task. json.Marshal(event) gives the JSON gorun publishes on NATS. docs/history.md describes
// every field.
type Recorder interface {
	Record(event any)
}

// RecorderFunc lets a plain function be a Recorder.
type RecorderFunc func(event any)

// Record calls f(event).
func (f RecorderFunc) Record(event any) { f(event) }
