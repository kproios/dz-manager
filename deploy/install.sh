#!/bin/bash
# Native Debian installer. Run as root from a checkout; re-running preserves secrets/state.
set -Eeuo pipefail
umask 077
[[ $EUID == 0 ]] || { echo 'Run as root' >&2; exit 1; }
domain=
ssh_address=
while (($#)); do
    case "$1" in
        --domain) domain=$2; shift 2;;
        --ssh-address) ssh_address=$2; shift 2;;
        *) echo 'Usage: install.sh --domain example.org [--ssh-address LAN_IP]' >&2; exit 1;;
    esac
done
[[ $domain =~ ^[a-z0-9.-]+$ && $domain == *.* ]] || exit 1
[[ -z $ssh_address || $ssh_address =~ ^192\.168\.[0-9]{1,3}\.[0-9]{1,3}$ || $ssh_address =~ ^10\.[0-9.]+$ ]] || exit 1
here=$(cd -- "$(dirname -- "$0")" && pwd)
source /etc/os-release
[[ $ID == debian ]] || { echo 'Only Debian is supported' >&2; exit 1; }
export DEBIAN_FRONTEND=noninteractive
export LC_ALL=C.UTF-8
apt-get update -qq
apt-get install -y --no-install-recommends ca-certificates curl jq sqlite3 unattended-upgrades apt-listchanges needrestart
install -d -m 0755 /usr/share/keyrings
curl --fail --silent --show-error --max-time 60 https://pkg.cloudflare.com/cloudflare-main.gpg -o /usr/share/keyrings/cloudflare-main.gpg
chmod 0644 /usr/share/keyrings/cloudflare-main.gpg
printf 'deb [signed-by=/usr/share/keyrings/cloudflare-main.gpg] https://pkg.cloudflare.com/cloudflared any main\n' >/etc/apt/sources.list.d/cloudflared.list
chmod 0644 /etc/apt/sources.list.d/cloudflared.list
apt-get update -qq
apt-get install -y --no-install-recommends cloudflared
timedatectl set-timezone UTC
if ! systemd-detect-virt --container --quiet; then
    # A container shares the hypervisor clock; timesyncd cannot set it (CAP_SYS_TIME).
    systemctl enable --now systemd-timesyncd.service
else
    # PVE mounts these itself. Without nesting, duplicate systemd mounts are denied.
    systemctl mask tmp.mount run-lock.mount dev-mqueue.mount
    systemctl reset-failed tmp.mount run-lock.mount dev-mqueue.mount
fi
getent group dzm >/dev/null || groupadd --system dzm
id dzm >/dev/null 2>&1 || useradd --system --gid dzm --no-create-home --home-dir /nonexistent --shell /usr/sbin/nologin dzm
install -d -o root -g root -m 0755 /opt/dzm /opt/dzm/releases
install -d -o root -g dzm -m 0750 /etc/dzm
install -d -o dzm -g dzm -m 0700 /var/lib/dzm
install -d -o dzm -g dzm -m 0700 /var/lib/dzm/backups /var/lib/dzm/tiles
install -d -o root -g dzm -m 0750 /var/lib/dzm/updater
if [[ ! -f /etc/dzm/dzm.env ]]; then
    secret=$(head -c32 /dev/urandom | base64)
    cat >/etc/dzm/dzm.env <<EOF
DZM_LISTEN=127.0.0.1:8080
DZM_DATA_DIR=/var/lib/dzm
DZM_SECRET_KEY=$secret
DZM_PUBLIC_URL=https://dzm.$domain
DZM_INGEST_PUBLIC_URL=https://dzm-ingest.$domain
DZM_OIDC_ISSUER=
DZM_OIDC_CLIENT_ID=
DZM_OIDC_CLIENT_SECRET=
DZM_TZ=UTC
DZM_LOG_LEVEL=info
DZM_ALERT_WEBHOOK=
DZM_BACKUP_PING_URL=
EOF
    # Display to the operator's terminal only; never to stdout/journald/CI logs.
    if [[ -t 1 && -w /dev/tty ]]; then
        printf 'Store DZM_SECRET_KEY in your password manager now: %s\n' "$secret" >/dev/tty
    else
        echo 'Secret generated privately. Retrieve DZM_SECRET_KEY directly in your terminal and store it in your password manager.'
    fi
    unset secret
