package gorun

// Recorder keeps a run's events, for example as history in a database. Set Config.Recorder to
// use one: Record is called with every event, right after Config.OnEvent, from one goroutine per
// server, so it must be safe for concurrent use. The history package's Store is a Recorder that
// saves to the database the gorun CLI and web UI read.
type Recorder interface {
	Record(event any)
}

// RecorderFunc lets a plain function be a Recorder.
type RecorderFunc func(event any)

// Record calls f(event).
func (f RecorderFunc) Record(event any) { f(event) }
