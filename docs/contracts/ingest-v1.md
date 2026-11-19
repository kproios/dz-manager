# Contract: game → DZ Manager ingest, version 1

Status: **binding** for `@DZMBridge` and any other emitter, including third-party integrations. Implemented in `internal/bridge`. The tests in
`internal/bridge/bridge_test.go` are the executable form of this document.

Base URL: `DZM_INGEST_PUBLIC_URL + /ingest/v1/{token}`, for example
`https://dzm-ingest.example.com/ingest/v1/<server_token>`.

## Authentication
- `{token}` is the per-server bridge token generated in **Settings → Servers → Connect** (32
  random bytes, rotatable). It is carried in the **URL path** because DayZ's `RestContext` can't
  set custom headers reliably. `Authorization: Bearer <token>` is also accepted for other
  emitters.
- A wrong or rotated token returns `401`. The payload's `server_id`, when present, must match the
  token's server, otherwise `403`.
- Transport is HTTPS only (Cloudflare). The ingest hostname publishes only `/ingest/*`.

## POST `{base}/events`
```json
{
  "v": 1,
  "server_id": "<uuid from the wizard>",
  "emitter": "dzmbridge",
  "seq": 42,
  "events": [
    {"id": "20261104120000-1234-17", "type": "player.connect", "ts": "2026-11-04T12:00:00Z",
     "actor": "76561198000000001", "target": "", "pos": {"x": 6560.1, "y": 9.8, "z": 2480.5},
     "data": {"name": "Alice", "uid": "<PlayerIdentity.GetId()>"}}
  ]
}
```
| Rule | Value |
|---|---|
| Batch size | ≤ 500 events, body ≤ 1 MiB |
| `id` | `[A-Za-z0-9_.:-]{1,80}`, unique per server **forever**. Re-sending the same id is a no-op, so retries are safe. |
| `type` | `[a-z][a-z0-9_.]{1,63}` |
| `ts` | RFC 3339 UTC, within −30 days … +10 min of server time |
| `actor` / `target` | Steam64 where applicable |
| `pos` | world metres: x east, **y altitude**, z north |
| `data` | JSON object ≤ 16 KiB (`null` or missing = `{}`) |

Response `200`: `{"accepted": n, "duplicates": n, "rejected": [{"id": "…", "reason": "…"}]}`.
Rejected events are not retried by the server, so fix the emitter. Order doesn't matter: handlers
tolerate late and out-of-order events.

### Event types understood today
| Type | Fields |
|---|---|
| `heartbeat` | `data.mod_version`, `data.game_version`, `data.commands[]` (handlers this emitter executes), `data.dropped`, `data.queue`. Sets the `bridge` / `bridge.commands` capabilities. Absence for 90 s means degraded. |
| `server.start` | Closes open sessions as `inferred_end` and ends active world events. |
| `player.connect` | `actor`, `data.name`, `data.uid` or `data.be_guid` (validated against Steam64) |
| `player.disconnect` | `actor` |
| `player.death` | `actor` = victim, `target` / `data.killer` = killer Steam64 (omit if none), `data.weapon`, `data.distance_m`, `data.killer_pos`, `data.cause`. Never send an invented attacker. |
| `event.start`, `event.seen` | `pos`, `data.key` (stable per occurrence), `data.category` (`heli_crash` / `airdrop` / `loot_box` / `custom`), `data.class`, optional `data.ttl_s` (≤ 24 h). With `ttl_s` the marker ends when that time is up. Without it the emitter must keep sending `event.seen`: a marker with no ping for the configured limit (default 120 s, at least 60 s) is expired, and a later ping revives it. |
| `event.end` | `data.key` |
| `item.spawn` | `pos`, `data.class` (must be in the configured rare-item allow-list), `data.key` |

Unknown types are **stored raw and flagged** (`known=false`), so a new emitter can start sending
before the UI supports it. Reserved namespace for LBMaster: `lbm.*`.

## POST `{base}/live`
Current positions, separate from the event queue because only the latest values matter
(nothing is stored or replayed). Sent every `live_seconds`.
```json
{"v": 1, "full": false, "interval_s": 2,
 "players": [{"steam64": "7656119…", "name": "…", "x": 0, "y": 0, "z": 0, "health": 0.9, "blood": 1, "shock": 1, "water": 0.7, "energy": 0.6, "unconscious": false, "restrained": false, "bleeding": false}],
 "vehicles": [{"id": "lo:hi", "class": "Motorbike_01_Blue", "type": "motorbike", "x": 0, "y": 0, "z": 0, "health": 1, "speed": 0, "crew": 0, "engine_on": false, "fuel": 0.5}],
 "gone": ["lo:hi"]}
```
`players` is always everyone. `vehicles` holds only what changed unless `full` is true, when it
is the complete list and anything absent is removed. `gone` lists deleted vehicles. Vitals are
0..1 fractions and `speed` is m/s. Rows that fail validation are dropped.

## POST `{base}/commands/poll`
Body `{}`. Returns up to 10 queued, unexpired commands and marks them `delivered`. A delivered
command is **never** delivered again.
```json
{"commands": [{"id": "uuid", "action": "teleport", "target": "7656119…", "params": {"x": 7000, "z": 2500}, "expires_at": "…"}]}
```
| Action | `params` | Expected `data` in the result |
|---|---|---|
| `ping` | – | `{"pong": true}` |
| `list_players` | – | `{"players": [{steam64, uid, name, pos, alive}]}` |
| `teleport` | `x`, `z`, optional `y` + `has_y`, or `to_player` | `{"pos": {...}}` (postcondition) |
| `give_item` | `item` (classname; `class` is reserved in Enforce Script), `quantity` 1–20 | `{"item", "in_inventory", "on_ground"}` |
| `heal` | – | `{"health", "blood", "shock"}` |
| `kill` | – | `{"alive": false}` |
| `strip` | – | `{"removed": n}` |

Validation happens on **both** sides. Emitters must refuse unknown actions and never offer
generic script or console execution.

## POST `{base}/commands/{id}/result`
Body `{"ok": true, "error": "", "data": {...}}`. Unknown id → `404`. A result after expiry (default
30 s) is recorded as late but the command stays `expired`. It never turns into a success.

## Backpressure (emitter obligations)
Bounded queue (drop oldest and count drops in the heartbeat), batch at most every few seconds, back
off up to 60 s on errors, at most one log line per minute while the backend is down, and never
block the game thread.
