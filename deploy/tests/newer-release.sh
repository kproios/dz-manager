#!/bin/bash
set -euo pipefail
tar -xzf /root/dzm-rehearsal.tar.gz -C /root/dzm-rehearsal
echo v0.0.103 >/root/dzm-rehearsal/latest
DZM_UPDATE_SOURCE=dir:/root/dzm-rehearsal /usr/local/sbin/dzm-update
jq -e '.installed == "v0.0.103" and (.blocked_versions | index("v0.0.102") != null)' /var/lib/dzm/updater/status.json
[[ $(find /opt/dzm/releases -mindepth 1 -maxdepth 1 -type d | wc -l) == 3 ]]
echo 'NEWER_RELEASE_AND_PRUNE=ok'
systemctl start dzm-update.service
cat /var/lib/dzm/updater/status.json
