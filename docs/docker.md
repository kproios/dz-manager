# Install with Docker

Docker runs DZ Manager the same way on Windows, macOS and Linux. The container is small (one
program on a minimal base image), runs as an unprivileged user with a read-only file system, and
keeps all its data in one volume.

## 1. Install Docker

- **Windows or macOS:** install [Docker Desktop](https://www.docker.com/products/docker-desktop/)
  and start it once.
- **Linux:** install Docker Engine and the Compose plugin from
  [docs.docker.com/engine/install](https://docs.docker.com/engine/install/).

Check in a terminal (PowerShell on Windows):

```bash
docker compose version
```

## 2. Get DZ Manager

Download the source code of the [latest release](../../../releases/latest) (the "Source code"
zip) and unpack it, or clone the repository with git. Then open a terminal in the
`deploy/docker` folder.

The image is built from that source on your machine the first time you start it. This takes a few
minutes once; later starts are instant.

## 3. Fill in the settings

Copy the example settings file:

```bash
cp dzm.env.example dzm.env          # Windows PowerShell: Copy-Item dzm.env.example dzm.env
```

Open `dzm.env` in a text editor and set at least:

| Setting | What to put |
|---|---|
| `DZM_SECRET_KEY` | 32 random bytes in base64. Generate one with `openssl rand -base64 32`, or in PowerShell 7: `[Convert]::ToBase64String([Security.Cryptography.RandomNumberGenerator]::GetBytes(32))`. **Save it in your password manager.** |
| `DZM_PUBLIC_URL` | Your panel address, for example `https://dzm.example.com` |
| `DZM_INGEST_PUBLIC_URL` | Where the game server sends data. Can be the same as the panel address. |
| `DZM_TZ` | Your timezone, for example `Europe/London` or `America/New_York` |

All other settings are explained in [Configuration](configuration.md). Never share or commit
`dzm.env`.

## 4. Start it

```bash
docker compose up -d
docker compose logs -f dzm          # watch the start; Ctrl+C stops watching, not the panel
```

The panel now answers on `http://127.0.0.1:8080` on this machine. To reach it from the internet
over HTTPS, set up one of the options in [Hosting options](hosting.md#getting-https).

### Running the Cloudflare tunnel in Docker too

If you use [Cloudflare Tunnel](../deploy/cloudflared/README.md), it can run next to DZ Manager in
the same Compose project. Add this service to `compose.yml`, put `TUNNEL_TOKEN=<your token>` in a
new file `tunnel.env`, and in the Cloudflare dashboard point the public hostname at
`http://dzm:8080`:

```yaml
  tunnel:
    image: cloudflare/cloudflared:latest
    command: tunnel --no-autoupdate run
    env_file: tunnel.env
    restart: unless-stopped
    depends_on: [dzm]
```

## 5. Create the first owner

```bash
docker compose exec dzm dzm admin invite-owner
```

Open the link it prints and set up your account. Continue with
[Getting started, step 5](getting-started.md#step-5-add-your-game-server).

## Everyday commands

| Task | Command |
|---|---|
| Stop / start | `docker compose stop` / `docker compose start` |
| See the logs | `docker compose logs -f dzm` |
| Check health | `docker compose exec dzm dzm health` |
| Install a map image from the command line | `docker compose exec dzm dzm map install --world chernarusplus` |
| Update to a new release | Download the new source, then `docker compose up -d --build` |

## Your data

Everything lives in the `dzm-data` volume: the database, map tiles and archives. Back it up
together with `DZM_SECRET_KEY`; without the key the stored game-server passwords cannot be read.
See [Updates and backups](updates-and-backups.md#docker).

The Compose file publishes the panel on `127.0.0.1:8080` only, so it is never exposed without
HTTPS in front of it.
