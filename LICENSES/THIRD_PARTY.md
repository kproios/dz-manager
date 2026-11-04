# Third-party components

DZ Manager is private, single-owner software. It includes or links the following
open-source components; their licenses are honoured and notices kept here.

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
- **DZMap** loader (MIT) is run by the operator to fetch map tiles; tile imagery is
  third-party (xam.nu / iZurvive sources) and used privately only, never redistributed.
- **CFTools GameLabs** plugin (all rights reserved) was used as a behaviour reference only. No code copied.
- **LogZ** and **metricz-exporter** (GPL/AGPL) were read to understand formats only. No code copied.
