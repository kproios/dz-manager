# Installing the game-side pieces (owner)

Everything on the game server is **server-side only**. Never add these mods to `-mod=`, and
players never download anything. Agents have no access to the game host: the owner installs them.

## 1. @DZMBridge (our PBO)
1. Download `DZMBridge.zip` from the latest GitHub release (also attached to every CI run as an
   artifact) and check it against `SHA256SUMS`.
2. Extract `@DZMBridge/` next to the server executable (TestServer: via Architect's mod folder;
   Managed: panel file manager).
3. Add it to the **server mods** launch parameter: `-servermod=@MetricZ;@DZMBridge`
   (Architect: server-side mods field). The PBO is unsigned on purpose: server-side mods need
   no `.bisign`.
4. Start once. The RPT shows `[DZMBridge] disabled: paste the config …` and
   `profiles/DZMBridge/config.json` is created.
5. DZ Manager → Settings → Servers → **Connect…**: copy the bridge JSON over that file
   (`enabled: true`, `ingest_url`, `server_id`, `server_token`, tracking lists) and restart.
6. Check: Settings → @DZMBridge shows the installed version and a heartbeat. The `bridge` and
   `bridge.commands` capabilities turn `ok`.

**Updates are manual:** the host auto-updates DZ Manager, but not the game-side PBO. Settings
shows installed vs. latest, so update the PBO when the release notes say it changed.

**Rollback:** remove `@DZMBridge` from `-servermod` and restart. Nothing else on the server is
modified by the mod (it only writes `profiles/DZMBridge/config.json`).

## 2. MetricZ (upstream, unmodified)
1. Install MetricZ from its upstream source (Steam Workshop id 3594119002 or its release) as a
   **server-side** mod.
2. Start once to create `profiles/metricz/config.json`, then merge the MetricZ JSON from
   **Connect…** (`http.enabled/url/user/password`, `settings.disable_telemetry: true`) and restart.
3. Check: the live map shows players, and the `metricz` capability turns `ok`.

## 3. Map tiles (control-plane host, once)
Tiles are third-party imagery, so they're used privately and never committed to git:
```bash
git clone https://github.com/WoozyMasta/dzmap && cd dzmap
go run ./cmd/loader -c config.yaml --limit chernarusplus
sudo mkdir -p /var/lib/dzm/tiles && sudo cp -r maps/chernarusplus /var/lib/dzm/tiles/
sudo chown -R dzm:dzm /var/lib/dzm/tiles
```
The UI expects `/var/lib/dzm/tiles/chernarusplus/topographic/{z}/{x}/{y}.webp` (zoom 0 = the
whole 15 360 m map in one 256 px tile). If your loader output differs, adjust the folder names,
not the code. Markers work without tiles.

## What is verified vs pending
| Item | Status |
|---|---|
| PBO structure (header, prefix, module paths, SHA1) | verified in CI (`mod/tools/check_pbo.py`) |
| Enforce Script compiles on DayZ | **pending live verification**: there is no DayZ compiler in CI |
| Events and command handlers in game | **pending live verification** on TestServer |
| MetricZ payload parsing | verified against a fixture built from MetricZ docs; **real payload pending** |
