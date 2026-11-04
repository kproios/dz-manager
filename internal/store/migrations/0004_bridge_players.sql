-- #16 bridge event store (90-day retention) and command queue.
CREATE TABLE events (
    id          INTEGER PRIMARY KEY,
    server_id   TEXT NOT NULL,
    event_id    TEXT NOT NULL,
    type        TEXT NOT NULL,
    ts          TEXT NOT NULL,
    received_at TEXT NOT NULL,
    source      TEXT NOT NULL,
    actor       TEXT NOT NULL DEFAULT '',
    target      TEXT NOT NULL DEFAULT '',
    x REAL, y REAL, z REAL,
    data        TEXT NOT NULL DEFAULT '{}',
    known       INTEGER NOT NULL DEFAULT 1,
    UNIQUE (server_id, event_id)
);
CREATE INDEX events_server_ts ON events(server_id, ts);
CREATE INDEX events_type ON events(server_id, type, ts);

CREATE TABLE bridge_commands (
    id           TEXT PRIMARY KEY,
    server_id    TEXT NOT NULL,
    action       TEXT NOT NULL,
    target       TEXT NOT NULL DEFAULT '',
    params       TEXT NOT NULL DEFAULT '{}',
    status       TEXT NOT NULL CHECK (status IN ('queued','delivered','ok','error','expired')),
    created_at   TEXT NOT NULL,
    expires_at   TEXT NOT NULL,
    delivered_at TEXT,
    finished_at  TEXT,
    result       TEXT,
    error        TEXT NOT NULL DEFAULT '',
    actor_sub    TEXT NOT NULL,
    actor_name   TEXT NOT NULL
);
CREATE INDEX bridge_commands_queue ON bridge_commands(server_id, status, created_at);

-- #11 player identities, names, sessions and combat.
CREATE TABLE players (
    steam64      TEXT PRIMARY KEY,
    be_guid      TEXT NOT NULL DEFAULT '',
    dayz_id      TEXT NOT NULL DEFAULT '',
    name         TEXT NOT NULL DEFAULT '',
    first_seen   TEXT NOT NULL,
    last_seen    TEXT NOT NULL,
    playtime_s   INTEGER NOT NULL DEFAULT 0,
    kills        INTEGER NOT NULL DEFAULT 0,
    deaths       INTEGER NOT NULL DEFAULT 0,
    id_mismatch  INTEGER NOT NULL DEFAULT 0   -- reported BE GUID disagreed with dzid(steam64)
);
CREATE INDEX players_guid ON players(be_guid);
CREATE INDEX players_name ON players(name);

CREATE TABLE player_names (
    steam64    TEXT NOT NULL,
    name       TEXT NOT NULL,
    first_seen TEXT NOT NULL,
    last_seen  TEXT NOT NULL,
    PRIMARY KEY (steam64, name)
);

CREATE TABLE player_sessions (
    id         INTEGER PRIMARY KEY,
    server_id  TEXT NOT NULL,
    steam64    TEXT NOT NULL,
    started_at TEXT NOT NULL,
    ended_at   TEXT,
    last_seen  TEXT NOT NULL,
    end_kind   TEXT NOT NULL DEFAULT 'open' CHECK (end_kind IN ('open','disconnect','inferred_end','presence_end')),
    source     TEXT NOT NULL
);
CREATE INDEX player_sessions_player ON player_sessions(steam64, started_at);
CREATE INDEX player_sessions_open ON player_sessions(server_id, end_kind);

CREATE TABLE combat (
    id         INTEGER PRIMARY KEY,
    server_id  TEXT NOT NULL,
    event_id   TEXT NOT NULL,
    ts         TEXT NOT NULL,
    victim     TEXT NOT NULL,
    killer     TEXT NOT NULL DEFAULT '',
    weapon     TEXT NOT NULL DEFAULT '',
    distance_m REAL,
    cause      TEXT NOT NULL DEFAULT '',
    vx REAL, vz REAL, kx REAL, kz REAL,
    UNIQUE (server_id, event_id)
);
CREATE INDEX combat_victim ON combat(victim, ts);
CREATE INDEX combat_killer ON combat(killer, ts);
