# Installing the game-side pieces

Everything on the game server is **server-side only**. Never add these mods to `-mod=`, and
players never download anything.

## 1. @DZMBridge (server-side mod)
1. Get `@DZMBridge` from the Steam Workshop ("DZ Manager Bridge"), or download `DZMBridge.zip`
   from the latest release and check it against `SHA256SUMS`.
2. Put `@DZMBridge/` next to the server executable (your launch folder, or your panel's file
   manager).
3. Add it to the **server mods** launch parameter: `-servermod=@DZMBridge` (or your panel's
   "server-side mods" field). The PBO is unsigned on purpose: server-side mods need no `.bisign`,
   and no key goes into `keys/`.
4. Start once. The RPT shows `[DZMBridge] disabled: paste the config …` and
   `profiles/DZMBridge/config.json` is created.
5. DZ Manager → Settings → Servers → **Connect…**: copy the bridge JSON over that file
   (`enabled: true`, `ingest_url`, `server_id`, `server_token`, tracking lists) and restart.
6. Check: Settings → @DZMBridge shows the installed version and a heartbeat. The `bridge` and
   `bridge.commands` capabilities turn `ok`, and Settings → Map & tracking shows the map name
   "reported by the game server".

The bridge has **no dependencies**. When LBmaster mods are loaded it also uses them (LB
notifications for messages, the LB global banlist for review, LB territory flags as base markers,
LB code lock raids as raid events); set `"disable_lbmaster": true` in its config to turn that off.

**Updates are manual:** DZ Manager can auto-update itself, but not the game-side mod. Settings
shows the installed version, so update the mod when the release notes say it changed. Vehicle
actions require an up-to-date @DZMBridge.

**Rollback:** remove `@DZMBridge` from `-servermod` and restart. Nothing else on the server is
modified by the mod (it only writes `profiles/DZMBridge/config.json`).

## 2. MetricZ (optional, upstream, unmodified)
@DZMBridge already sends live positions. MetricZ adds server performance history.
1. Install MetricZ from its upstream source (Steam Workshop id 3594119002 or its release) as a
   **server-side** mod.
2. Start once to create `profiles/metricz/config.json`, then merge the MetricZ JSON from
   **Connect…** (`http.enabled/url/user/password`, `settings.disable_telemetry: true`) and restart.
3. Check: the `metricz` capability turns `ok`.

## 3. Map image (in DZ Manager, once per map)
Settings → Map & tracking → **Map image**: pick the map and a size, then **Download and install**.
DZ Manager downloads a terrain image from [tools.dzhosts.com/maps](https://tools.dzhosts.com/maps/)
(rendered from the game's own terrain textures by
[dayz-terrain-map](https://github.com/jmhobbs/dayz-terrain-map), MIT), cuts it into map tiles under
`$DZM_DATA_DIR/tiles/<world>/terrain/` and deletes the download. 7,000–8,000 px is plenty; tiling
a 7616 px image takes a few seconds and about 270 MB of memory. `DZM_MAP_MAX_PX` (default 10000)
caps the size.

From the command line (also for maps that are not listed, with a PNG you rendered yourself with
dayz-terrain-map from your own game files):
```bash
dzm map list
dzm map install --world chernarusplus                 # best size from dzhosts.com
dzm map install --world enoch --slug livonia --px 7616
dzm map install --world namalsk --file namalsk.png
```
Run it as the user that runs `dzm serve` (it writes into `DZM_DATA_DIR`). The map image is never
committed to git or shipped with releases. Markers work without an image.

The map's name, size and place names come from the game server (@DZMBridge reads `CfgWorlds`), so
any map works. Until the bridge has connected, set the map by hand in the same card.

## What is verified vs pending
| Item | Status |
|---|---|
| PBO structure (header, prefix, module paths, SHA1) | verified in CI (`mod/tools/check_pbo.py`) |
| Enforce Script compiles on DayZ | verified with DayZDiag before each bridge release; there is no DayZ compiler in CI |
| MetricZ payload parsing | verified against a fixture built from MetricZ docs |
