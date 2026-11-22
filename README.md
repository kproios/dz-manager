# DZ Manager

DZ Manager is a web control panel for a DayZ PC server. Open it in a browser to see who is
online, follow players and vehicles on a live map, handle bans and the priority queue, plan
restarts, send messages, post events to Discord, and let bots and MCP clients work with your
server through a scoped API.

It is a single program with its own database and web interface built in. There is nothing else to
install next to it, and it runs comfortably on a small Linux machine, a home server or a free
cloud VM.

## How the pieces fit together

```
            Your browser                          Discord, bots, MCP clients
                 │  HTTPS                                    │  HTTPS + API key
                 ▼                                           ▼
        ┌──────────────────────────────────────────────────────────┐
        │  DZ Manager (one program + SQLite database)              │
        │  runs on your machine, a home server or a small VM       │
        └──────────────────────────────────────────────────────────┘
            ▲  HTTPS (events, live map,           │  RCON + server query
            │  performance, commands)             │  (status, roster, bans,
            │                                     │   messages, restarts)
        ┌──────────────────────────────────────────────────────────┐
        │  Your DayZ server, with the server-side mod @DZMBridge    │
        │  (rented from a host or run yourself)                     │
        └──────────────────────────────────────────────────────────┘
```

- **DZ Manager** is the panel. You install it once, on any always-on machine.
- **@DZMBridge** is a small server-side mod for the game server. Players never download it. It
  sends live positions, events and performance to DZ Manager and carries out actions such as
  teleport or vehicle repair.
- **RCON and the server query** are built into every DayZ server. DZ Manager uses them for status,
  the player list, messages, kicks, bans and restarts, even before the mod is installed.

## What you get

| Feature | What it needs |
|---|---|
| Server status, player list, messages, kick, lock | RCON and the server query |
| Bans enforced by BattlEye, ban review, ban list export | RCON |
| Restart schedule with in-game countdowns, restart history | RCON (your host or launcher still starts the server) |
| Live map of any map: players, vehicles, events, place names | @DZMBridge and a map image (one click in Settings) |
| Player actions: teleport, give item, heal, kill, strip | @DZMBridge |
| Vehicle actions: refuel, repair, flip, stop engine, delete | @DZMBridge |
| Server performance: FPS, uptime, infected, animals, items, vehicles | @DZMBridge |
| Player history: sessions, playtime, kills and deaths | @DZMBridge |
| Heli crashes, airdrops and rare-item spawns on the map | @DZMBridge |
| Discord feeds (raid alerts never reveal locations) | Nothing extra |
| Accounts with roles, invites and optional two-step sign-in | Nothing extra |
| Public API with scoped keys, and an MCP server for MCP clients | Nothing extra |

Popular admin mods such as LBmaster are detected and used when present (notifications, global
ban list, territory flags, code lock raids). None of their code is included.

## Get started

New here? Follow **[Getting started](docs/getting-started.md)**. It takes you from nothing to a
working panel in about half an hour.

| Guide | For |
|---|---|
| [Getting started](docs/getting-started.md) | The whole setup, step by step |
| [Hosting options](docs/hosting.md) | Where to run it, including free options, and how to get HTTPS |
| [Install with Docker](docs/docker.md) | Windows, macOS or Linux with Docker |
| [Install on Linux](deploy/README.md) | A dedicated Debian machine with automatic updates and backups |
| [HTTPS with Cloudflare Tunnel](deploy/cloudflared/README.md) | Publishing the panel without opening ports |
| [Game server setup](docs/install-game-side.md) | Installing @DZMBridge and the map image |
| [Configuration](docs/configuration.md) | Every setting explained |
| [Updates and backups](docs/updates-and-backups.md) | Keeping it current and safe |
| [Troubleshooting](docs/troubleshooting.md) | Common problems and fixes |

Reference: [accounts and roles](docs/accounts.md) · [public API and MCP](docs/api.md) ·
[game hosts](docs/providers.md) · [map tracking](docs/map-tracking.md) · [events](docs/events.md) ·
[architecture](docs/adr/0001-architecture.md) · [host and release contract](docs/contracts/host-and-release.md) ·
[ingest protocol](docs/contracts/ingest-v1.md) · [development](docs/development.md) ·
[security policy](SECURITY.md)

## Licence

MIT, see [LICENSE](LICENSE). Third-party notices are in [LICENSES](LICENSES/THIRD_PARTY.md).
