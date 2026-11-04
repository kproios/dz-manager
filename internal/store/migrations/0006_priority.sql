-- #13 priority queue, phase 1: DZ Manager list only; the provider applies it.
CREATE TABLE priority (
    steam64    TEXT PRIMARY KEY,
    reason     TEXT NOT NULL DEFAULT '',
    expires_at TEXT,
    applied    INTEGER NOT NULL DEFAULT 0,
    added_by   TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
