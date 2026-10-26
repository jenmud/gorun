package store

import (
	"context"
	"database/sql"
	"time"

	_ "modernc.org/sqlite"
)

type RunSummary struct {
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
}

type RunDetail struct {
	RunSummary
	Servers []string
	Tasks   []TaskDetail
}

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
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
	}
	for _, st := range stmts {
		if _, err := s.db.Exec(st); err != nil {
			return err
		}
	}

	// Ensure the command column exists on databases created before it was added.
	rows, err := s.db.Query("PRAGMA table_info(tasks)")
	if err == nil {
		defer func() { _ = rows.Close() }()
		hasCommand := false
		for rows.Next() {
			var cid int
			var name, ctype string
			var notnull, pk int
			var dflt sql.NullString
			if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err == nil && name == "command" {
				hasCommand = true
			}
		}
		if !hasCommand {
			_, _ = s.db.Exec("ALTER TABLE tasks ADD COLUMN command TEXT")
		}
	}

	return nil
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
		`SELECT id,title,hostname,pid,started_at,finished_at,success,total_tasks,failed,skipped,duration_ns FROM runs ORDER BY started_at DESC`)
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
		if err := rows.Scan(&r.ID, &r.Title, &r.Hostname, &pid, &started, &finished, &succ, &total, &failed, &skipped, &dur); err != nil {
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
		`SELECT id,title,hostname,pid,started_at,finished_at,success,total_tasks,failed,skipped,duration_ns FROM runs WHERE id=?`, id)
	var r RunDetail
	var started, finished sql.NullString
	var succ sql.NullInt64
	var pid sql.NullInt64
	var total, failed, skipped sql.NullInt64
	var dur sql.NullInt64
	if err := row.Scan(&r.ID, &r.Title, &r.Hostname, &pid, &started, &finished, &succ, &total, &failed, &skipped, &dur); err != nil {
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

	// servers
	srows, err := s.db.QueryContext(ctx, `SELECT name,address FROM servers WHERE run_id=?`, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = srows.Close() }()
	for srows.Next() {
		var name, addr string
		if err := srows.Scan(&name, &addr); err != nil {
			return nil, err
		}
		r.Servers = append(r.Servers, name)
	}
	if err := srows.Err(); err != nil {
		return nil, err
	}

	// tasks
	trows, err := s.db.QueryContext(ctx, `SELECT server,name,state,command,stdout,stderr,exit_code,duration_ns FROM tasks WHERE run_id=?`, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = trows.Close() }()
	for trows.Next() {
		var td TaskDetail
		var dur sql.NullInt64
		if err := trows.Scan(&td.Server, &td.Name, &td.State, &td.Command, &td.Stdout, &td.Stderr, &td.ExitCode, &dur); err != nil {
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
