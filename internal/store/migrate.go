package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"
)

// migrations holds each database's schema changes, as migrations/<dialect>/NNNN_name.sql. A
// change to the schema is a new file with the next number for every dialect; files already
// released are never edited.
//
//go:embed migrations
var migrations embed.FS

// migration is one numbered file of statements.
type migration struct {
	version int
	name    string
	stmts   []string
}

// loadMigrations reads d's migrations, in order.
func loadMigrations(d dialect) ([]migration, error) {
	dir := path.Join("migrations", string(d))
	entries, err := fs.ReadDir(migrations, dir)
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, e := range entries {
		num, _, ok := strings.Cut(e.Name(), "_")
		version, err := strconv.Atoi(num)
		if !ok || err != nil || !strings.HasSuffix(e.Name(), ".sql") {
			return nil, fmt.Errorf("migration %s: name it NNNN_what.sql", e.Name())
		}
		data, err := fs.ReadFile(migrations, path.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, migration{version, e.Name(), statements(string(data))})
	}
	slices.SortFunc(out, func(a, b migration) int { return a.version - b.version })
	return out, nil
}

// statements splits a migration file at the semicolons that end its lines, without its
// comment lines. The files hold plain DDL, so that is enough.
func statements(sql string) []string {
	var out []string
	var cur strings.Builder
	for _, line := range strings.Split(sql, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		cur.WriteString(line + "\n")
		if strings.HasSuffix(trimmed, ";") {
			out = append(out, strings.TrimSuffix(strings.TrimSpace(cur.String()), ";"))
			cur.Reset()
		}
	}
	if s := strings.TrimSpace(cur.String()); s != "" {
		out = append(out, s)
	}
	return out
}

// legacyColumns are the columns SQLite databases gained before migrations existed. A database
// that old gets the ones it lacks before the first migration is recorded.
var legacyColumns = [][2]string{
	{"tasks", "command"}, {"servers", "error"}, {"servers", "envvars"}, {"servers", "vars"},
	{"tasks", "envvars"}, {"tasks", "vars"}, {"runs", "envvars"}, {"runs", "vars"},
	{"runs", "includes"}, {"runs", "schedule"}, {"tasks", "steps"}, {"runs", "check_only"},
	{"runs", "origin"}, {"gorunfiles", "name"},
}

// migrate applies the migrations the database hasn't had yet, each with its version recorded
// in schema_migrations.
func (s *Store) migrate(ctx context.Context) error {
	all, err := loadMigrations(s.dialect)
	if err != nil {
		return err
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	release, err := s.lockMigrations(ctx, conn)
	if err != nil {
		return err
	}
	if err := s.migrateLocked(ctx, conn, all); err != nil {
		_ = release(false)
		return err
	}
	return release(true)
}

// migrateLocked applies the migrations in all that schema_migrations doesn't list yet.
func (s *Store) migrateLocked(ctx context.Context, conn *sql.Conn, all []migration) error {

	if _, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER NOT NULL PRIMARY KEY,
		applied_at VARCHAR(64) NOT NULL
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	applied := map[int]bool{}
	rows, err := conn.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			_ = rows.Close()
			return err
		}
		applied[v] = true
	}
	if err := rows.Close(); err != nil {
		return err
	}

	if len(applied) == 0 && s.dialect == dialectSQLite {
		s.addLegacyColumns(ctx, conn)
	}
	for _, m := range all {
		if applied[m.version] {
			continue
		}
		if err := s.apply(ctx, conn, m); err != nil {
			return fmt.Errorf("migration %s: %w", m.name, err)
		}
	}
	return nil
}

// apply runs one migration and records it, in a transaction on Postgres. SQLite's migrations
// already run inside the transaction lockMigrations holds. MySQL commits each DDL statement by
// itself, so there a failed migration may be half done.
func (s *Store) apply(ctx context.Context, conn *sql.Conn, m migration) error {
	type execer interface {
		ExecContext(context.Context, string, ...any) (sql.Result, error)
	}
	var ex execer = conn
	var tx *sql.Tx
	if s.dialect == dialectPostgres {
		var err error
		if tx, err = conn.BeginTx(ctx, nil); err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		ex = tx
	}
	for _, stmt := range m.stmts {
		if _, err := ex.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if _, err := ex.ExecContext(ctx, s.dialect.rebind(`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`),
		m.version, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	if tx != nil {
		return tx.Commit()
	}
	return nil
}

// addLegacyColumns adds the columns an SQLite database from before migrations may lack.
func (s *Store) addLegacyColumns(ctx context.Context, conn *sql.Conn) {
	var n int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='runs'`).Scan(&n); err != nil || n == 0 {
		return
	}
	for _, c := range legacyColumns {
		// fails when the column is already there, which is fine
		_, _ = conn.ExecContext(ctx, "ALTER TABLE "+c[0]+" ADD COLUMN "+c[1]+" TEXT")
	}
}

// lockMigrations stops two gorun processes migrating one database at once, and returns what
// releases the lock, told whether the migrations succeeded. On SQLite the lock is a write
// transaction around them all, committed or rolled back on release, so a second process waits
// for it, up to the busy timeout.
func (s *Store) lockMigrations(ctx context.Context, conn *sql.Conn) (func(ok bool) error, error) {
	const key = 7104331 // any fixed number, the same in every gorun
	switch s.dialect {
	case dialectPostgres:
		if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, key); err != nil {
			return nil, fmt.Errorf("lock for migrations: %w", err)
		}
		return func(bool) error {
			_, err := conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, key)
			return err
		}, nil
	case dialectMySQL:
		var got sql.NullInt64
		if err := conn.QueryRowContext(ctx, `SELECT GET_LOCK('gorun_migrations', 60)`).Scan(&got); err != nil || got.Int64 != 1 {
			return nil, fmt.Errorf("lock for migrations: %v", cmpErr(err, "timed out after 60s"))
		}
		return func(bool) error {
			_, err := conn.ExecContext(context.Background(), `SELECT RELEASE_LOCK('gorun_migrations')`)
			return err
		}, nil
	}
	if _, err := conn.ExecContext(ctx, `PRAGMA busy_timeout=5000`); err != nil {
		return nil, err
	}
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return nil, fmt.Errorf("lock for migrations: %w", err)
	}
	return func(ok bool) error {
		end := `ROLLBACK`
		if ok {
			end = `COMMIT`
		}
		_, err := conn.ExecContext(context.Background(), end)
		return err
	}, nil
}

func cmpErr(err error, otherwise string) string {
	if err != nil {
		return err.Error()
	}
	return otherwise
}
