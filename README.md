# DZ Manager

Private, single-owner, GameLabs-style admin console for a DayZ PC server (Chernarus).
One static Go binary with an embedded React UI and SQLite, behind Cloudflare Tunnel, with login only
through SSO.

- Architecture: [ADR 0001](docs/adr/0001-architecture.md)
- Contracts: [host ⇄ release](docs/contracts/host-and-release.md) · [game ingest v1](docs/contracts/ingest-v1.md)
- Owner guides: [install game-side PBOs and tiles](docs/install-game-side.md) · [providers / Managed / migration](docs/providers.md)
- [Development](docs/development.md) · Constitution: issue #1 · Roadmap: issue #2

| Feature | Needs | Issue |
|---|---|---|
| Server status, roster, broadcast / PM / kick / lock | RCON + A2S | #15 |
| Live map: players, vehicles, server performance | MetricZ | #5 |
| Restart observation + countdowns (provider is the authority) | RCON/A2S | #4 |
| Event push + command channel | @DZMBridge | #16 |
| Teleport / give / heal / kill / strip | @DZMBridge | #7 |
| Active heli crashes, airdrops, rare-item spawns | @DZMBridge | #6 |
| Player history: sessions, playtime, kills/deaths | @DZMBridge (+ MetricZ presence) | #11 |
| Bans (BattlEye-enforced) + external list review | RCON | #10 |
