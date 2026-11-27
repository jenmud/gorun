-- Web UI users, with bcrypt password hashes, and settings such as the key that signs sessions.
CREATE TABLE IF NOT EXISTS users (
	name VARCHAR(64) NOT NULL PRIMARY KEY,
	password_hash VARCHAR(255) NOT NULL,
	admin INTEGER NOT NULL DEFAULT 0,
	created_at VARCHAR(64) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS settings (
	name VARCHAR(64) NOT NULL PRIMARY KEY,
	value TEXT NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
