-- #3 App foundation
CREATE TABLE users (
    id            INTEGER PRIMARY KEY,
    sub           TEXT NOT NULL UNIQUE,      -- account identity
    username      TEXT NOT NULL,
    email         TEXT NOT NULL DEFAULT '',
    created_at    TEXT NOT NULL,
    last_login_at TEXT NOT NULL
);

CREATE TABLE sessions (
    id_hash      TEXT PRIMARY KEY,           -- keyed hash of the cookie token
    user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    csrf         TEXT NOT NULL,
    id_token     TEXT NOT NULL DEFAULT '',   -- for RP-initiated logout (id_token_hint)
    created_at   INTEGER NOT NULL,           -- unix seconds
    last_seen_at INTEGER NOT NULL,
    expires_at   INTEGER NOT NULL
);
CREATE INDEX sessions_expires ON sessions(expires_at);

CREATE TABLE audit_log (
    id         INTEGER PRIMARY KEY,
    ts         TEXT NOT NULL,
    actor_sub  TEXT NOT NULL,
    actor_name TEXT NOT NULL,
    action     TEXT NOT NULL,
    target     TEXT NOT NULL DEFAULT '',
    params     TEXT NOT NULL DEFAULT '{}',
    result     TEXT NOT NULL,                -- ok | error | denied
    detail     TEXT NOT NULL DEFAULT ''
);
CREATE INDEX audit_ts ON audit_log(ts);
CREATE TRIGGER audit_no_update BEFORE UPDATE ON audit_log BEGIN SELECT RAISE(ABORT, 'audit_log is append-only'); END;
CREATE TRIGGER audit_no_delete BEFORE DELETE ON audit_log BEGIN SELECT RAISE(ABORT, 'audit_log is append-only'); END;

CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- #20 server profiles. Secrets are AES-GCM sealed (secret.Box) with the row id as AAD.
CREATE TABLE server_profiles (
    id                TEXT PRIMARY KEY,      -- stable server_id (UUID), never derived from host
    name              TEXT NOT NULL,
    provider_kind     TEXT NOT NULL CHECK (provider_kind IN ('selfhosted_architect','vykix','generic')),
    active            INTEGER NOT NULL DEFAULT 0,
    archived          INTEGER NOT NULL DEFAULT 0,
    game_host         TEXT NOT NULL DEFAULT '',
    game_port         INTEGER NOT NULL DEFAULT 2302,
    query_host        TEXT NOT NULL DEFAULT '',
    query_port        INTEGER NOT NULL DEFAULT 27016,
    rcon_host         TEXT NOT NULL DEFAULT '',
    rcon_port         INTEGER NOT NULL DEFAULT 2305,
    secrets           BLOB,                  -- sealed JSON {rcon_password, metricz_password, bridge_token}
    metricz_user      TEXT NOT NULL UNIQUE,
    metricz_pass_hash TEXT NOT NULL,
    bridge_token_hash TEXT NOT NULL UNIQUE,
    created_at        TEXT NOT NULL,
    updated_at        TEXT NOT NULL
);
CREATE UNIQUE INDEX server_profiles_one_active ON server_profiles(active) WHERE active = 1;
