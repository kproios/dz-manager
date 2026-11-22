# Configuration

DZ Manager reads its settings from environment variables. On a Linux install they are in
`/etc/dzm/dzm.env`; with Docker, in `deploy/docker/dzm.env`. Restart DZ Manager after a change
(`systemctl restart dzm.service`, or `docker compose up -d`).

If a value is wrong, DZ Manager refuses to start and lists every problem at once. To check without
starting: `dzm check-config`.

Your **game servers** are not configured here. Add them in the panel under **Settings → Server**;
their addresses and passwords are stored encrypted in the database, so moving to another game host
never needs access to this file.

## Required

| Setting | Example | What it is |
|---|---|---|
| `DZM_SECRET_KEY` | `openssl rand -base64 32` | 32 random bytes in base64. Encrypts stored passwords and tokens. **Keep a copy in your password manager**: a backup cannot be restored without it, and changing it makes stored game-server passwords unreadable. |
| `DZM_PUBLIC_URL` | `https://dzm.example.com` | The address you open the panel at. Used for sign-in, invite links and cookies. |
| `DZM_INGEST_PUBLIC_URL` | `https://dzm-ingest.example.com` | The address the game server sends data to. May be the same as `DZM_PUBLIC_URL`. |

Both addresses must start with `https://`. Plain `http://` is only accepted for `localhost`, for
testing on your own machine.

## Common

| Setting | Default | What it is |
|---|---|---|
| `DZM_TZ` | `UTC` | Your timezone for display, for example `Europe/London` or `America/Chicago`. Data is always stored in UTC. Restart schedules carry their own timezone. |
| `DZM_STEAM_LOGIN` | `1` | `0` hides "Sign in with Steam". |
| `DZM_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error`. |
| `DZM_ALERT_WEBHOOK` | empty | A webhook address (for example ntfy or a Discord webhook) for operational alerts: server down, restart missed, update rolled back. |
| `DZM_MAP_MAX_PX` | `10000` | Largest map image to tile, in pixels per side (1024–20000). Tiling uses about 4 bytes per pixel of memory for a few seconds (7,616 px ≈ 270 MB). |

## Optional single sign-on

Leave these empty unless you want a "Single sign-on" button for an OpenID Connect provider you
already run. Set all three or none. The provider only proves who someone is; their role is still
decided in DZ Manager, and the account must be invited first. See [Accounts](accounts.md).

| Setting | What it is |
|---|---|
| `DZM_OIDC_ISSUER` | The provider's issuer address (https) |
| `DZM_OIDC_CLIENT_ID` | Client ID of a confidential client with redirect URI `<DZM_PUBLIC_URL>/auth/callback` |
| `DZM_OIDC_CLIENT_SECRET` | Its client secret |
| `DZM_OIDC_LABEL` | Text on the button (default "Single sign-on", at most 40 characters) |

## Set by the installer or the Docker image

You normally leave these alone.

| Setting | Default | What it is |
|---|---|---|
| `DZM_LISTEN` | `127.0.0.1:8080` | Where the program listens. Must stay on the local machine: HTTPS comes from a tunnel or reverse proxy in front. |
| `DZM_DATA_DIR` | `/var/lib/dzm` | Database, map tiles and archives (`/data` in Docker). |
| `DZM_CONTAINER` | empty | Set to `1` by the Docker image so the program may listen inside the container. |
| `DZM_BACKUP_PING_URL` | empty | Linux install only: an address the daily backup calls after it succeeds, for an external "did the backup run?" monitor. |

## Settings in the panel

Everything else is configured in the browser: game servers and the bridge (**Settings → Server**),
player checks and the priority queue (**Players**), tracked events and map icons
(**Map & tracking**), in-game messages (**Messages**), API keys, MCP clients and Discord
(**Integrations**), and data retention (**Data**). The restart schedule is under **Restarts**.
