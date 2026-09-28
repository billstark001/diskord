PRAGMA foreign_keys=ON;
CREATE TABLE IF NOT EXISTS schema_version(version INTEGER NOT NULL);
INSERT INTO schema_version(version) SELECT 1 WHERE NOT EXISTS (SELECT 1 FROM schema_version);
CREATE TABLE IF NOT EXISTS users(
 id TEXT PRIMARY KEY, username TEXT, display_name TEXT, avatar TEXT,
 observed_at INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS guilds(
 id TEXT PRIMARY KEY, name TEXT, unavailable INTEGER NOT NULL DEFAULT 0,
 deleted INTEGER NOT NULL DEFAULT 0, observed_at INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS channels(
 id TEXT PRIMARY KEY, guild_id TEXT REFERENCES guilds(id), parent_id TEXT REFERENCES channels(id),
 name TEXT, kind INTEGER, deleted INTEGER NOT NULL DEFAULT 0, observed_at INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS guild_members(
 guild_id TEXT NOT NULL REFERENCES guilds(id), user_id TEXT NOT NULL REFERENCES users(id),
 PRIMARY KEY(guild_id,user_id)
);
CREATE TABLE IF NOT EXISTS channel_recipients(
 channel_id TEXT NOT NULL REFERENCES channels(id), user_id TEXT NOT NULL REFERENCES users(id),
 PRIMARY KEY(channel_id,user_id)
);
CREATE TABLE IF NOT EXISTS messages(
 id TEXT PRIMARY KEY, sort_key TEXT NOT NULL UNIQUE,
 channel_id TEXT REFERENCES channels(id), author_id TEXT REFERENCES users(id),
 content TEXT, created_at TEXT, edited_at TEXT, kind INTEGER,
 deleted INTEGER NOT NULL DEFAULT 0, revision INTEGER NOT NULL DEFAULT 0,
 observed_at INTEGER NOT NULL DEFAULT 0, source TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS messages_channel_sort ON messages(channel_id,sort_key DESC);
CREATE INDEX IF NOT EXISTS channels_guild ON channels(guild_id);
CREATE TABLE IF NOT EXISTS attachments(
 id TEXT PRIMARY KEY, message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
 name TEXT NOT NULL, host TEXT NOT NULL, path TEXT NOT NULL, size INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS attachments_message ON attachments(message_id);
CREATE TABLE IF NOT EXISTS assets(
 hash TEXT PRIMARY KEY, extension TEXT NOT NULL, mime TEXT NOT NULL, size INTEGER NOT NULL,
 observed_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS resource_urls(
 host TEXT NOT NULL, path TEXT NOT NULL, hash TEXT NOT NULL REFERENCES assets(hash),
 PRIMARY KEY(host,path)
);
