// Package store is the SQLite database behind gorun's history: every run, its servers and its
// tasks. The public history package wraps it for library users.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	_ "modernc.org/sqlite"
)

// Task states, as stored. They match gorun.TaskState, which only ever adds states at the end.
const (
	StatePending = iota
	StateRunning
	StateDone
	StateFailed
	StateSkipped
	StateUnchanged
	StateWouldRun
)

// RunSummary is one run: when it started and finished, and how its tasks went.
type RunSummary struct {
	Servers    int // how many servers it ran on
	ID         string
	Title      string
	Hostname   string
	PID        int
	StartedAt  time.Time
	FinishedAt time.Time
	Finished   bool
	Success    bool
	TotalTasks int
	Failed     int
	Skipped    int
	Duration   time.Duration
	// Schedule is the schedule that started the run, or empty.
	Schedule string
	// CheckOnly is set for a check-only run (--check), which changed nothing.
	CheckOnly bool
	// Origin is who started the run, from where, and with what; nil for runs saved before
	// gorun recorded it.
	Origin *Origin
}

// TaskDetail is one task on one server in a run.
type TaskDetail struct {
	Server   string
	Name     string
	State    int
	Command  string
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
	EnvVars  map[string]string
	Vars     map[string]any
	// Check and Verify are the task's check and verify results, when they ran.
	Check  *Step
	Verify *Step
}

// Include is a git include a run read: the repository, the ref asked for and the commit used.
type Include struct {
	Source string `json:"source"`
	Ref    string `json:"ref"`
	Commit string `json:"commit"`
}

// RunDetail is a run with its servers and tasks. Tasks carry no command or output here:
// GetTask loads those for one task.
type RunDetail struct {
	RunSummary

	// Includes are the git includes the run's Gorunfile read, with the commit each one used.
	Includes []Include

	Servers []string
	Tasks   []TaskDetail

	// ServerErrors holds, by server name, why a server couldn't run its tasks at
	// all (e.g. its SSH connection failed). Such servers have no tasks recorded.
	ServerErrors map[string]string

	// The address, envvars and vars each server's plan event carried, by server name, and
	// the run's own envvars and vars.
	ServerAddress map[string]string
	ServerEnvVars map[string]map[string]string
	ServerVars    map[string]map[string]any
	RunEnvVars    map[string]string
	RunVars       map[string]any
}

// Store is an open history database.
type Store struct {
	db *sql.DB
}

// Open opens the database at path, creating its tables if needed.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	_, _ = db.Exec("PRAGMA busy_timeout=5000;")
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// schema is the original tables. Columns added since are in addedColumns, so databases made
// by older versions gain them on open.
var schema = []string{
	`CREATE TABLE IF NOT EXISTS runs (
		id TEXT PRIMARY KEY,
		title TEXT,
		hostname TEXT,
		pid INTEGER,
		started_at TEXT,
		finished_at TEXT,
		success INTEGER,
		total_tasks INTEGER,
		failed INTEGER,
		skipped INTEGER,
		duration_ns INTEGER
	)`,
	`CREATE TABLE IF NOT EXISTS servers (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		run_id TEXT,
		name TEXT,
		address TEXT,
		UNIQUE(run_id, name)
	)`,
	`CREATE TABLE IF NOT EXISTS tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		run_id TEXT,
		server TEXT,
		name TEXT,
		state INTEGER,
		command TEXT,
		stdout TEXT,
		stderr TEXT,
		exit_code INTEGER,
		duration_ns INTEGER,
		UNIQUE(run_id, server, name)
	)`,
}

// addedColumns are TEXT columns added after the first release, as {table, column}. To store
// something new, add its column here.
var addedColumns = [][2]string{
	{"tasks", "command"},
	{"servers", "error"},
	{"servers", "envvars"},
	{"servers", "vars"},
	{"tasks", "envvars"},
	{"tasks", "vars"},
	{"runs", "envvars"},
	{"runs", "vars"},
	{"runs", "includes"},
	{"runs", "schedule"},
	{"tasks", "steps"},
	{"runs", "check_only"},
	{"runs", "origin"},
}

