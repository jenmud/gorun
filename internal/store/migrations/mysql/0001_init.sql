-- The history tables. Times are RFC 3339 text and JSON columns are text, as in SQLite, so the
-- same queries work on every database. Key columns are VARCHAR, since MySQL can't index TEXT
-- whole; output and JSON are LONGTEXT, since TEXT stops at 64 KB.

CREATE TABLE IF NOT EXISTS runs (
	id VARCHAR(255) NOT NULL PRIMARY KEY,
	title TEXT,
	hostname TEXT,
	pid BIGINT,
	started_at VARCHAR(64),
	finished_at VARCHAR(64),
	success INTEGER,
	total_tasks INTEGER,
	failed INTEGER,
	skipped INTEGER,
	duration_ns BIGINT,
	envvars LONGTEXT,
	vars LONGTEXT,
	includes LONGTEXT,
	schedule TEXT,
	check_only TEXT,
	origin LONGTEXT,
	INDEX runs_started_at (started_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS servers (
	id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
	run_id VARCHAR(255),
	name VARCHAR(255),
	address TEXT,
	error LONGTEXT,
	envvars LONGTEXT,
	vars LONGTEXT,
	UNIQUE (run_id, name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS tasks (
	id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
	run_id VARCHAR(255),
	server VARCHAR(255),
	name VARCHAR(255),
	state INTEGER,
	command LONGTEXT,
	stdout LONGTEXT,
	stderr LONGTEXT,
	exit_code INTEGER,
	duration_ns BIGINT,
	envvars LONGTEXT,
	vars LONGTEXT,
	steps LONGTEXT,
	UNIQUE (run_id, server, name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- each save of the Gorunfiles kept here, by name, for gorun -serve started without one
CREATE TABLE IF NOT EXISTS gorunfiles (
	id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
	name VARCHAR(255),
	content LONGTEXT NOT NULL,
	saved_at VARCHAR(64) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
