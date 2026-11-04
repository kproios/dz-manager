# Game-server provider capabilities

Moving a server between hosts is a profile change. Verify capabilities on the
target host before enabling dependent features.

| Channel | Used for | Check |
|---|---|---|
| A2S query | Status and player count | The assigned query port is reachable; cached query replies do not prove the game is joinable. |
| BattlEye RCON | Roster, messages, kick, lock and bans | The assigned port is reachable from the panel host. |
| Outbound HTTPS via DayZ `RestApi` | Bridge and MetricZ telemetry, command polling | The game can reach the configured ingest URL without an interactive challenge. |
| Server-side PBO installation | Bridge and MetricZ | The launcher supports `-servermod` and profile JSON configuration. |
| Host scheduler | Game process lifecycle | Mirror its timezone and schedule in DZ Manager. |

## Connect a server

1. Create a server profile with its game, query and RCON endpoints.
2. Install the server-side mods and paste the configurations from **Connect…**.
3. Check the `rcon`, `a2s`, `metricz` and `bridge` capability badges.
4. Mirror the hosting panel's restart schedule and observe a planned restart.
5. Activate the profile. Archive an old profile to retain history and disable its ingest credentials.

Players and audit history use player identities across server profiles. Reconcile
local bans with the new server's BattlEye ban list after moving.

## RCON security

BattlEye RCON uses unencrypted UDP. Use a unique password and restrict the RCON
port to the control-plane host where the provider supports an allowlist.
Prefer HTTPS-backed bridge actions when available.

## Priority queue

The launcher or hosting panel applies queue priority. DZ Manager maintains and
exports the list through Players → Priority queue or `GET /public/v1/priority`
with a `priority:read` API key. See [API access](api.md).
