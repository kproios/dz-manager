# Game hosts

DZ Manager works with rented game servers and self-hosted ones alike. It only uses what every
DayZ server offers, plus one server-side mod.

## What the game server must allow

| Needed | Used for | How to check |
|---|---|---|
| Server query (A2S) | Status, map, version, player count | The query port is reachable from the DZ Manager machine. A cached reply from a host's proxy does not prove the server is joinable. |
| BattlEye RCON | Player list, messages, kicks, lock, bans, restarts | RCON is enabled and its port is reachable from the DZ Manager machine (some hosts need its IP on an allowlist). |
| Server-side mods (`-servermod`) | Loading @DZMBridge | The host lets you add server-side mods and edit files under `profiles/`. |
| Outgoing HTTPS from the game server | The bridge sending data and fetching commands | The game server can reach your ingest address with no browser challenge in between. |
| Automatic start after a shutdown | Restarts sent by DZ Manager | The host's panel or your launcher starts the server again when it stops. |

When you add a server, choose the type that fits under **Settings → Server → Add server**:
self-hosted, a managed host panel without SSH, or another host. It only changes the hints the
panel shows.

## Connecting a server

1. **Settings → Server → Add server** with the game, query and RCON addresses and the RCON
   password.
2. Install @DZMBridge and paste the configuration from **Connect…**, see
   [Game server setup](install-game-side.md).
3. Check the badges on the dashboard: query, RCON and bridge should be green.
4. Under **Restarts**, copy your host's restart schedule and timezone, then watch the next planned
   restart appear in the history as completed.

## Moving to another host

Moving is a change of server profile, not a reinstall:

1. Add the new server as another profile and install the bridge there with the new
   **Connect…** configuration.
2. When it is ready, **Make active** the new profile. Every page, the map and all actions switch to
   it.
3. **Archive** the old profile. Its history is kept and its ingest credentials stop working.

Player history follows players across profiles. Compare DZ Manager's bans with the new server's
BattlEye ban list after moving.

## RCON security

BattlEye RCON is unencrypted UDP. Use a long, unique RCON password, and if your host offers an IP
allowlist for RCON, allow only the DZ Manager machine. Actions that go through the bridge travel
over HTTPS instead.

## Priority queue

Your host or launcher applies queue priority. DZ Manager keeps the list (**Players → Priority
queue**) and exports it for your host's tools through `GET /public/v1/priority` with an API key
that has the `priority:read` scope. See [API and MCP](api.md).
