-- API keys for the public API and the MCP server. Only a keyed hash of the
-- token is stored; scopes are space-separated (see internal/apikeys).
CREATE TABLE api_keys (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    prefix       TEXT NOT NULL,          -- first characters, to recognise a key
    hash         TEXT NOT NULL UNIQUE,
    scopes       TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    created_by   TEXT NOT NULL,
    expires_at   TEXT,
    last_used_at TEXT,
    revoked_at   TEXT
);
