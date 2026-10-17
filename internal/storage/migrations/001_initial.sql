CREATE TABLE history (
    id           TEXT PRIMARY KEY,
    command      TEXT NOT NULL,
    cwd          TEXT,
    username     TEXT,
    hostname     TEXT,
    shell        TEXT,
    session_id   TEXT,
    exit_code    INTEGER,
    duration_ms  INTEGER,
    actor        TEXT NOT NULL DEFAULT 'user',
    started_at   INTEGER NOT NULL,
    dedupe_key   TEXT
);

CREATE UNIQUE INDEX idx_history_dedupe ON history(dedupe_key) WHERE dedupe_key IS NOT NULL;
CREATE INDEX idx_history_started ON history(started_at DESC);
CREATE INDEX idx_history_cwd ON history(cwd);

CREATE VIRTUAL TABLE history_fts USING fts5(
    command,
    content='history',
    content_rowid='rowid',
    tokenize='trigram'
);
