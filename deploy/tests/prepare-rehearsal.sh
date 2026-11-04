#!/bin/bash
set -euo pipefail
systemctl stop dzm-update.timer
version=$(cat /opt/dzm/current/VERSION)
[[ $version == v0.0.100 || $version == v0.0.101 ]]
jq -n --arg installed "$version" '{installed:$installed,blocked_versions:[],token_expires_at:"",latest_seen:""}' >/tmp/dzm-rehearsal-status.json
install -o root -g dzm -m 0640 /tmp/dzm-rehearsal-status.json /var/lib/dzm/updater/status.json
echo v0.0.100 >/root/dzm-rehearsal/latest
DZM_UPDATE_SOURCE=dir:/root/dzm-rehearsal /usr/local/sbin/dzm-update
exec bash /root/rehearse.sh
