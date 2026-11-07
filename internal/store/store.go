package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	_ "modernc.org/sqlite"
)

type RunSummary struct {
	Servers    int
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
}

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
}

type RunDetail struct {
	RunSummary
	Servers []string
	Tasks   []TaskDetail

	// ServerErrors holds, by server name, why a server couldn't run its tasks at
	// all (e.g. its SSH connection failed). Such servers have no tasks recorded.
	ServerErrors map[string]string

	// ServerEnvVars and ServerVars hold context captured by each plan event.
	ServerEnvVars map[string]map[string]string
	ServerVars    map[string]map[string]any
	ServerAddress map[string]string
	RunEnvVars    map[string]string
	RunVars       map[string]any
}

type Store struct {
	db *sql.DB
}

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

func (s *Store) migrate() error {
	_, _ = s.db.Exec("PRAGMA journal_mode=WAL;")
	stmts := []string{
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
		`CREATE TABLE IF NOT EXISTS alerts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			rule TEXT,
			run_id TEXT,
			server TEXT,
			name TEXT,
			message TEXT,
			created_at TEXT,
			UNIQUE(rule, run_id, server, name)
		)`,
	}
	for _, st := range stmts {
		if _, err := s.db.Exec(st); err != nil {
			return err
		}
	}

	// Ensure columns added later exist on databases created before them.
	s.addColumn("tasks", "command")
	s.addColumn("servers", "error")
	s.addColumn("servers", "envvars")
	s.addColumn("servers", "vars")
	s.addColumn("tasks", "envvars")
	s.addColumn("tasks", "vars")
	s.addColumn("runs", "envvars")
	s.addColumn("runs", "vars")

	return nil
}

// addColumn adds a TEXT column to table unless it is there already.
func (s *Store) addColumn(table, column string) {
	rows, err := s.db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return
	}
	has := false
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err == nil && name == column {
			has = true
		}
	}
	_ = rows.Close()
	if !has {
		_, _ = s.db.Exec("ALTER TABLE " + table + " ADD COLUMN " + column + " TEXT")
	}
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) CreateRun(ctx context.Context, id, title, hostname string, pid int, startedAt time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO runs (id,title,hostname,pid,started_at) VALUES (?,?,?,?,?)`,
		id, title, hostname, pid, startedAt.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) AddServer(ctx context.Context, runID, name, address string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO servers (run_id,name,address) VALUES (?,?,?)`,
		runID, name, address)
	return err
}

// SetServerEnvVars stores the merged env vars and vars for a server.
func (s *Store) SetServerEnvVars(ctx context.Context, runID, name string, envvars map[string]string, vars map[string]any) error {
	envJSON, _ := json.Marshal(envvars)
	varJSON, _ := json.Marshal(vars)
	_, err := s.db.ExecContext(ctx,
		`UPDATE servers SET envvars=?, vars=? WHERE run_id=? AND name=?`,
		string(envJSON), string(varJSON), runID, name)
	return err
}

// SetRunEnvVars stores the global env vars and vars for a run.
func (s *Store) SetRunEnvVars(ctx context.Context, runID string, envvars map[string]string, vars map[string]any) error {
	envJSON, _ := json.Marshal(envvars)
	varJSON, _ := json.Marshal(vars)
	// store on first server row? Instead, we add columns to runs. But runs table has no envvars/vars column yet.
	// We'll add migration. For now, no-op if columns missing handled by migrate.
	_, err := s.db.ExecContext(ctx,
		`UPDATE runs SET envvars=?, vars=? WHERE id=?`,
		string(envJSON), string(varJSON), runID)
	return err
}

// SetTaskEnvVars stores the merged env vars and vars for a task.
func (s *Store) SetTaskEnvVars(ctx context.Context, runID, server, name string, envvars map[string]string, vars map[string]any) error {
	envJSON, _ := json.Marshal(envvars)
	varJSON, _ := json.Marshal(vars)
	_, err := s.db.ExecContext(ctx,
		`UPDATE tasks SET envvars=?, vars=? WHERE run_id=? AND server=? AND name=?`,
		string(envJSON), string(varJSON), runID, server, name)
	return err
}

// SetServerError records why a server couldn't run its tasks at all.
func (s *Store) SetServerError(ctx context.Context, runID, name, message string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO servers (run_id,name,address,error) VALUES (?,?,'',?)
		 ON CONFLICT(run_id,name) DO UPDATE SET error=excluded.error`,
		runID, name, message)
	return err
}

// PlanTask records a task as pending, unless it already has a row, so a run shows the tasks a
// server was meant to run even when it never got to them.
func (s *Store) PlanTask(ctx context.Context, runID, server, name string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO tasks (run_id,server,name,state,command,stdout,stderr,exit_code,duration_ns)
		 VALUES (?,?,?,0,'','','',0,0)`, runID, server, name)
	return err
}

