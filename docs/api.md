# Public API and MCP server

Both use **API keys** made in Settings → Integrations → API keys. A key has a name, an optional
expiry and a fixed set of scopes; the token (`dzm_…`) is shown once and only a keyed hash is
stored. Revoking a key stops it at once. Everything a key does in game is in the audit log under
"API key “name”".

| Scope | Gives |
|---|---|
| `public` | Server status, names online, kill feed, leaderboards, player stats by name, ban list (names and reasons), world event counts, server info text, the event feed. No Steam IDs, IPs or positions; raid events never say where. Safe for a website. |
| `players:read` | Players with Steam64, kills, deaths, playtime, banned / priority flags. No IPs. |
| `bans:read` | Active bans with Steam64 and BattlEye GUID, as JSON, text or a BattlEye `bans.txt`, to share with other servers. |
| `priority:read` | Queue priority (Steam64 and end), as JSON or text. |
| `map:read` | The world, live players and vehicles with positions, active events. |
| `actions:write` | Player actions, vehicle actions and on-screen messages. Destructive ones still need their confirmation word. |

Only ever put a key with **just** `public` in browser code; keep the others on a server or in your
AI client.

## REST: `/public/v1`

Send `Authorization: Bearer dzm_…`. GET routes answer CORS, so a website can call them from the
browser. The OpenAPI 3.1 description is at `/public/v1/openapi.json` (no key needed).

| Route | Scope |
|---|---|
| `GET /status`, `/online`, `/info`, `/feed?after=`, `/killfeed`, `/leaderboard?by=kills\|kd\|playtime`, `/player?name=`, `/bans`, `/events` | `public` |
| `GET /players?q=&sort=&limit=&offset=`, `/players/{steam64}` | `players:read` |
| `GET /banlist?format=json\|txt\|battleye` | `bans:read` |
| `GET /priority?format=json\|txt` | `priority:read` |
| `GET /map` | `map:read` |
| `POST /players/{steam64}/actions/{teleport\|give_item\|heal\|kill\|strip}` | `actions:write` |
| `POST /vehicles/{id}/actions/{vehicle_refuel\|vehicle_repair\|vehicle_flip\|vehicle_stop_engine\|vehicle_delete}` | `actions:write` |
| `POST /message` `{"text": "…", "steam64": "optional"}` | `actions:write` |

Examples:
```bash
# Leaderboard for a website
curl -H "Authorization: Bearer $KEY" https://dzm.example.com/public/v1/leaderboard?by=kd
# Pull another server's bans as a BattlEye bans.txt
curl -H "Authorization: Bearer $KEY" "https://dzm.example.com/public/v1/banlist?format=battleye" > bans.txt
# Priority list for a queue tool
curl -H "Authorization: Bearer $KEY" "https://dzm.example.com/public/v1/priority?format=txt"
# Repair a vehicle (id from GET /map)
curl -X POST -H "Authorization: Bearer $KEY" https://dzm.example.com/public/v1/vehicles/12345:678/actions/vehicle_repair -d '{}'
# Delete one (refused while someone is inside)
curl -X POST -H "Authorization: Bearer $KEY" https://dzm.example.com/public/v1/vehicles/12345:678/actions/vehicle_delete -d '{"confirm":"DELETE"}'
```
Actions wait for the game server's answer (up to 20 s) and return what it reported; `502` means
the game refused (with the reason), `504` that it did not answer.

## MCP: `/mcp`

A Model Context Protocol server (Streamable HTTP, stateless, protocol 2024-11-05 to 2025-11-25).
For clients that support Streamable HTTP, use the snippet Settings shows
after creating a key:
```json
{ "mcpServers": { "dz-manager": { "type": "http", "url": "https://dzm.example.com/mcp",
  "headers": { "Authorization": "Bearer dzm_…" } } } }
```

Tools (a client only sees those its key's scopes allow):

| Tool | Scope |
|---|---|
| `server_status`, `players_online`, `kill_feed`, `leaderboard`, `player_stats`, `recent_events`, `world_events` | `public` |
| `search_players`, `get_player` | `players:read` |
| `ban_list` | `bans:read` |
| `priority_list` | `priority:read` |
| `live_map` | `map:read` |
| `player_action`, `vehicle_action`, `send_message` | `actions:write` |

Safety: each tool runs the matching REST route in-process with the same key, so validation, scopes,
confirmation words and the audit log are identical. Write tools are marked `destructiveHint`, the
server tells the assistant to ask the human before any write, and destructive actions need their
confirmation word (`KILL`, `STRIP`, `DELETE`). Requests with an `Origin` header (browsers) are
refused. For an assistant that should only look, give its key no `actions:write`.
