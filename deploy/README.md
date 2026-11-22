# Install on Linux (Debian)

The installer turns a Debian machine into a DZ Manager host: it creates a service user, installs
the panel as a system service, downloads the current release, and adds automatic updates, daily
database backups and operating-system security updates.

Use a machine that does nothing else (a home server, a VM or a VPS). The installer sets the
system clock to UTC, enables automatic security updates and allows the machine to reboot itself
at 04:45 when an update needs it.

## Requirements

- Debian 12 or newer, on x86-64 or ARM64, with internet access
- Root access (directly or with `sudo -i`)
- A domain name. The installer prepares two addresses under it: `dzm.<domain>` for the panel and
  `dzm-ingest.<domain>` for the game server. You can change both afterwards.

## 1. Run the installer

Download or clone this repository onto the machine, then as root:

```bash
bash deploy/install.sh --domain example.com
```

It installs the required packages (including the Cloudflare tunnel program), creates the `dzm`
user, writes the settings file `/etc/dzm/dzm.env` with a freshly generated `DZM_SECRET_KEY`,
downloads and starts the current release, and enables the update and backup timers. **Copy the
secret key it shows into your password manager.** Running the installer again keeps your settings
and data.

Your SSH settings are not changed.

### Optional: SSH hardening

On a machine in your home or office network you can also restrict SSH:

```bash
bash deploy/install.sh --domain example.com --ssh-address 10.0.0.20
```

SSH then listens only on that private address, accepts only `root` from the same /24 network,
and only with an SSH key (passwords are turned off). The installer refuses this unless the
address belongs to the machine and `/root/.ssh/authorized_keys` already contains a key. Do not use
it on a cloud VM that you reach over the internet.

## 2. Check that it runs

```bash
systemctl status dzm.service
curl -s http://127.0.0.1:8080/ready          # {"status":"ready","version":"v…"}
```

If the release could not be downloaded during installation (for example without internet
access), run `systemctl start dzm-update.service` once the connection works.

## 3. Review the settings

Open `/etc/dzm/dzm.env` and check at least `DZM_PUBLIC_URL`, `DZM_INGEST_PUBLIC_URL` and `DZM_TZ`
(your timezone). Every setting is explained in [Configuration](../docs/configuration.md). After a
change: `systemctl restart dzm.service`.

## 4. Publish it over HTTPS

The panel listens on `127.0.0.1:8080` only. Make it reachable with
[Cloudflare Tunnel](cloudflared/README.md) (already installed) or another option from
[Hosting options](../docs/hosting.md#getting-https).

## 5. Create the first owner

```bash
systemd-run --pipe --wait -p User=dzm -p EnvironmentFile=/etc/dzm/dzm.env \
  /opt/dzm/current/dzm admin invite-owner
```

Open the link it prints, then continue with
[Getting started, step 5](../docs/getting-started.md#step-5-add-your-game-server).

## Automatic updates

The updater checks GitHub for a new release every 30 minutes, verifies the download, backs up the
database first and rolls back automatically if the new version does not become ready. Details in
[Updates and backups](../docs/updates-and-backups.md).

It needs no account or token: releases are public. GitHub allows 60 anonymous requests per hour
per IP address and a check uses one, so this is only a concern on addresses shared by many users.
In that case add a [fine-grained token](https://github.com/settings/personal-access-tokens/new)
with read-only access to public repositories and no other permissions to `/etc/dzm/updater.env`:

```
GITHUB_TOKEN=github_pat_...
```

## Where things are

| Path | Contents |
|---|---|
| `/etc/dzm/dzm.env` | Settings and the secret key (readable by root and the service only) |
| `/etc/dzm/updater.env` | Optional GitHub token for the updater |
| `/etc/dzm/hold` | Create this file to pause automatic updates; delete it to resume |
| `/opt/dzm/current/dzm` | The program (the last three releases are kept for rollback) |
| `/var/lib/dzm/` | Database, map tiles and backups |

Useful commands: `journalctl -u dzm.service -f` (logs), `systemctl list-timers 'dzm-*'` (next
update and backup). More in [Updates and backups](../docs/updates-and-backups.md).

For the exact host and release rules see the
[host and release contract](../docs/contracts/host-and-release.md).
