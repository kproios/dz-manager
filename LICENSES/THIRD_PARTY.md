# Third-party components

DZ Manager includes or links the following open-source components; their licenses are
honoured and notices kept here. It contains no proprietary third-party code.

## Linked into the `dzm` binary (Go)
| Module | License | Use |
|---|---|---|
| github.com/woozymasta/bercon | MIT | BattlEye RCON client and response parser |
| github.com/woozymasta/a2s | MIT | A2S server query |
| github.com/woozymasta/dzid | MIT | Steam64 → BattlEye GUID / DayZ ID |
| github.com/coreos/go-oidc/v3 | Apache-2.0 | OpenID Connect (SSO) |
| github.com/go-jose/go-jose/v4 | Apache-2.0 | JOSE/JWT (via go-oidc) |
| golang.org/x/oauth2 | BSD-3-Clause | OAuth2 client |
| modernc.org/sqlite (+ modernc.org/libc etc.) | BSD-3-Clause | Pure-Go SQLite |
| github.com/google/uuid, dustin/go-humanize, mattn/go-isatty, ncruces/go-strftime, remyoudompheng/bigfft | BSD/MIT | transitive (sqlite) |

## Embedded UI (npm)
| Package | License |
|---|---|
| react, react-dom | MIT |
| leaflet | BSD-2-Clause |

## Game-side PBO (`@DZMBridge`)
Adapted from **DayZ-Takaro-Integration** (https://github.com/mad-001/DayZ-Takaro-Integration),
MIT License, Copyright (c) 2026 mad-001: RestApi wrapper, queue/dispatcher pattern,
MissionServer/PlayerBase hook approach, kill resolution, and the Linux PBO packer.
The full notice ships inside the mod as `LICENSE-THIRD-PARTY.txt`.

## Used but not included
- **MetricZ** (GPL-3.0) runs as a separate, unmodified upstream server-side mod; DZ Manager
  implements its own receiver for MetricZ's documented HTTP push protocol.
- **Map images**: the operator downloads a terrain image from tools.dzhosts.com (rendered from
  the game's terrain textures by **dayz-terrain-map**, MIT, https://github.com/jmhobbs/dayz-terrain-map)
  or renders one from their own game files; DZ Manager tiles it on their server. No image is in
  this repository or in any release. DayZ terrain © Bohemia Interactive.
- **LBmaster** mods (paid, all rights reserved): @DZMBridge calls their public script API at
  runtime when they are loaded (`#ifdef` guarded). No LBmaster code or files are included.
- **LogZ** and **metricz-exporter** (GPL/AGPL) were read to understand formats only. No code copied.
