# Contract: control-plane host ⇄ DZ Manager release

Status: **binding** for infrastructure work (#21)
and application work (#3). Changing it means updating this file in the
same PR as the code that changes it.

This file is the only thing the two sides have to agree on. The host does not need to know how
the app works, and the app does not need to know how the host was built.

## 1. Release artifacts (produced by GitHub Actions in this repo)

| Item | Value |
|---|---|
| Trigger | Push of a git tag `vMAJOR.MINOR.PATCH` (semver, no suffix) on `main`. |
| Deployable release | A **published, non-draft, non-prerelease** GitHub Release for that tag. Pre-releases (`v1.2.3-rc.1`) are never auto-installed. |
| Assets | `dzm_linux_amd64.tar.gz`, `dzm_linux_arm64.tar.gz`, `SHA256SUMS` (exact names, no version in the file name). The updater ignores any other assets. |
| Game-side asset | `DZMBridge.zip`: the server-side mod folder `@DZMBridge/` (`addons/DZMBridge.pbo`, `mod.cpp`), built by CI. **Unsigned** (server-side mods loaded with `-servermod` need no `.bisign`). The owner installs it on the game server by hand. The host updater never touches it. |
| Tarball layout | `dzm` (static binary, CGO disabled), `VERSION` (single line, equals the tag, e.g. `v0.3.1`), `LICENSES/` (third-party notices). Nothing else is required. |
| `SHA256SUMS` | `sha256sum` format, one line per tarball. |

No signing key. Integrity comes from HTTPS + an authenticated download from a private repo +
`SHA256SUMS`. A signing key would live in the same GitHub account, so it would add work without
adding protection.

## 2. Binary command line

| Command | Behaviour |
|---|---|
| `dzm version` | Prints the version (same as `VERSION`) and exits 0. Must not touch the DB. |
| `dzm serve` | Runs the HTTP server. Applies pending DB migrations at startup, before `/ready` returns 200. |
| `dzm check-config` | Validates the environment and the data directory, then exits 0 or 1. Use it for preflight. |

## 3. Environment (`/etc/dzm/dzm.env`, mode 0640 root:dzm)

| Variable | Required | Default | Meaning |
|---|---|---|---|
| `DZM_LISTEN` | no | `127.0.0.1:8080` | Listener. Must stay on loopback. Ingress is only through cloudflared. |
| `DZM_DATA_DIR` | no | `/var/lib/dzm` | SQLite DB, tiles, backups, updater status. |
| `DZM_SECRET_KEY` | **yes** | – | 32-byte base64 key that encrypts stored game-server secrets (RCON password, ingest tokens) at rest. Missing or invalid key: refuse to start. **The owner keeps a copy in their password manager, because a DB backup is useless without it.** |
| `DZM_PUBLIC_URL` | yes | – | `https://dzm.example.com`. Used for the OIDC redirect URI (`<url>/auth/callback`), cookie scope and CSRF origin checks. |
| `DZM_INGEST_PUBLIC_URL` | yes | – | `https://dzm-ingest.example.com` (shown to the operator when configuring the game-side mods). |
| `DZM_OIDC_ISSUER` | **yes** | – | Authentik issuer URL of the DZ Manager provider, e.g. `https://<authentik-host>/application/o/dz-manager/`. Must be reachable from the host. |
| `DZM_OIDC_CLIENT_ID` | **yes** | – | Authentik OAuth2/OIDC provider client ID. |
| `DZM_OIDC_CLIENT_SECRET` | **yes** | – | Authentik confidential-client secret. |
| `DZM_OIDC_ADMIN_GROUP` | no | `dzm-admins` | Authentik group whose members may sign in. Everyone else is denied. |
| `DZM_TZ` | no | `Europe/Athens` | Display and schedule timezone. Everything is stored in UTC. |
| `DZM_LOG_LEVEL` | no | `info` | `debug` / `info` / `warn` / `error`. JSON logs go to stdout (journald). |
| `DZM_ALERT_WEBHOOK` | no | – | Optional ntfy/Discord-compatible URL for operational alerts. |

Game-server connection details (host, ports, RCON password, ingest tokens) are **not** environment
variables. They are server profiles configured in the UI and stored encrypted in the DB, so
moving the game server to another provider does not need host access.

## 4. HTTP endpoints the host relies on

| Path | Auth | Contract |
|---|---|---|
| `GET /health` | none | 200 as soon as the process serves HTTP. Liveness only. |
| `GET /ready` | none | 200 + `{"status":"ready","version":"vX.Y.Z"}` once the DB is open and migrated. 503 otherwise. The updater checks that `version` equals the tag it installed. |
| `/ingest/…` | per-server credentials (Basic or token) | Game → DZ Manager telemetry and command polling. It is the **only** path published on the ingest hostname. |
| `/auth/login`, `/auth/callback`, `/auth/logout` | none | OIDC authorization-code + PKCE flow against Authentik. |
| everything else | session created **only** by a successful Authentik login | UI, API, SSE. There are no local passwords. |

`/health` and `/ready` expose no secrets and no game data.

## 5. Filesystem layout (owned by the host)

```
/opt/dzm/releases/<tag>/dzm      immutable release directories (keep the last 3)
/opt/dzm/current -> releases/<tag>
/etc/dzm/dzm.env                 secrets and env (0640 root:dzm)
/etc/dzm/updater.env             GitHub read-only token for the updater (0600 root:root)
/var/lib/dzm/                    0700 dzm:dzm, the app's only writable path
  dzm.db (+ -wal, -shm)          SQLite in WAL mode
  tiles/                         map tiles (written by the app or a one-off loader)
  backups/                       daily + pre-update snapshots (written by host timers)
  updater/status.json            last update attempt (written by the updater, read by the app)
```

`updater/status.json` schema (the app shows it on its status page, read-only):

```json
{
  "checked_at": "2026-11-04T10:00:00Z",
  "installed": "v0.3.1",
  "latest_seen": "v0.3.2",
  "last_result": "ok | rolled_back | download_failed | verify_failed | skipped_hold | skipped_blocked",
  "last_error": "",
  "blocked_versions": ["v0.3.2"],
  "token_expires_at": "2027-11-01T00:00:00Z"
}
```

## 6. Database rules the app must follow (so updater rollback stays safe)

- SQLite, WAL mode, single writer: the `dzm serve` process.
- Migrations are forward-only, embedded in the binary, and applied in a transaction at startup.
- If the DB schema version is **newer** than the binary knows, refuse to start and keep `/ready` at 503.
- The host snapshots the DB before every update and restores it on rollback. Telemetry written
  in the seconds between update and rollback may be lost. That is acceptable.
