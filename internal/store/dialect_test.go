package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestDialectOf(t *testing.T) {
	for in, want := range map[string]dialect{
		"/home/me/.gorun/gorun.db":           dialectSQLite,
		"gorun.db":                           dialectSQLite,
		"postgres://u:p@db:5432/gorun":       dialectPostgres,
		"postgresql://u@db/gorun":            dialectPostgres,
		"mysql://u:p@db:3306/gorun?tls=true": dialectMySQL,
	} {
		if got := dialectOf(in); got != want {
			t.Errorf("dialectOf(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestRedactHidesThePassword(t *testing.T) {
	for _, in := range []string{"postgres://gorun:s3cret@db:5432/gorun", "mysql://gorun:s3cret@db/gorun"} {
		if got := Redact(in); strings.Contains(got, "s3cret") || !strings.Contains(got, "@db") {
			t.Errorf("Redact(%q) = %q", in, got)
		}
	}
	if got := Redact("/srv/gorun.db"); got != "/srv/gorun.db" {
		t.Errorf("Redact of a file = %q, want it unchanged", got)
	}
}

func TestMySQLDSN(t *testing.T) {
	got, err := mysqlDSN("mysql://gorun:p%40ss@db.internal/history?tls=skip-verify&charset=utf8mb4")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"gorun:p@ss@tcp(db.internal:3306)/history", "tls=skip-verify", "charset=utf8mb4"} {
		if !strings.Contains(got, want) {
			t.Errorf("dsn = %q, want it to contain %q", got, want)
		}
	}
	if _, err := mysqlDSN("mysql://gorun@db:3306"); err == nil {
		t.Error("expected an error for a URL with no database name")
	}
}

func TestRebind(t *testing.T) {
	q := `UPDATE runs SET a=?, b=? WHERE id=?`
	if got := dialectPostgres.rebind(q); got != `UPDATE runs SET a=$1, b=$2 WHERE id=$3` {
		t.Errorf("postgres: %s", got)
	}
	if got := dialectMySQL.rebind(q); got != q {
		t.Errorf("mysql: %s", got)
	}
}

func TestUpsert(t *testing.T) {
	cols, key := []string{"run_id", "name", "error"}, []string{"run_id", "name"}
	for _, tt := range []struct {
		d      dialect
		update []string
		want   string
	}{
		{dialectSQLite, []string{"error"}, "INSERT INTO servers (run_id,name,error) VALUES (?,?,?) ON CONFLICT(run_id,name) DO UPDATE SET error=excluded.error"},
		{dialectPostgres, nil, "INSERT INTO servers (run_id,name,error) VALUES (?,?,?) ON CONFLICT(run_id,name) DO NOTHING"},
		{dialectMySQL, []string{"error"}, "INSERT INTO servers (run_id,name,error) VALUES (?,?,?) ON DUPLICATE KEY UPDATE error=VALUES(error)"},
		{dialectMySQL, nil, "INSERT IGNORE INTO servers (run_id,name,error) VALUES (?,?,?)"},
	} {
		if got := tt.d.upsert("servers", cols, key, tt.update); got != tt.want {
			t.Errorf("%s upsert = %s\nwant %s", tt.d, got, tt.want)
		}
	}
}

func TestEveryDialectHasTheSameMigrations(t *testing.T) {
	var want []int
	for i, d := range []dialect{dialectSQLite, dialectPostgres, dialectMySQL} {
		ms, err := loadMigrations(d)
		if err != nil {
			t.Fatal(err)
		}
		var versions []int
		for _, m := range ms {
			if len(m.stmts) == 0 {
				t.Errorf("%s %s has no statements", d, m.name)
			}
			versions = append(versions, m.version)
		}
		if i == 0 {
			want = versions
		} else if !slices.Equal(versions, want) {
			t.Errorf("%s migrations %v, sqlite has %v: every database needs every migration", d, versions, want)
		}
	}
}

// TestOpenUpgradesAnSQLiteDatabaseFromBeforeMigrations opens a database as the first release
// made it: its runs and tasks must still be there, with the columns added since.
func TestOpenUpgradesAnSQLiteDatabaseFromBeforeMigrations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`CREATE TABLE runs (id TEXT PRIMARY KEY, title TEXT, hostname TEXT, pid INTEGER, started_at TEXT, finished_at TEXT, success INTEGER, total_tasks INTEGER, failed INTEGER, skipped INTEGER, duration_ns INTEGER)`,
		`CREATE TABLE servers (id INTEGER PRIMARY KEY AUTOINCREMENT, run_id TEXT, name TEXT, address TEXT, UNIQUE(run_id, name))`,
		`CREATE TABLE tasks (id INTEGER PRIMARY KEY AUTOINCREMENT, run_id TEXT, server TEXT, name TEXT, state INTEGER, stdout TEXT, stderr TEXT, exit_code INTEGER, duration_ns INTEGER, UNIQUE(run_id, server, name))`,
		`INSERT INTO runs (id, title, hostname, pid, started_at) VALUES ('old-run', 'Old', 'laptop', 7, '2025-01-02T03:04:05Z')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	_ = db.Close()

	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if err := st.SetRunSchedule(ctx, "old-run", "nightly"); err != nil {
		t.Fatalf("a column added since the first release is missing: %v", err)
	}
	r, err := st.GetRun(ctx, "old-run")
	if err != nil || r.Title != "Old" || r.Schedule != "nightly" {
		t.Fatalf("GetRun = %+v, %v", r, err)
	}
	if _, err := st.SaveGorunfile(ctx, "web", "title = \"Web\"\n", time.Now()); err != nil {
		t.Fatalf("the gorunfiles table is missing: %v", err)
	}
}

// TestStoreOnEveryDatabase runs the same calls on SQLite, and on Postgres and MySQL when
// GORUN_TEST_POSTGRES_URL and GORUN_TEST_MYSQL_URL name an empty database to use (its tables
// are dropped first), such as postgres://gorun:gorun@localhost:5432/gorun_test.
func TestStoreOnEveryDatabase(t *testing.T) {
	targets := map[string]string{"sqlite": filepath.Join(t.TempDir(), "gorun.db")}
	for name, env := range map[string]string{"postgres": "GORUN_TEST_POSTGRES_URL", "mysql": "GORUN_TEST_MYSQL_URL"} {
		if url := os.Getenv(env); url != "" {
			targets[name] = url
		} else {
			t.Logf("%s: set %s to test it", name, env)
		}
	}
	for name, location := range targets {
		t.Run(name, func(t *testing.T) {
			dropTables(t, location)
			openAtOnce(t, location)
			dropTables(t, location)
			exerciseStore(t, location)
		})
	}
}

// openAtOnce opens a new database from several processes' worth of stores at once, as gorun
// processes starting together do: each migration must be applied once, without errors.
func openAtOnce(t *testing.T, location string) {
	t.Helper()
	errs := make(chan error, 4)
	for range cap(errs) {
		go func() {
			st, err := Open(location)
			if err == nil {
				err = st.Close()
			}
			errs <- err
		}()
	}
	for range cap(errs) {
		if err := <-errs; err != nil {
			t.Errorf("opening at once: %v", err)
		}
	}
}

// dropTables empties a test server database, so each run starts from no tables.
func dropTables(t *testing.T, location string) {
	t.Helper()
	if !IsURL(location) {
		return
	}
	name, dsn, err := driver(location)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open(name, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, table := range []string{"tasks", "servers", "runs", "gorunfiles", "schema_migrations"} {
		if _, err := db.Exec("DROP TABLE IF EXISTS " + table); err != nil {
			t.Fatalf("drop %s: %v", table, err)
		}
	}
}

func exerciseStore(t *testing.T, location string) {
	ctx := context.Background()
	st, err := Open(location)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 11, 27, 1, 2, 3, 4, time.UTC)

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	for i, id := range []string{"run-1", "run-2", "run-3"} {
		must(st.CreateRun(ctx, id, "Web fleet", "laptop", 42, start.Add(time.Duration(i)*time.Hour)))
	}
	must(st.CreateRun(ctx, "run-3", "Web fleet again", "laptop", 43, start.Add(2*time.Hour))) // a repeated start replaces it
	must(st.AddServer(ctx, "run-3", "web-1", "10.0.0.1"))
	must(st.AddServer(ctx, "run-3", "web-1", "10.0.0.9"))
	must(st.SetServerEnvVars(ctx, "run-3", "web-1", map[string]string{"REGION": "eu"}, map[string]any{"port": 8080.0}))
	must(st.SetServerError(ctx, "run-3", "db-1", "dial tcp: connection refused"))
	must(st.PlanTask(ctx, "run-3", "web-1", "nginx"))
	must(st.PlanTask(ctx, "run-3", "web-1", "site"))
	// output that Postgres and MySQL can't keep as it is: a NUL byte and bytes that aren't UTF-8
	must(st.UpsertTask(ctx, "run-3", "web-1", "nginx", StateFailed, "nginx -t", "ok\x00\n", "bad \xff byte", 1, 1500*time.Millisecond))
	must(st.PlanTask(ctx, "run-3", "web-1", "nginx")) // planning again leaves the result alone
	must(st.SetTaskSteps(ctx, "run-3", "web-1", "nginx", &Step{Command: "test -f /etc/nginx", ExitCode: 1}, nil))
	must(st.SetTaskEnvVars(ctx, "run-3", "web-1", "nginx", map[string]string{"A": "1"}, nil))
	must(st.SetRunEnvVars(ctx, "run-3", map[string]string{"G": "1"}, map[string]any{"v": "x"}))
	must(st.SetRunIncludes(ctx, "run-3", []Include{{Source: "git::x", Ref: "v1", Commit: "abc"}}))
	must(st.SetRunSchedule(ctx, "run-3", "nightly"))
	must(st.SetRunCheckOnly(ctx, "run-3"))
	must(st.SetRunOrigin(ctx, "run-3", &Origin{User: "jenda"}))
	must(st.FinishRun(ctx, "run-3", false, 2, 1, 1, 90*time.Second, start.Add(3*time.Hour)))

	runs, err := st.ListRuns(ctx)
	must(err)
	if len(runs) != 3 || runs[2].ID != "run-3" || runs[2].Title != "Web fleet again" || runs[2].Servers != 2 {
		t.Fatalf("ListRuns = %+v", runs)
	}
	r, err := st.GetRun(ctx, "run-3")
	must(err)
	if !r.Finished || r.Success || r.Failed != 1 || r.Duration != 90*time.Second || r.Schedule != "nightly" ||
		!r.CheckOnly || r.Origin == nil || r.Origin.User != "jenda" || !r.StartedAt.Equal(start.Add(2*time.Hour)) {
		t.Errorf("GetRun summary = %+v", r.RunSummary)
	}
	if r.ServerAddress["web-1"] != "10.0.0.9" || r.ServerErrors["db-1"] == "" || r.ServerEnvVars["web-1"]["REGION"] != "eu" ||
		len(r.Includes) != 1 || r.RunVars["v"] != "x" || len(r.Tasks) != 2 {
		t.Errorf("GetRun detail = %+v", r)
	}
	task, err := st.GetTask(ctx, "run-3", "web-1", "nginx")
	must(err)
	if task.State != StateFailed || task.ExitCode != 1 || task.Command != "nginx -t" || !strings.HasPrefix(task.Stdout, "ok") ||
		!strings.HasPrefix(task.Stderr, "bad ") || task.Check == nil || task.Check.ExitCode != 1 || task.EnvVars["A"] != "1" {
		t.Errorf("GetTask = %+v", task)
	}

	saved, err := st.SaveGorunfile(ctx, "web", "title = \"v1\"\n", start)
	must(err)
	again, err := st.SaveGorunfile(ctx, "web", "title = \"v1\"\n", start)
	must(err)
	_, err = st.SaveGorunfile(ctx, "web", "title = \"v2\"\n", start.Add(time.Minute))
	must(err)
	_, err = st.SaveGorunfile(ctx, "base", "title = \"base\"\n", start.Add(2*time.Minute))
	must(err)
	if !saved || again {
		t.Errorf("SaveGorunfile = %v then %v, want true then false for the same content", saved, again)
	}
	content, ok, err := st.Gorunfile(ctx, "web")
	must(err)
	versions, err := st.GorunfileVersions(ctx, "web")
	must(err)
	list, err := st.Gorunfiles(ctx)
	must(err)
	if !ok || content != "title = \"v2\"\n" || versions != 2 || len(list) != 2 || list[0].Name != "base" {
		t.Errorf("Gorunfile = %q %v, versions %d, list %+v", content, ok, versions, list)
	}
	must(st.DeleteGorunfile(ctx, "base"))

	must(st.TruncateRuns(ctx, 1))
	runs, err = st.ListRuns(ctx)
	must(err)
	if len(runs) != 1 || runs[0].ID != "run-3" {
		t.Errorf("after TruncateRuns(1): %+v", runs)
	}
	must(st.Close())

	// opening again applies nothing new and keeps the data
	st, err = Open(location)
	must(err)
	defer st.Close()
	if _, err := st.GetRun(ctx, "run-3"); err != nil {
		t.Errorf("after reopening: %v", err)
	}
}
