CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,                          -- "gh:{userId}" or "google:{sub}"
    user_id INTEGER NOT NULL UNIQUE,
    email TEXT NOT NULL DEFAULT '',
    display_name TEXT NOT NULL DEFAULT '',
    first_name TEXT NOT NULL DEFAULT '',
    last_name TEXT NOT NULL DEFAULT '',
    avatar_url TEXT NOT NULL DEFAULT '',
    slug TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS providers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id TEXT NOT NULL REFERENCES users(id),
    provider TEXT NOT NULL,
    provider_user_id TEXT NOT NULL,
    provider_login TEXT NOT NULL DEFAULT '',
    email TEXT NOT NULL DEFAULT '',
    linked_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    UNIQUE(user_id, provider)
);

CREATE TABLE IF NOT EXISTS sync_blobs (
    kind TEXT PRIMARY KEY,
    content_hash TEXT NOT NULL,
    payload TEXT NOT NULL,           -- base64 encoded
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
