-- #6 active world events and historical rare-item spawns.
CREATE TABLE map_events (
    id           INTEGER PRIMARY KEY,
    server_id    TEXT NOT NULL,
    key          TEXT NOT NULL,
    category     TEXT NOT NULL,
    class        TEXT NOT NULL DEFAULT '',
    x REAL, y REAL, z REAL,
    started_at   TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    expires_at   TEXT NOT NULL,
    ended_at     TEXT,
    status       TEXT NOT NULL CHECK (status IN ('active','ended','expired','historical')),
    source       TEXT NOT NULL,
    data         TEXT NOT NULL DEFAULT '{}',
    UNIQUE (server_id, key)
);
CREATE INDEX map_events_status ON map_events(server_id, status);

-- #10 local bans (BattlEye-enforced) and external lists (review only).
CREATE TABLE bans (
    id          INTEGER PRIMARY KEY,
    steam64     TEXT NOT NULL DEFAULT '',
    be_guid     TEXT NOT NULL,
    reason      TEXT NOT NULL,
    created_at  TEXT NOT NULL,
    expires_at  TEXT,
    issuer_sub  TEXT NOT NULL,
    issuer_name TEXT NOT NULL,
    revoked_at  TEXT,
    revoked_by  TEXT NOT NULL DEFAULT '',
    applied     TEXT NOT NULL
);
CREATE INDEX bans_guid ON bans(be_guid);

CREATE TABLE external_lists (
    id          INTEGER PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    source      TEXT NOT NULL DEFAULT '',
    imported_at TEXT NOT NULL,
    entries     INTEGER NOT NULL
);
CREATE TABLE external_entries (
    list_id         INTEGER NOT NULL REFERENCES external_lists(id) ON DELETE CASCADE,
    identifier      TEXT NOT NULL,
    identifier_type TEXT NOT NULL CHECK (identifier_type IN ('steam64','be_guid')),
    reason          TEXT NOT NULL DEFAULT '',
    listed_at       TEXT NOT NULL DEFAULT ''
);
CREATE INDEX external_entries_ident ON external_entries(identifier_type, identifier);
CREATE TABLE external_reviews (
    steam64  TEXT PRIMARY KEY,
    decision TEXT NOT NULL,
    note     TEXT NOT NULL DEFAULT '',
    reviewer TEXT NOT NULL,
    at       TEXT NOT NULL
);
