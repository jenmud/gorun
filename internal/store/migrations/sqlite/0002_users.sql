-- Web UI users, with bcrypt password hashes, and settings such as the key that signs sessions.
CREATE TABLE IF NOT EXISTS users (
	name TEXT PRIMARY KEY,
	password_hash TEXT NOT NULL,
	admin INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS settings (
	name TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