func (s *Store) UpsertTask(ctx context.Context, runID, server, name string, state int, command, stdout, stderr string, exitCode int, duration time.Duration) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO tasks (run_id,server,name,state,command,stdout,stderr,exit_code,duration_ns)
		 VALUES (?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(run_id,server,name) DO UPDATE SET
		   state=excluded.state, command=excluded.command, stdout=excluded.stdout, stderr=excluded.stderr,
		   exit_code=excluded.exit_code, duration_ns=excluded.duration_ns`,
		runID, server, name, state, command, stdout, stderr, exitCode, int64(duration))
	return err
}

func (s *Store) FinishRun(ctx context.Context, runID string, success bool, total, failed, skipped int, duration time.Duration, finishedAt time.Time) error {
	succ := 0
	if success {
		succ = 1
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE runs SET finished_at=?, success=?, total_tasks=?, failed=?, skipped=?, duration_ns=? WHERE id=?`,
		finishedAt.UTC().Format(time.RFC3339Nano), succ, total, failed, skipped, int64(duration), runID)
	return err
}

func (s *Store) ListRuns(ctx context.Context) ([]RunSummary, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT (SELECT COUNT(*) FROM servers WHERE run_id=runs.id),id,title,hostname,pid,started_at,finished_at,success,total_tasks,failed,skipped,duration_ns FROM runs ORDER BY started_at ASC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []RunSummary
	for rows.Next() {
		var r RunSummary
		var started, finished sql.NullString
		var succ sql.NullInt64
		var pid sql.NullInt64
		var total, failed, skipped sql.NullInt64
		var dur sql.NullInt64
		if err := rows.Scan(&r.Servers, &r.ID, &r.Title, &r.Hostname, &pid, &started, &finished, &succ, &total, &failed, &skipped, &dur); err != nil {
			return nil, err
		}
		r.PID = int(pid.Int64)
		if started.Valid {
			r.StartedAt, _ = time.Parse(time.RFC3339Nano, started.String)
		}
		if finished.Valid {
			r.FinishedAt, _ = time.Parse(time.RFC3339Nano, finished.String)
			r.Finished = true
		}
		if succ.Valid {
			r.Success = succ.Int64 == 1
		} else if r.Finished {
			r.Success = r.Failed == 0 && r.Skipped == 0
		}
		r.TotalTasks = int(total.Int64)
		r.Failed = int(failed.Int64)
		r.Skipped = int(skipped.Int64)
		r.Duration = time.Duration(dur.Int64)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) GetRun(ctx context.Context, id string) (*RunDetail, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id,title,hostname,pid,started_at,finished_at,success,total_tasks,failed,skipped,duration_ns,envvars,vars FROM runs WHERE id=?`, id)
	var r RunDetail
	var started, finished sql.NullString
	var succ sql.NullInt64
	var pid sql.NullInt64
	var total, failed, skipped sql.NullInt64
	var dur sql.NullInt64
	var runEnv, runVars sql.NullString
	if err := row.Scan(&r.ID, &r.Title, &r.Hostname, &pid, &started, &finished, &succ, &total, &failed, &skipped, &dur, &runEnv, &runVars); err != nil {
		return nil, err
	}
	r.PID = int(pid.Int64)
	r.TotalTasks = int(total.Int64)
	r.Failed = int(failed.Int64)
	r.Skipped = int(skipped.Int64)
	if started.Valid {
		r.StartedAt, _ = time.Parse(time.RFC3339Nano, started.String)
	}
	if finished.Valid {
		r.FinishedAt, _ = time.Parse(time.RFC3339Nano, finished.String)
		r.Finished = true
	}
	if succ.Valid {
		r.Success = succ.Int64 == 1
	} else if r.Finished {
		r.Success = r.Failed == 0 && r.Skipped == 0
	}
	r.Duration = time.Duration(dur.Int64)
	if runEnv.Valid && runEnv.String != "" {
		var ev map[string]string
		if json.Unmarshal([]byte(runEnv.String), &ev) == nil {
			r.RunEnvVars = ev
		}
	}
	if runVars.Valid && runVars.String != "" {
		var vv map[string]any
		if json.Unmarshal([]byte(runVars.String), &vv) == nil {
			r.RunVars = vv
		}
	}

	// servers
	srows, err := s.db.QueryContext(ctx, `SELECT name,address,error,envvars,vars FROM servers WHERE run_id=?`, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = srows.Close() }()
	r.ServerErrors = map[string]string{}
	r.ServerEnvVars = map[string]map[string]string{}
	r.ServerVars = map[string]map[string]any{}
	r.ServerAddress = map[string]string{}
	for srows.Next() {
		var name, addr string
		var serverErr sql.NullString
		var envJSON, varJSON sql.NullString
		if err := srows.Scan(&name, &addr, &serverErr, &envJSON, &varJSON); err != nil {
			return nil, err
		}
		r.Servers = append(r.Servers, name)
		r.ServerAddress[name] = addr
		if serverErr.String != "" {
			r.ServerErrors[name] = serverErr.String
		}
		if envJSON.Valid && envJSON.String != "" {
			var ev map[string]string
			if json.Unmarshal([]byte(envJSON.String), &ev) == nil {
				r.ServerEnvVars[name] = ev
			}
		}
		if varJSON.Valid && varJSON.String != "" {
			var vv map[string]any
			if json.Unmarshal([]byte(varJSON.String), &vv) == nil {
				r.ServerVars[name] = vv
			}
		}
	}
	if err := srows.Err(); err != nil {
		return nil, err
	}

	// tasks (lightweight: heavy output is loaded on demand)
	trows, err := s.db.QueryContext(ctx, `SELECT server,name,state,exit_code,duration_ns FROM tasks WHERE run_id=?`, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = trows.Close() }()
	for trows.Next() {
		var td TaskDetail
		var dur sql.NullInt64
		if err := trows.Scan(&td.Server, &td.Name, &td.State, &td.ExitCode, &dur); err != nil {
			return nil, err
		}
		td.Duration = time.Duration(dur.Int64)
		r.Tasks = append(r.Tasks, td)
	}
	if err := trows.Err(); err != nil {
		return nil, err
	}
	return &r, nil
}

// GetTask loads the full detail (command, stdout, stderr) for a single task.
func (s *Store) GetTask(ctx context.Context, runID, server, name string) (TaskDetail, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT server,name,state,command,stdout,stderr,exit_code,duration_ns,envvars,vars FROM tasks WHERE run_id=? AND server=? AND name=?`,
		runID, server, name)
	var td TaskDetail
	var dur sql.NullInt64
	var envJSON, varJSON sql.NullString
	if err := row.Scan(&td.Server, &td.Name, &td.State, &td.Command, &td.Stdout, &td.Stderr, &td.ExitCode, &dur, &envJSON, &varJSON); err != nil {
		return TaskDetail{}, err
	}
	td.Duration = time.Duration(dur.Int64)
	if envJSON.Valid && envJSON.String != "" {
		var ev map[string]string
		if json.Unmarshal([]byte(envJSON.String), &ev) == nil {
			td.EnvVars = ev
		}
	}
	if varJSON.Valid && varJSON.String != "" {
		var vv map[string]any
		if json.Unmarshal([]byte(varJSON.String), &vv) == nil {
			td.Vars = vv
		}
	}
	return td, nil
}

