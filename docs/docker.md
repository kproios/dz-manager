# Running DZ Manager with Docker

Docker is an alternative installation method. DZ Manager is one
static binary with no runtime dependencies, so the image is small (distroless, non-root, read-only
root filesystem) and costs nothing to maintain. The systemd install with the self-updater
(`deploy/install.sh`) stays the reference setup; with Docker you update by pulling or rebuilding
the image.

```bash
cd deploy/docker
cp dzm.env.example dzm.env   # fill it in
docker compose up -d
docker compose logs -f dzm
```

- All state is in the `/data` volume: the SQLite database, map tiles and archives. Back it up
  together with `DZM_SECRET_KEY` (without the key the stored game-server secrets can't be read).
- The container listens on all interfaces inside the container (`DZM_CONTAINER=1`); compose
  publishes it on `127.0.0.1:8080` only. Put a reverse proxy (Caddy, nginx, Traefik) or a tunnel
  (Cloudflare Tunnel) in front for HTTPS. The game server's @DZMBridge must reach
  `DZM_INGEST_PUBLIC_URL` over HTTPS.
- Map images: Settings → Map & tracking → Download and install, or
  `docker compose exec dzm dzm map install --world chernarusplus`.
- Health: `dzm health` (used by the image's `HEALTHCHECK`) calls `/ready`.
- Build with a version label: `docker build --build-arg VERSION=v0.2.0 -t dz-manager:v0.2.0 .`
