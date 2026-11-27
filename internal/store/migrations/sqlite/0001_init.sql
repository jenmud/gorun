-- The history tables as gorun v0.36 left them. IF NOT EXISTS, because databases made before
-- migrations existed already have them; Open adds any columns those are missing first.

CREATE TABLE IF NOT EXISTS runs (
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
	duration_ns INTEGER,
	envvars TEXT,
	vars TEXT,
	includes TEXT,
	schedule TEXT,
	check_only TEXT,
	origin TEXT
);

CREATE TABLE IF NOT EXISTS servers (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	run_id TEXT,
	name TEXT,
	address TEXT,
	error TEXT,
	envvars TEXT,
	vars TEXT,
	UNIQUE(run_id, name)
);

CREATE TABLE IF NOT EXISTS tasks (
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
	envvars TEXT,
	vars TEXT,
	steps TEXT,
	UNIQUE(run_id, server, name)
);

-- each save of the Gorunfiles kept here, by name, for gorun -serve started without one
CREATE TABLE IF NOT EXISTS gorunfiles (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT,
	content TEXT NOT NULL,
	saved_at TEXT NOT NULL
);
