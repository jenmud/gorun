-- Assistant chats kept for training, when an admin turns capture on: the prompt as sent, the reply,
-- whether its tasks loaded and were saved, and the asker's rating.
CREATE TABLE IF NOT EXISTS assistant_captures (
	id TEXT PRIMARY KEY,
	kind TEXT NOT NULL,
	created_at TEXT NOT NULL,
	user_name TEXT NOT NULL DEFAULT '',
	model TEXT NOT NULL DEFAULT '',
	messages TEXT NOT NULL,
	reply TEXT NOT NULL,
	tasks TEXT NOT NULL DEFAULT '',
	task_names TEXT NOT NULL DEFAULT '',
	task_error TEXT NOT NULL DEFAULT '',
	repaired INTEGER NOT NULL DEFAULT 0,
	saved_at TEXT NOT NULL DEFAULT '',
	feedback INTEGER NOT NULL DEFAULT 0
);
