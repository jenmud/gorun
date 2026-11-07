package store

import (
	"context"
	"time"
)

// TaskOutcome is one finished task in history, newest run first.
type TaskOutcome struct {
	RunID    string
	Server   string
	Name     string
	Failed   bool
	Duration time.Duration
	At       time.Time
}

// AlertRow is a saved alert. Server and Name are empty for run-level alerts.
type AlertRow struct {
	ID        int64
	Rule      string
	RunID     string
	Server    string
	Name      string
	Message   string
	CreatedAt time.Time
}

// TaskOutcomes returns the most recent finished task outcomes, newest run first, up to limit.
// Only done (2) and failed (3) tasks count; skipped and pending ones say nothing about health.
func (s *Store) TaskOutcomes(ctx context.Context, limit int) ([]TaskOutcome, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT t.run_id, t.server, t.name, t.state, t.duration_ns, r.started_at
		FROM tasks t JOIN runs r ON r.id = t.run_id
		WHERE t.state IN (2, 3)
		ORDER BY r.started_at DESC, t.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []TaskOutcome
	for rows.Next() {
		var o TaskOutcome
		var state int
		var durNS int64
		var started string
		if err := rows.Scan(&o.RunID, &o.Server, &o.Name, &state, &durNS, &started); err != nil {
			return nil, err
		}
		o.Failed = state == 3
		o.Duration = time.Duration(durNS)
		o.At, _ = time.Parse(time.RFC3339Nano, started)
		out = append(out, o)
	}
	return out, rows.Err()
}

// AddAlert saves a. It reports false when the same rule already fired for that task in that run,
// so re-evaluating a run never raises the same alert twice.
func (s *Store) AddAlert(ctx context.Context, a AlertRow) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO alerts (rule, run_id, server, name, message, created_at) VALUES (?,?,?,?,?,?)`,
		a.Rule, a.RunID, a.Server, a.Name, a.Message, a.CreatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// ListAlerts returns the newest alerts first, up to limit.
func (s *Store) ListAlerts(ctx context.Context, limit int) ([]AlertRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, rule, run_id, server, name, message, created_at FROM alerts ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []AlertRow
	for rows.Next() {
		var a AlertRow
		var created string
		if err := rows.Scan(&a.ID, &a.Rule, &a.RunID, &a.Server, &a.Name, &a.Message, &created); err != nil {
			return nil, err
		}
		a.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, a)
	}
	return out, rows.Err()
}
