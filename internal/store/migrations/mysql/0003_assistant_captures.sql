-- Assistant chats kept for training, when an admin turns capture on: the prompt as sent, the reply,
-- whether its tasks loaded and were saved, and the asker's rating.
CREATE TABLE IF NOT EXISTS assistant_captures (
	id VARCHAR(32) NOT NULL PRIMARY KEY,
	kind VARCHAR(16) NOT NULL,
	created_at VARCHAR(64) NOT NULL,
	user_name VARCHAR(64) NOT NULL DEFAULT '',
	model VARCHAR(255) NOT NULL DEFAULT '',
	messages LONGTEXT NOT NULL,
	reply LONGTEXT NOT NULL,
	tasks LONGTEXT NOT NULL,
	task_names TEXT NOT NULL,
	task_error TEXT NOT NULL,
	repaired INTEGER NOT NULL DEFAULT 0,
	saved_at VARCHAR(64) NOT NULL DEFAULT '',
	feedback INTEGER NOT NULL DEFAULT 0
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
