-- Accounts owned by DZ Manager: roles, built-in password + TOTP sign-in,
-- Steam and (optional) OpenID Connect as linked sign-in methods, invites,
-- recovery codes. Users that existed before were all full admins: they
-- become owners and keep signing in through OpenID Connect.
ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'owner';
ALTER TABLE users ADD COLUMN login TEXT;                              -- lower-case name for password sign-in
ALTER TABLE users ADD COLUMN password_hash TEXT NOT NULL DEFAULT '';  -- argon2id, PHC string
ALTER TABLE users ADD COLUMN totp_secret TEXT NOT NULL DEFAULT '';    -- sealed with the secret key
ALTER TABLE users ADD COLUMN totp_last_step INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN steam64 TEXT;
ALTER TABLE users ADD COLUMN oidc_sub TEXT;
ALTER TABLE users ADD COLUMN disabled_at TEXT;
ALTER TABLE users ADD COLUMN failed_logins INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN locked_until INTEGER NOT NULL DEFAULT 0;
UPDATE users SET oidc_sub = sub;
CREATE UNIQUE INDEX users_login ON users(login) WHERE login IS NOT NULL;
CREATE UNIQUE INDEX users_steam64 ON users(steam64) WHERE steam64 IS NOT NULL;
CREATE UNIQUE INDEX users_oidc_sub ON users(oidc_sub) WHERE oidc_sub IS NOT NULL;

CREATE TABLE recovery_codes (
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    hash    TEXT NOT NULL,
    used_at TEXT,
    PRIMARY KEY (user_id, hash)
);

CREATE TABLE invites (
    id         TEXT PRIMARY KEY,
    hash       TEXT NOT NULL UNIQUE,   -- keyed hash of the invite token
    role       TEXT NOT NULL,
    note       TEXT NOT NULL DEFAULT '',
    created_by TEXT NOT NULL,
    created_at TEXT NOT NULL,
    expires_at INTEGER NOT NULL,       -- unix seconds
    used_at    TEXT,
    used_by    INTEGER REFERENCES users(id) ON DELETE SET NULL
);

ALTER TABLE sessions ADD COLUMN method TEXT NOT NULL DEFAULT 'oidc';
ALTER TABLE sessions ADD COLUMN mfa_at INTEGER NOT NULL DEFAULT 0;   -- last 2FA confirmation (unix seconds)
ALTER TABLE sessions ADD COLUMN ip TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN user_agent TEXT NOT NULL DEFAULT '';
-- Everyone signs in again: sessions without a second factor end here.
DELETE FROM sessions;

-- An API key can never do more than the user who created it.
ALTER TABLE api_keys ADD COLUMN user_id INTEGER REFERENCES users(id) ON DELETE CASCADE;
UPDATE api_keys SET user_id = (SELECT MIN(id) FROM users);
