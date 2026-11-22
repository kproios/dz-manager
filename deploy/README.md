# Install on Linux (Debian)

The installer turns a dedicated Debian machine into a DZ Manager host: it creates a service user,
installs the panel as a system service, and adds automatic updates, daily database backups and
operating-system security updates. Use it on a machine or container that does nothing else.

> **Read this first.** The installer is meant for a dedicated machine you can reach from its own
> console (a home server, a virtual machine or a container with console access). For safety it
> **turns SSH off** unless you pass `--ssh-address` with a private network address (`192.168.x.x`
> or `10.x.x.x`), in which case SSH only listens there and only accepts keys. On a cloud VM that
> you can only reach over SSH, use [Docker](../docs/docker.md) instead.

## Requirements

- Debian 12 or newer, on x86-64 or ARM64, with internet access
- Root access on its console
- A domain name. The installer prepares two addresses under it: `dzm.<domain>` for the panel and
  `dzm-ingest.<domain>` for the game server. You can change both afterwards.

## 1. Run the installer

Download or clone this repository onto the machine, then as root:

```bash
bash deploy/install.sh --domain example.com
# optional: keep SSH on a private address
bash deploy/install.sh --domain example.com --ssh-address 10.0.0.20
```

It installs the required packages (including the Cloudflare tunnel program), creates the `dzm`
user, writes the settings file `/etc/dzm/dzm.env` with a freshly generated `DZM_SECRET_KEY`, and
enables the update and backup timers. **Copy the secret key it shows into your password manager.**
Running the installer again keeps your settings and data.

## 2. Allow the updater to download releases

The updater downloads releases from GitHub with a personal access token. Create a
[fine-grained token](https://github.com/settings/personal-access-tokens/new) with read-only access
to public repositories (no other permissions), and put it in `/etc/dzm/updater.env`:

```
GITHUB_TOKEN=github_pat_...
```

Then install the current release:

```bash
systemctl start dzm-update.service
systemctl status dzm.service
curl -s http://127.0.0.1:8080/ready          # {"status":"ready","version":"v…"}
```

The updater checks for new releases every 30 minutes, verifies the download, backs up the
database first and rolls back automatically if the new version does not become ready.

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

## Where things are

| Path | Contents |
|---|---|
| `/etc/dzm/dzm.env` | Settings and the secret key (readable by root and the service only) |
| `/etc/dzm/updater.env` | The updater's GitHub token |
| `/etc/dzm/hold` | Create this file to pause automatic updates; delete it to resume |
| `/opt/dzm/current/dzm` | The program (the last three releases are kept for rollback) |
| `/var/lib/dzm/` | Database, map tiles and backups |

Useful commands: `journalctl -u dzm.service -f` (logs), `systemctl list-timers 'dzm-*'` (next
update and backup). More in [Updates and backups](../docs/updates-and-backups.md).

For the exact host and release rules see the
[host and release contract](../docs/contracts/host-and-release.md).
