package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Capture is one assistant answer kept for training: the prompt exactly as the model got it,
// its reply, and what became of it.
type Capture struct {
	ID        string
	Kind      string // "chat" or "generate"
	CreatedAt time.Time
	User      string // who asked, when the web UI has a login
	Model     string
	Messages  string // the prompt as JSON chat messages, system first
	Reply     string
	Tasks     string // the tasks and schedules in the reply that loaded, as TOML
	TaskNames string
	TaskError string // why the reply's tasks didn't load
	Repaired  bool   // the model was asked to fix its tasks
	SavedAt   time.Time
	Feedback  int // 1 good, -1 bad, 0 not rated
}

// SaveCapture keeps an assistant answer.
func (s *Store) SaveCapture(ctx context.Context, c Capture) error {
	return s.exec(ctx, `INSERT INTO assistant_captures (id, kind, created_at, user_name, model, messages, reply, tasks, task_names, task_error, repaired, saved_at, feedback)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.Kind, c.CreatedAt.UTC().Format(time.RFC3339Nano), c.User, c.Model, c.Messages, c.Reply,
		c.Tasks, c.TaskNames, c.TaskError, boolInt(c.Repaired), timeText(c.SavedAt), c.Feedback)
}

// MarkCaptureSaved records that the tasks of a capture were saved into a Gorunfile.
func (s *Store) MarkCaptureSaved(ctx context.Context, id string, at time.Time) error {
	return s.exec(ctx, `UPDATE assistant_captures SET saved_at = ? WHERE id = ?`, timeText(at), id)
}

// SetCaptureFeedback records the asker's rating of a capture: 1 good, -1 bad, 0 none.
func (s *Store) SetCaptureFeedback(ctx context.Context, id string, feedback int) error {
	return s.exec(ctx, `UPDATE assistant_captures SET feedback = ? WHERE id = ?`, feedback, id)
}

// Captures lists the kept answers, newest first; limit 0 lists them all.
func (s *Store) Captures(ctx context.Context, limit int) ([]Capture, error) {
	q := `SELECT id, kind, created_at, user_name, model, messages, reply, tasks, task_names, task_error, repaired, saved_at, feedback
		FROM assistant_captures ORDER BY created_at DESC, id`
	var args []any
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Capture
	for rows.Next() {
		var c Capture
		var created, saved string
		var repaired int
		if err := rows.Scan(&c.ID, &c.Kind, &created, &c.User, &c.Model, &c.Messages, &c.Reply, &c.Tasks, &c.TaskNames, &c.TaskError, &repaired, &saved, &c.Feedback); err != nil {
			return nil, err
		}
		c.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		c.SavedAt, _ = time.Parse(time.RFC3339Nano, saved)
		c.Repaired = repaired != 0
		out = append(out, c)
	}
	return out, rows.Err()
}

// CountCaptures counts the kept answers.
func (s *Store) CountCaptures(ctx context.Context) (int, error) {
	var n int
	err := s.queryRow(ctx, `SELECT COUNT(*) FROM assistant_captures`).Scan(&n)
	return n, err
}

// DeleteCapture removes one kept answer; an empty id removes them all.
func (s *Store) DeleteCapture(ctx context.Context, id string) error {
	if id == "" {
		return s.exec(ctx, `DELETE FROM assistant_captures`)
	}
	return s.exec(ctx, `DELETE FROM assistant_captures WHERE id = ?`, id)
}

// Outcome is how the runs of some tasks went.
type Outcome struct {
	Passed, Failed int // tasks that finished or found nothing to do, and tasks that failed
}

// TaskOutcome counts how the tasks called names did in runs that started at since or later, such
// as the tasks an assistant answer wrote, once they were saved.
func (s *Store) TaskOutcome(ctx context.Context, names []string, since time.Time) (Outcome, error) {
	var o Outcome
	if len(names) == 0 {
		return o, nil
	}
	args := []any{since.UTC().Format(time.RFC3339Nano)}
	for _, n := range names {
		args = append(args, n)
	}
	q := `SELECT t.state, COUNT(*) FROM tasks t JOIN runs r ON r.id = t.run_id
		WHERE r.started_at >= ? AND t.name IN (` + strings.TrimSuffix(strings.Repeat("?,", len(names)), ",") + `)
		GROUP BY t.state`
	rows, err := s.query(ctx, q, args...)
	if err != nil {
		return o, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var state, n int
		if err := rows.Scan(&state, &n); err != nil {
			return o, err
		}
		switch state {
		case StateDone, StateUnchanged:
			o.Passed += n
		case StateFailed:
			o.Failed += n
		}
	}
	return o, rows.Err()
}

// Setting returns a stored setting, and false when it was never set.
func (s *Store) Setting(ctx context.Context, name string) (string, bool, error) {
	var v string
	err := s.queryRow(ctx, `SELECT value FROM settings WHERE name = ?`, name).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return v, err == nil, err
}

// SetSetting stores a setting.
func (s *Store) SetSetting(ctx context.Context, name, value string) error {
	return s.exec(ctx, s.dialect.upsert("settings", []string{"name", "value"}, []string{"name"}, []string{"value"}), name, value)
}

// timeText is t for a TEXT column, or "" for the zero time.
func timeText(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}