// Aliases for compatibility with callers that use other names.
func (s *Store) GetTaskDetail(ctx context.Context, runID, server, name string) (TaskDetail, error) {
	return s.GetTask(ctx, runID, server, name)
}
func (s *Store) LoadTask(ctx context.Context, runID, server, name string) (TaskDetail, error) {
	return s.GetTask(ctx, runID, server, name)
}
func (s *Store) FetchTask(ctx context.Context, runID, server, name string) (TaskDetail, error) {
	return s.GetTask(ctx, runID, server, name)
}

// DeleteRun removes a run and its associated servers and tasks.
func (s *Store) DeleteRun(ctx context.Context, id string) error {
	_, _ = s.db.ExecContext(ctx, `DELETE FROM tasks WHERE run_id=?`, id)
	_, _ = s.db.ExecContext(ctx, `DELETE FROM servers WHERE run_id=?`, id)
	_, err := s.db.ExecContext(ctx, `DELETE FROM runs WHERE id=?`, id)
	return err
}

// TruncateRuns keeps only the most recent N runs (by started_at), deleting the rest.
func (s *Store) TruncateRuns(ctx context.Context, keep int) error {
	if keep < 0 {
		keep = 0
	}
	// find ids to delete
	rows, err := s.db.QueryContext(ctx,
		`SELECT id FROM runs ORDER BY started_at DESC LIMIT -1 OFFSET ?`, keep)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if err := s.DeleteRun(ctx, id); err != nil {
			return err
		}
	}
	return nil
}
