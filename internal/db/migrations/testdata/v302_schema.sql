CREATE TABLE settings (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE api_tokens (
  token_hash TEXT PRIMARY KEY,
  user_id    TEXT NOT NULL,
  issued_at  TEXT NOT NULL,
  last_used  TEXT,
  revoked_at TEXT
);
CREATE INDEX idx_api_tokens_user ON api_tokens(user_id);

CREATE TABLE allowed_users (
  user_id    TEXT PRIMARY KEY,
  source     TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE pending_users (
  user_id      TEXT PRIMARY KEY,
  username     TEXT NOT NULL,
  requested_at TEXT NOT NULL,
  decided_at   TEXT,
  decision     TEXT
);
CREATE INDEX idx_pending_users_decision ON pending_users(decision);

CREATE TABLE scheduled_tasks (
  id                     INTEGER PRIMARY KEY AUTOINCREMENT,
  name                   TEXT NOT NULL UNIQUE,
  kind                   TEXT NOT NULL,
  payload                TEXT NOT NULL DEFAULT '',
  recipient_id           TEXT NOT NULL DEFAULT '',
  schedule_kind          TEXT NOT NULL,
  schedule_at            TEXT,
  schedule_daily         TEXT,
  schedule_every_minutes INTEGER NOT NULL DEFAULT 0,
  next_run_at            TEXT NOT NULL,
  last_run_at            TEXT,
  last_error             TEXT NOT NULL DEFAULT '',
  status                 TEXT NOT NULL DEFAULT 'active',
  created_at             TEXT NOT NULL,
  updated_at             TEXT NOT NULL
);
CREATE INDEX idx_scheduled_tasks_due ON scheduled_tasks(status, next_run_at);

CREATE TABLE conversations (
  id                INTEGER PRIMARY KEY AUTOINCREMENT,
  chat_id           INTEGER NOT NULL,
  user_id           INTEGER NOT NULL,
  turn_index        INTEGER NOT NULL,
  role              TEXT NOT NULL,
  content           TEXT NOT NULL,
  tool_calls        TEXT,
  tool_call_id      TEXT,
  llm_calls         INTEGER NOT NULL DEFAULT 0,
  tool_calls_count  INTEGER NOT NULL DEFAULT 0,
  elapsed_ms        INTEGER NOT NULL DEFAULT 0,
  tokens_in         INTEGER NOT NULL DEFAULT 0,
  tokens_out        INTEGER NOT NULL DEFAULT 0,
  created_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(chat_id, turn_index)
);
CREATE INDEX idx_conv_chat ON conversations(chat_id, turn_index);
CREATE INDEX idx_conv_user ON conversations(user_id, created_at);

CREATE TABLE proposed_updates (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  chat_id         INTEGER NOT NULL,
  fact            TEXT NOT NULL,
  action          TEXT NOT NULL,
  target_slug     TEXT NOT NULL DEFAULT '',
  similarity      REAL NOT NULL DEFAULT 0,
  source_turn_ids TEXT NOT NULL DEFAULT '',
  status          TEXT NOT NULL DEFAULT 'pending',
  created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE wiki_issues (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  kind        TEXT NOT NULL,
  severity    TEXT NOT NULL,
  slug        TEXT NOT NULL DEFAULT '',
  broken_link TEXT NOT NULL DEFAULT '',
  message     TEXT NOT NULL DEFAULT '',
  status      TEXT NOT NULL DEFAULT 'open',
  created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  resolved_at DATETIME
);
CREATE UNIQUE INDEX idx_wiki_issues_key ON wiki_issues(kind, slug, broken_link);

CREATE TABLE embedding_cache (
  content_sha TEXT NOT NULL,
  model       TEXT NOT NULL,
  embedding   BLOB NOT NULL,
  created_at  TIMESTAMP NOT NULL,
  PRIMARY KEY (content_sha, model)
);

CREATE VIRTUAL TABLE wiki_documents
USING fts5(id, content, metadata, title);

CREATE TABLE swarm_runs (
  id           TEXT PRIMARY KEY,
  goal         TEXT NOT NULL,
  status       TEXT NOT NULL,
  created_by   TEXT NOT NULL,
  created_at   TEXT NOT NULL,
  updated_at   TEXT NOT NULL,
  completed_at TEXT,
  last_error   TEXT NOT NULL DEFAULT ''
);

CREATE TABLE swarm_tasks (
  id             TEXT PRIMARY KEY,
  run_id         TEXT NOT NULL,
  parent_id      TEXT NOT NULL DEFAULT '',
  role           TEXT NOT NULL,
  subject        TEXT NOT NULL,
  prompt         TEXT NOT NULL,
  tool_allowlist TEXT NOT NULL DEFAULT '[]',
  status         TEXT NOT NULL,
  depth          INTEGER NOT NULL DEFAULT 0,
  attempts       INTEGER NOT NULL DEFAULT 0,
  blocked_by     TEXT NOT NULL DEFAULT '[]',
  result         TEXT NOT NULL DEFAULT '',
  tool_calls     INTEGER NOT NULL DEFAULT 0,
  llm_calls      INTEGER NOT NULL DEFAULT 0,
  elapsed_ms     INTEGER NOT NULL DEFAULT 0,
  created_at     TEXT NOT NULL,
  started_at     TEXT,
  completed_at   TEXT,
  last_error     TEXT NOT NULL DEFAULT '',
  FOREIGN KEY(run_id) REFERENCES swarm_runs(id)
);
CREATE INDEX idx_swarm_tasks_run ON swarm_tasks(run_id, status, created_at);
CREATE INDEX idx_swarm_tasks_status ON swarm_tasks(status, created_at);
