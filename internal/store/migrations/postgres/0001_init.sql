-- The history tables. Times are RFC 3339 text and JSON columns are text, as in SQLite, so the
-- same queries work on every database.

CREATE TABLE IF NOT EXISTS runs (
	id TEXT PRIMARY KEY,
	title TEXT,
	hostname TEXT,
	pid BIGINT,
	started_at TEXT,
	finished_at TEXT,
	success INTEGER,
	total_tasks INTEGER,
	failed INTEGER,
	skipped INTEGER,
	duration_ns BIGINT,
	envvars TEXT,
	vars TEXT,
	includes TEXT,
	schedule TEXT,
	check_only TEXT,
	origin TEXT
);

CREATE INDEX IF NOT EXISTS runs_started_at ON runs (started_at);

CREATE TABLE IF NOT EXISTS servers (
	id BIGSERIAL PRIMARY KEY,
	run_id TEXT,
	name TEXT,
	address TEXT,
	error TEXT,
	envvars TEXT,
	vars TEXT,
	UNIQUE (run_id, name)
);

CREATE TABLE IF NOT EXISTS tasks (
	id BIGSERIAL PRIMARY KEY,
	run_id TEXT,
	server TEXT,
	name TEXT,
	state INTEGER,
	command TEXT,
	stdout TEXT,
	stderr TEXT,
	exit_code INTEGER,
	duration_ns BIGINT,
	envvars TEXT,
	vars TEXT,
	steps TEXT,
	UNIQUE (run_id, server, name)
);

-- each save of the Gorunfiles kept here, by name, for gorun -serve started without one
CREATE TABLE IF NOT EXISTS gorunfiles (
	id BIGSERIAL PRIMARY KEY,
	name TEXT,
	content TEXT NOT NULL,
	saved_at TEXT NOT NULL
);
