# Troubleshooting

Start with the **dashboard**: its badges and warnings say which part is not working. The logs
usually say why: `journalctl -u dzm.service -f` on a Linux install, `docker compose logs -f dzm`
with Docker. On the game server, the bridge writes lines starting with `[DZMBridge]` to the
server's script log.

## DZ Manager does not start

Run `dzm check-config` with the same settings. It lists every problem at once. The usual ones:

- `DZM_SECRET_KEY` is missing or not exactly 32 bytes in base64. Generate a new one only on a new
  install; on an existing one, restore the original from your password manager.
- `DZM_PUBLIC_URL` or `DZM_INGEST_PUBLIC_URL` does not start with `https://`.
- `DZM_TZ` is not a valid timezone name. Use names like `Europe/Berlin`, not abbreviations like
  `CET`.

## The panel does not open from outside

1. On the machine itself, `curl http://127.0.0.1:8080/ready` must answer `"status":"ready"`. If it
   does not, DZ Manager is not running (see above).
2. If it does, the problem is the tunnel or proxy in front. For Cloudflare Tunnel check that the
   tunnel is **Healthy** and the public hostname points at `http://127.0.0.1:8080` (or
   `http://dzm:8080` when the tunnel runs in Docker Compose).
3. Sign-in loops or "origin not allowed" errors mean the address in your browser is not exactly
   `DZM_PUBLIC_URL`.

## RCON or query stays red

- Check the host, ports and password in **Settings → Server**. The query port is usually the game
  port + 1 or 27016; the RCON port and password are in the game server's
  `battleye/BEServer_x64.cfg` (`RConPort`, `RConPassword`).
- RCON must be enabled on the game server and reachable from the machine running DZ Manager. Some
  hosts only allow RCON from listed IP addresses: add the public address of your DZ Manager
  machine.
- RCON is plain UDP. Some home routers and networks block it; try from another network to rule
  this out.

## The bridge never connects

**Settings → Server** shows no bridge version or heartbeat.

1. In the game server's script log, look for `[DZMBridge]`:
   - no line at all: the mod is not loaded. It must be in the **server-side** mods
     (`-servermod=@DZMBridge`), not in `-mod=`.
   - `disabled: paste the config…`: `profiles/DZMBridge/config.json` still has the defaults.
     Paste the configuration from **Connect…** and restart.
   - `v… started` followed by warnings about failed requests: the game server cannot reach the
     ingest address. Continue below.
2. From any machine run `curl -i -X POST <DZM_INGEST_PUBLIC_URL>/ingest/ping`. DZ Manager answers
   `401` with a short JSON error. An HTML page, a CAPTCHA or a `403` from your CDN means a
   security rule is blocking the game server: exempt `/ingest/*` from bot protection, challenges
   and access policies. See [Cloudflare Tunnel, step 4](../deploy/cloudflared/README.md#4-let-the-game-server-through).
3. If you rotated the ingest keys in **Settings → Server**, paste the new configuration on the
   game server and restart it.

## The map is empty or grey

- Grey background but markers present: install the map image under **Settings → Map & tracking**.
- No players or vehicles: the bridge is not connected (see above). Vehicles appear a few minutes
  after a server start, once the world has loaded.
- Wrong map: the bridge reports the map automatically; until it has connected, pick the map by
  hand in **Settings → Map & tracking**.

## Restarts are marked "missed"

DZ Manager compares the schedule under **Restarts** with what actually happens.

- Make the schedule match your host's or launcher's restart times and timezone exactly.
- With @DZMBridge connected, every restart is detected from the server's own start report, even
  very fast ones. Without the bridge, DZ Manager can only notice restarts that keep the server
  offline for at least about 20 seconds.
- "Restart now" and countdown restarts only send a clean shutdown. Your host or launcher must be
  set to start the server again automatically.

## Actions say "needs @DZMBridge" or time out

Player and vehicle actions run inside the game through the bridge. Check that the bridge is
connected and up to date (**Settings → Server** shows the installed version and which actions it
supports). Actions time out after 20 seconds if the game server does not answer, for example
while it is restarting.

## Still stuck

Check the [game host requirements](providers.md) and the open and closed issues on the project's
GitHub page. Security problems should be reported privately as described in the
[security policy](../SECURITY.md).