fi
chown root:dzm /etc/dzm/dzm.env
chmod 0640 /etc/dzm/dzm.env
if [[ ! -f /etc/dzm/updater.env ]]; then
    printf '# Fine-grained PAT, this repository only, Contents read-only\nGITHUB_TOKEN=\n' >/etc/dzm/updater.env
fi
chown root:root /etc/dzm/updater.env
chmod 0600 /etc/dzm/updater.env
install -o root -g root -m 0755 "$here/bin/dzm-update" "$here/bin/dzm-backup" /usr/local/sbin/
install -o root -g root -m 0644 "$here"/systemd/* /etc/systemd/system/
cat >/etc/apt/apt.conf.d/52unattended-dzm <<'EOF'
Unattended-Upgrade::Origins-Pattern {
    "origin=Debian,codename=${distro_codename},label=Debian";
    "origin=Debian,codename=${distro_codename}-updates,label=Debian";
    "origin=Debian,codename=${distro_codename}-security,label=Debian-Security";
    "origin=cloudflared";
};
Unattended-Upgrade::Automatic-Reboot "true";
Unattended-Upgrade::Automatic-Reboot-Time "04:45";
Unattended-Upgrade::Remove-Unused-Dependencies "true";
Unattended-Upgrade::Remove-New-Unused-Dependencies "true";
EOF
cat >/etc/apt/apt.conf.d/20auto-upgrades <<'EOF'
APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "1";
APT::Periodic::AutocleanInterval "7";
EOF
install -d -m 0755 /etc/systemd/journald.conf.d /etc/needrestart/conf.d
printf '[Journal]\nSystemMaxUse=200M\n' >/etc/systemd/journald.conf.d/dzm.conf
printf '$nrconf{restart} = '\''a'\'';\n' >/etc/needrestart/conf.d/dzm.conf
chmod 0644 /etc/apt/apt.conf.d/{52unattended-dzm,20auto-upgrades} /etc/systemd/journald.conf.d/dzm.conf /etc/needrestart/conf.d/dzm.conf
if [[ -n $ssh_address ]]; then
    install -d -m 0755 /etc/ssh/sshd_config.d
    cat >/etc/ssh/sshd_config.d/00-dzm.conf <<EOF
ListenAddress $ssh_address
PasswordAuthentication no
KbdInteractiveAuthentication no
PermitRootLogin prohibit-password
AllowUsers root@${ssh_address%.*}.0/24
EOF
    chmod 0644 /etc/ssh/sshd_config.d/00-dzm.conf
    install -d -m 0755 /run/sshd
    /usr/sbin/sshd -t
    install -d -m 0755 /etc/systemd/system/ssh.service.d
    # ifupdown owns the static address; network.target alone can precede address assignment.
    printf '[Unit]\nAfter=networking.service\n' >/etc/systemd/system/ssh.service.d/dzm-network.conf
    chmod 0644 /etc/systemd/system/ssh.service.d/dzm-network.conf
    systemctl daemon-reload
    systemctl disable --now ssh.socket 2>/dev/null || true
    systemctl enable ssh.service
    systemctl reload-or-restart ssh.service || systemctl restart ssh.service
else
    # No SSH exposure by default; console access is sufficient for management.
    systemctl disable --now ssh.socket ssh.service 2>/dev/null || true
fi
# The stock LXC template may include a mail listener; this host does not send mail.
systemctl disable --now postfix.service 2>/dev/null || true
systemctl daemon-reload
systemctl restart systemd-journald.service
systemctl enable dzm.service
systemctl enable --now dzm-update.timer dzm-backup.timer apt-daily.timer apt-daily-upgrade.timer unattended-upgrades.service
if [[ -x /opt/dzm/current/dzm ]]; then systemctl start dzm.service; fi
echo 'Installation complete. Configure OIDC, read-only GitHub token and tunnel before public use.'
