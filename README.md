# DZ Manager

Admin console for a DayZ PC server: live map, players, bans, priority queue, restarts, Discord
feeds, a public API and an MCP server for MCP clients. One static Go binary with an embedded
React UI and SQLite. Its own accounts with roles, invites and optional 2FA (password or Steam
sign-in, optional single sign-on). Works on any map, official or Workshop.

- Architecture: [ADR 0001](docs/adr/0001-architecture.md)
- Contracts: [host ⇄ release](docs/contracts/host-and-release.md) · [game ingest v1](docs/contracts/ingest-v1.md)
- Guides: [accounts, sign-in and roles](docs/accounts.md) · [game-side mod and map image](docs/install-game-side.md) · [public API and MCP](docs/api.md) · [Docker](docs/docker.md) · [providers](docs/providers.md)
- [Development](docs/development.md) · Constitution: issue #1 · Roadmap: issue #2

| Feature | Needs |
|---|---|
| Server status, roster, broadcast / PM / kick / lock | RCON + A2S |
| Live map of any map: players, vehicles, events, place names | @DZMBridge + a map image (one click in Settings) |
| Restart observation + countdowns (your panel or launcher is the authority) | RCON/A2S |
| Event push + command channel | @DZMBridge |
| Player actions: teleport / give / heal / kill / strip | @DZMBridge |
| Vehicle actions: refuel / repair / flip / stop engine / delete | @DZMBridge |
| Active heli crashes, airdrops, rare-item spawns | @DZMBridge |
| Player history: sessions, playtime, kills/deaths | @DZMBridge |
| Bans (BattlEye-enforced), external list review, export to share | RCON |
| Discord feeds (raids never say where) | – |
| Public API with scoped keys, MCP server for MCP clients | – |

`@DZMBridge` is a server-side mod with no dependencies (Steam Workshop: "DZ Manager Bridge"). When
LBmaster mods are loaded it uses them too (notifications, global banlist, territory flags, code
lock raids); nothing of LBmaster is included.
