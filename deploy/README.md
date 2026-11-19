# Native Linux installation

Run the installer as root on a dedicated Debian host, supplying your own domain:

```bash
bash deploy/install.sh --domain example.com
```

It prepares the service, update and backup timers, and private configuration files.
The application listens on loopback. Configure HTTPS through a reverse proxy or
the [tunnel setup](cloudflared/README.md), then set the public panel and ingest
URLs in `/etc/dzm/dzm.env`.

Accounts are built in. Leave all `DZM_OIDC_*` credentials empty unless you want
OpenID Connect, in which case configure the issuer, client ID and client secret
together. The callback is `<DZM_PUBLIC_URL>/auth/callback`. TOTP is optional per
account. Set `DZM_TZ` to your display and schedule timezone.

Configure a repository-scoped read-only GitHub token in `/etc/dzm/updater.env`
for private release downloads. Install the current release using
`systemctl start dzm-update.service`, then check `dzm.service` and `/ready`.
Run `dzm admin invite-owner` with the service environment to create the first
account. Keep the generated encryption key and backups in private storage.

See the [host and release contract](../docs/contracts/host-and-release.md) for
configuration, update verification and filesystem permissions. The game-side
bridge is installed separately; see [game-side installation](../docs/install-game-side.md).
Docker installation is described in [docs/docker.md](../docs/docker.md).