// migrate creates the tables and adds the columns newer versions need.
func (s *Store) migrate() error {
	_, _ = s.db.Exec("PRAGMA journal_mode=WAL;")
	for _, stmt := range schema {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	for _, c := range addedColumns {
		// fails when the column is already there, which is fine
		_, _ = s.db.Exec("ALTER TABLE " + c[0] + " ADD COLUMN " + c[1] + " TEXT")
	}
	return nil
}

// exec runs a statement that returns no rows.
func (s *Store) exec(ctx context.Context, query string, args ...any) error {
	_, err := s.db.ExecContext(ctx, query, args...)
	return err
}

// toJSON encodes v for a TEXT column.
func toJSON(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}

// fromJSON decodes a TEXT column into dst, leaving dst alone when the column is empty or bad.
func fromJSON(col sql.NullString, dst any) {
	if col.String != "" {
		_ = json.Unmarshal([]byte(col.String), dst)
	}
}

// CreateRun records the start of a run.
func (s *Store) CreateRun(ctx context.Context, id, title, hostname string, pid int, startedAt time.Time) error {
	return s.exec(ctx, `INSERT OR REPLACE INTO runs (id,title,hostname,pid,started_at) VALUES (?,?,?,?,?)`,
		id, title, hostname, pid, startedAt.UTC().Format(time.RFC3339Nano))
}

// FinishRun records how a run ended.
func (s *Store) FinishRun(ctx context.Context, runID string, success bool, total, failed, skipped int, duration time.Duration, finishedAt time.Time) error {
	return s.exec(ctx, `UPDATE runs SET finished_at=?, success=?, total_tasks=?, failed=?, skipped=?, duration_ns=? WHERE id=?`,
		finishedAt.UTC().Format(time.RFC3339Nano), success, total, failed, skipped, int64(duration), runID)
}

// SetRunEnvVars stores a run's global envvars and vars.
func (s *Store) SetRunEnvVars(ctx context.Context, runID string, envvars map[string]string, vars map[string]any) error {
	return s.exec(ctx, `UPDATE runs SET envvars=?, vars=? WHERE id=?`, toJSON(envvars), toJSON(vars), runID)
}

// SetRunSchedule records the schedule that started a run.
func (s *Store) SetRunSchedule(ctx context.Context, runID, schedule string) error {
	return s.exec(ctx, `UPDATE runs SET schedule=? WHERE id=?`, schedule, runID)
}

// SetRunCheckOnly marks a run as check-only: only checks ran, and nothing changed.
func (s *Store) SetRunCheckOnly(ctx context.Context, runID string) error {
	return s.exec(ctx, `UPDATE runs SET check_only='1' WHERE id=?`, runID)
}

// SetRunIncludes stores the git includes a run read, with their commits.
func (s *Store) SetRunIncludes(ctx context.Context, runID string, includes []Include) error {
	return s.exec(ctx, `UPDATE runs SET includes=? WHERE id=?`, toJSON(includes), runID)
}

// AddServer records a server of a run, replacing what was saved for it.
func (s *Store) AddServer(ctx context.Context, runID, name, address string) error {
	return s.exec(ctx, `INSERT OR REPLACE INTO servers (run_id,name,address) VALUES (?,?,?)`, runID, name, address)
}

// SetServerEnvVars stores a server's merged envvars and vars.
func (s *Store) SetServerEnvVars(ctx context.Context, runID, name string, envvars map[string]string, vars map[string]any) error {
	return s.exec(ctx, `UPDATE servers SET envvars=?, vars=? WHERE run_id=? AND name=?`, toJSON(envvars), toJSON(vars), runID, name)
}

// SetServerError records why a server couldn't run its tasks at all.
func (s *Store) SetServerError(ctx context.Context, runID, name, message string) error {
	return s.exec(ctx, `INSERT INTO servers (run_id,name,address,error) VALUES (?,?,'',?)
		ON CONFLICT(run_id,name) DO UPDATE SET error=excluded.error`, runID, name, message)
}

// PlanTask records a task as pending, unless it already has a row, so a run shows the tasks a
// server was meant to run even when it never got to them.
func (s *Store) PlanTask(ctx context.Context, runID, server, name string) error {
	return s.exec(ctx, `INSERT OR IGNORE INTO tasks (run_id,server,name,state,command,stdout,stderr,exit_code,duration_ns)
		VALUES (?,?,?,0,'','','',0,0)`, runID, server, name)
}

// UpsertTask saves a task's latest state and output.
func (s *Store) UpsertTask(ctx context.Context, runID, server, name string, state int, command, stdout, stderr string, exitCode int, duration time.Duration) error {
	return s.exec(ctx, `INSERT INTO tasks (run_id,server,name,state,command,stdout,stderr,exit_code,duration_ns)
		VALUES (?,?,?,?,?,?,?,?,?)
		ON CONFLICT(run_id,server,name) DO UPDATE SET
			state=excluded.state, command=excluded.command, stdout=excluded.stdout, stderr=excluded.stderr,
			exit_code=excluded.exit_code, duration_ns=excluded.duration_ns`,
		runID, server, name, state, command, stdout, stderr, exitCode, int64(duration))
}

// SetTaskSteps stores a task's check and verify results.
func (s *Store) SetTaskSteps(ctx context.Context, runID, server, name string, check, verify *Step) error {
	return s.exec(ctx, `UPDATE tasks SET steps=? WHERE run_id=? AND server=? AND name=?`,
		toJSON(steps{check, verify}), runID, server, name)
}

// steps is how a task's check and verify results are kept in its steps column.
type steps struct {
	Check  *Step `json:"check,omitempty"`
	Verify *Step `json:"verify,omitempty"`
}

// SetTaskEnvVars stores a task's merged envvars and vars.
func (s *Store) SetTaskEnvVars(ctx context.Context, runID, server, name string, envvars map[string]string, vars map[string]any) error {
	return s.exec(ctx, `UPDATE tasks SET envvars=?, vars=? WHERE run_id=? AND server=? AND name=?`,
		toJSON(envvars), toJSON(vars), runID, server, name)
}

// SetRunOrigin stores who started a run, from where, and with what.
func (s *Store) SetRunOrigin(ctx context.Context, id string, o *Origin) error {
	return s.exec(ctx, `UPDATE runs SET origin=? WHERE id=?`, toJSON(o), id)
}

// runColumns are the columns scanRun reads, in order.
const runColumns = `id,title,hostname,pid,started_at,finished_at,success,total_tasks,failed,skipped,duration_ns,schedule,check_only,origin`

// scanRun reads runColumns, followed by any extra columns, into r.
func scanRun(scan func(...any) error, r *RunSummary, extra ...any) error {
	var started, finished, schedule, checkOnly, origin sql.NullString
	var pid, success, total, failed, skipped, duration sql.NullInt64
	dest := append([]any{&r.ID, &r.Title, &r.Hostname, &pid, &started, &finished, &success, &total, &failed, &skipped, &duration, &schedule, &checkOnly, &origin}, extra...)
	if err := scan(dest...); err != nil {
		return err
	}
	r.PID = int(pid.Int64)
	r.StartedAt, _ = time.Parse(time.RFC3339Nano, started.String)
	if finished.Valid {
		r.FinishedAt, _ = time.Parse(time.RFC3339Nano, finished.String)
		r.Finished = true
	}
	r.Success = success.Int64 == 1
	r.TotalTasks = int(total.Int64)
	r.Failed = int(failed.Int64)
	r.Skipped = int(skipped.Int64)
	r.Duration = time.Duration(duration.Int64)
	r.Schedule = schedule.String
	r.CheckOnly = checkOnly.String == "1"
	if origin.String != "" {
		r.Origin = &Origin{}
		fromJSON(origin, r.Origin)
	}
	return nil
}

// ListRuns returns every run, oldest first.
func (s *Store) ListRuns(ctx context.Context) ([]RunSummary, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+runColumns+`,(SELECT COUNT(*) FROM servers WHERE run_id=runs.id) FROM runs ORDER BY started_at ASC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []RunSummary
	for rows.Next() {
		var r RunSummary
		if err := scanRun(rows.Scan, &r, &r.Servers); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetRun returns one run with its servers and tasks, or sql.ErrNoRows.
func (s *Store) GetRun(ctx context.Context, id string) (*RunDetail, error) {
	r := RunDetail{
		ServerErrors:  map[string]string{},
		ServerAddress: map[string]string{},
		ServerEnvVars: map[string]map[string]string{},
		ServerVars:    map[string]map[string]any{},
	}
	var env, vars, includes sql.NullString
	row := s.db.QueryRowContext(ctx, `SELECT `+runColumns+`,envvars,vars,includes FROM runs WHERE id=?`, id)
	if err := scanRun(row.Scan, &r.RunSummary, &env, &vars, &includes); err != nil {
		return nil, err
	}
	fromJSON(env, &r.RunEnvVars)
	fromJSON(vars, &r.RunVars)
	fromJSON(includes, &r.Includes)

	servers, err := s.db.QueryContext(ctx, `SELECT name,address,error,envvars,vars FROM servers WHERE run_id=?`, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = servers.Close() }()
	for servers.Next() {
		var name, address string
		var serverErr, env, vars sql.NullString
		if err := servers.Scan(&name, &address, &serverErr, &env, &vars); err != nil {
			return nil, err
		}
		r.Servers = append(r.Servers, name)
		r.ServerAddress[name] = address
		if serverErr.String != "" {
			r.ServerErrors[name] = serverErr.String
		}
		var serverEnv map[string]string
		var serverVars map[string]any
		if fromJSON(env, &serverEnv); serverEnv != nil {
			r.ServerEnvVars[name] = serverEnv
		}
		if fromJSON(vars, &serverVars); serverVars != nil {
			r.ServerVars[name] = serverVars
		}
	}
	if err := servers.Err(); err != nil {
		return nil, err
	}

	// each task's check and verify come with the list, so a view can show them straight away;
	// the command's own output waits for GetTask
	tasks, err := s.db.QueryContext(ctx, `SELECT server,name,state,exit_code,duration_ns,steps FROM tasks WHERE run_id=?`, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tasks.Close() }()
	for tasks.Next() {
		var t TaskDetail
		var duration sql.NullInt64
		var stepsCol sql.NullString
		if err := tasks.Scan(&t.Server, &t.Name, &t.State, &t.ExitCode, &duration, &stepsCol); err != nil {
			return nil, err
		}
		t.Duration = time.Duration(duration.Int64)
		var st steps
		fromJSON(stepsCol, &st)
		t.Check, t.Verify = st.Check, st.Verify
		r.Tasks = append(r.Tasks, t)
	}
	return &r, tasks.Err()
}

// GetTask returns one task with its command, output, envvars and vars, or sql.ErrNoRows.
func (s *Store) GetTask(ctx context.Context, runID, server, name string) (TaskDetail, error) {
	var t TaskDetail
	var duration sql.NullInt64
	var env, vars, stepsCol sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT server,name,state,command,stdout,stderr,exit_code,duration_ns,envvars,vars,steps FROM tasks WHERE run_id=? AND server=? AND name=?`,
		runID, server, name).Scan(&t.Server, &t.Name, &t.State, &t.Command, &t.Stdout, &t.Stderr, &t.ExitCode, &duration, &env, &vars, &stepsCol)
	if err != nil {
		return TaskDetail{}, err
	}
	t.Duration = time.Duration(duration.Int64)
	fromJSON(env, &t.EnvVars)
	fromJSON(vars, &t.Vars)
	var st steps
	fromJSON(stepsCol, &st)
	t.Check, t.Verify = st.Check, st.Verify
	return t, nil
}

// DeleteRun removes a run with its servers and tasks.
func (s *Store) DeleteRun(ctx context.Context, id string) error {
	_ = s.exec(ctx, `DELETE FROM tasks WHERE run_id=?`, id)
	_ = s.exec(ctx, `DELETE FROM servers WHERE run_id=?`, id)
	return s.exec(ctx, `DELETE FROM runs WHERE id=?`, id)
}

// TruncateRuns deletes all but the keep most recently started runs.
func (s *Store) TruncateRuns(ctx context.Context, keep int) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM runs ORDER BY started_at DESC LIMIT -1 OFFSET ?`, max(keep, 0))
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, id := range ids {
		if err := s.DeleteRun(ctx, id); err != nil {
			return err
		}
	}
	return nil
}
