# Getting started

This guide takes you from nothing to a working panel. You do not need to be a programmer; you
need to be comfortable copying commands into a terminal and editing a text file.

**Time:** about 30 minutes. **Cost:** nothing, if you use one of the free options in
[Hosting options](hosting.md).

## What you need

- A DayZ PC server you control: rented from a game host or run yourself. You need its **IP
  address**, **game port**, **query port** (usually game port + 1 or 27016) and **RCON port and
  password**. Game hosts show these in their control panel; RCON settings live in
  `battleye/BEServer_x64.cfg`.
- The ability to **upload a mod folder** to the game server and edit its **launch parameters**
  (every host's file manager and "server mods" field can do this).
- An **always-on machine** for DZ Manager: a small Linux VM, a home server, a Raspberry Pi 4/5
  (64-bit), or your own PC if it stays on. 1 CPU and 1 GB of memory are enough.
- A way to reach it over **HTTPS**. The game server sends its data to DZ Manager over the
  internet, and browsers need HTTPS for sign-in. [Hosting options](hosting.md) explains free ways
  to do this, such as Cloudflare Tunnel.

## Step 1: choose where DZ Manager runs

| If you have… | Use |
|---|---|
| Windows or macOS, or any machine with Docker | [Docker](docker.md) |
| A dedicated Debian machine or container you can reach from its console | [The Linux installer](../deploy/README.md) (adds automatic updates and backups) |
| Nothing yet | A free cloud VM, see [Hosting options](hosting.md), then Docker |

## Step 2: install and start it

Follow [Docker](docker.md) or [Linux](../deploy/README.md). Both end with DZ Manager listening on
`http://127.0.0.1:8080` on that machine. That address only works on the machine itself; the next
step makes it reachable from outside.

Before you continue, decide on two web addresses (they can be the same):

- the **panel address**, where you and your admins sign in, for example `https://dzm.example.com`
- the **ingest address**, where the game server sends its data, for example
  `https://dzm-ingest.example.com`

Put them in the configuration as `DZM_PUBLIC_URL` and `DZM_INGEST_PUBLIC_URL`. See
[Configuration](configuration.md) for every setting.

## Step 3: make it reachable over HTTPS

Pick one of the options in [Hosting options](hosting.md#getting-https). The easiest free choice
is a [Cloudflare Tunnel](../deploy/cloudflared/README.md): no ports to open and certificates are
handled for you.

Check from any browser that `https://<your panel address>/ready` shows `"status":"ready"`.

## Step 4: create the first owner account

On the machine running DZ Manager:

```bash
# Docker (in the deploy/docker folder)
docker compose exec dzm dzm admin invite-owner

# Linux install (as root): runs it as the service user with the service settings
systemd-run --pipe --wait -p User=dzm -p EnvironmentFile=/etc/dzm/dzm.env \
  /opt/dzm/current/dzm admin invite-owner
```

It prints a one-time link. Open it, choose your name and a password (or sign in with Steam), and
you are in. You can turn on two-step sign-in later under **Account**. Invite other admins under
**Users** and choose their role ([roles explained](accounts.md#roles)).

## Step 5: add your game server

1. **Settings → Server → Add server.** Enter a name, the game host and ports, and the RCON port
   and password. Leave the query and RCON hosts blank when they are the same as the game host.
2. Within a few seconds the **query** and **RCON** badges on the dashboard turn green. The player
   list, messages, kicks and bans work from this point on.

If a badge stays red, see [Troubleshooting](troubleshooting.md#rcon-or-query-stays-red).

## Step 6: install @DZMBridge on the game server

1. On the server's row, click **Connect…**. It shows the launch parameter and the exact
   configuration to paste.
2. Follow [Game server setup](install-game-side.md): upload the `@DZMBridge` folder, add it to
   the **server-side** mods, start once, paste the configuration and restart.
3. **Settings → Server** then shows the bridge version and a recent heartbeat. The live map,
   player and vehicle actions and performance charts come alive.

## Step 7: install the map image

**Settings → Map & tracking → Map image**: pick your map and click **Download and install**. It
takes a few seconds. Markers work without it; the image is the background.

## Step 8: optional extras

- **Restarts:** under **Restarts**, copy the schedule your host or launcher uses. DZ Manager warns
  players before each restart and records whether the server came back. It can also send the
  shutdown itself; your host or launcher must start the server again.
- **Discord:** **Settings → Integrations → Discord** sends kills, joins, raids, restarts and bans
  to your channels.
- **API keys and MCP clients:** **Settings → Integrations → API keys** creates keys for a
  website, a bot or an MCP client. See [API and MCP](api.md).
- **Messages:** **Settings → Messages** sets welcome and rotating messages.

## Keep it running

- Linux installs update themselves and keep daily backups. Docker installs update when you pull
  or rebuild. See [Updates and backups](updates-and-backups.md).
- **Store `DZM_SECRET_KEY` in a password manager.** A backup cannot be restored without it.
- @DZMBridge is not updated automatically: when a release says it changed, replace the mod on the
  game server during a restart.
