-- Player intel cache: Steam profile/ban flags and IP country/VPN flags.
-- Evidence only; nothing here kicks or bans automatically.
CREATE TABLE player_intel (
    steam64          TEXT PRIMARY KEY,
    avatar           TEXT NOT NULL DEFAULT '',
    persona          TEXT NOT NULL DEFAULT '',
    profile_private  INTEGER NOT NULL DEFAULT 0,
    limited          INTEGER NOT NULL DEFAULT 0,
    vac_banned       INTEGER NOT NULL DEFAULT 0,
    vac_bans         INTEGER,
    game_bans        INTEGER,
    days_since_ban   INTEGER,
    community_banned INTEGER NOT NULL DEFAULT 0,
    trade_ban        TEXT NOT NULL DEFAULT '',
    steam_checked_at TEXT,
    steam_error      TEXT NOT NULL DEFAULT '',
    ip               TEXT NOT NULL DEFAULT '',
    country          TEXT NOT NULL DEFAULT '',
    country_code     TEXT NOT NULL DEFAULT '',
    isp              TEXT NOT NULL DEFAULT '',
    proxy            INTEGER NOT NULL DEFAULT 0,
    hosting          INTEGER NOT NULL DEFAULT 0,
    ip_checked_at    TEXT
);

-- In-game chat and admin messages captured from BattlEye RCON (30-day retention).
CREATE TABLE chat_messages (
    id        INTEGER PRIMARY KEY,
    server_id TEXT NOT NULL,
    ts        TEXT NOT NULL,
    channel   TEXT NOT NULL,
    name      TEXT NOT NULL DEFAULT '',
    text      TEXT NOT NULL
);
CREATE INDEX chat_messages_server ON chat_messages(server_id, id);
