# Architecture of DZ Manager

## Deployment boundary

DZ Manager runs separately from the game server. Core features use BattlEye RCON,
A2S query and outbound HTTPS from server-side mods. They do not require game-host
shell access, file scraping or sidecar executables. This supports self-hosted
servers and managed game panels through the same server-profile interface.

The panel is a static Go binary with an embedded React UI and a SQLite database
in WAL mode. A reverse proxy or outbound tunnel terminates public HTTPS. A separate
ingest hostname may expose only `/ingest/*`. Per-server credentials protect ingest.
Server-Sent Events deliver live browser updates. Native Linux and Docker installs
share the same application and data format.

## Accounts and data

Password and Steam sign-in are built in; OpenID Connect is optional. TOTP is
optional per account and, when enabled, follows every sign-in method. Roles,
invites and route permissions stay in DZ Manager. Sensitive actions require
recent verification for accounts with TOTP enabled. Sessions use server-side
cookies and CSRF origin checks. Stored connection and TOTP secrets are encrypted
with `DZM_SECRET_KEY`. See [accounts](../accounts.md).

## Game-side sources

- `@DZMBridge` provides live positions, events and commands through outbound HTTPS.
  It is an unsigned server-only PBO, installed separately from the panel.
- MetricZ runs upstream and unmodified as an optional source of performance data.
- BattlEye RCON and A2S provide status and supported native administration actions.
- Map names, dimensions and places come from `CfgWorlds`. Terrain images are
  downloaded and tiled on the operator's host; they are not redistributed.
- Optional mod integrations use available APIs without copying proprietary source.

Every feature declares its required capability. The UI disables unavailable
features. The hosting panel or launcher owns process lifecycle and restart
scheduling; DZ Manager observes restarts and can send countdowns. RCON-driven
shutdown requires an explicitly enabled capability and a host that restarts the
exited game process.

## Licensing and updates

MIT/BSD/Apache code may be imported or ported with its required notice. Upstream
GPL integrations remain separate and unmodified. Proprietary mod source is not
included. Bridge HTTP scaffolding is derived from DayZ-Takaro-Integration with its
MIT notice preserved in `LICENSES/`.

Releases embed the UI and migrations. The host updater verifies downloads,
snapshots the database and can roll back a failed update. Game-side mod updates
are installed separately. See the [host and release contract](../contracts/host-and-release.md).
