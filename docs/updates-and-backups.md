# Updates and backups

## What updates where

| Part | How it updates |
|---|---|
| DZ Manager on a Linux install | Automatically. The updater checks every 30 minutes. |
| DZ Manager in Docker | When you download the new source and rebuild (below). |
| @DZMBridge on the game server | By hand, when a release says the bridge changed. **Settings → Server** shows the installed version. |

Release notes are on the [releases page](../../../releases).

## Linux install

The updater only installs full releases (never pre-releases). For each update it:

1. downloads the release and checks it against the published `SHA256SUMS`,
2. makes a database snapshot,
3. switches to the new version and waits for it to report ready,
4. switches back and restores the snapshot if the new version does not start.

The last three versions stay on disk. Useful commands:

```bash
systemctl start dzm-update.service        # check for an update now
cat /var/lib/dzm/updater/status.json      # result of the last check
touch /etc/dzm/hold                       # pause automatic updates
rm /etc/dzm/hold                          # resume them
```

The panel also shows the last update result, and warns before an optional updater GitHub token
expires.
If you set `DZM_ALERT_WEBHOOK`, a rolled-back update sends an alert.

### Backups

A daily snapshot of the database is written to `/var/lib/dzm/backups/` at 03:15 (host time) and
checked for integrity; the last 14 days are kept, plus the snapshots taken before each update.
Copy that folder somewhere else from time to time (another disk, cloud storage). Map tiles can
always be downloaded again.

To restore: stop the service, copy a snapshot over `/var/lib/dzm/dzm.db`, delete
`dzm.db-wal` and `dzm.db-shm` next to it, and start the service again. The `DZM_SECRET_KEY` in
`/etc/dzm/dzm.env` must be the same one the backup was made with.

## Docker

Update by getting the new release's source into the same folder, then:

```bash
docker compose up -d --build
```

Your data stays in the `dzm-data` volume. To back it up, stop the container and copy the volume,
for example:

```bash
docker compose stop dzm
docker run --rm -v docker_dzm-data:/data -v "$PWD":/backup busybox tar czf /backup/dzm-data.tgz -C /data .
docker compose start dzm
```

(`docker volume ls` shows the exact volume name; it starts with the folder name.) Keep the backup
together with your `DZM_SECRET_KEY`.

## The game-side mod

To update @DZMBridge: stop the game server (or let it restart), replace the `@DZMBridge` folder
with the new one, and start it. A running server locks its mod files, so the swap only works
while it is stopped. Check **Settings → Server** for the new version afterwards. See
[Game server setup](install-game-side.md#updating-and-removing).
