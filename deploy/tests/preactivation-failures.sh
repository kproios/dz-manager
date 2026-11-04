#!/bin/bash
# Candidate validation failures must never stop/change the running release or DB.
set -euo pipefail
src=$(mktemp -d /root/dzm-invalid.XXXXXX)
trap 'rm -rf -- "$src"' EXIT
before=$(readlink -f /opt/dzm/current)
db_before=$(sha256sum /var/lib/dzm/dzm.db | cut -d' ' -f1)
mkdir "$src/v0.0.104"
cp /root/dzm-rehearsal/v0.0.103/{dzm_linux_amd64.tar.gz,SHA256SUMS} "$src/v0.0.104/"
echo v0.0.104 >"$src/latest"
echo 'invalid checksum' >"$src/v0.0.104/SHA256SUMS"
if DZM_UPDATE_SOURCE=dir:$src /usr/local/sbin/dzm-update; then exit 1; fi
jq -e '.last_result == "verify_failed"' /var/lib/dzm/updater/status.json
cp /root/dzm-rehearsal/v0.0.103/SHA256SUMS "$src/v0.0.104/SHA256SUMS"
# Valid archive/checksum, but both VERSION and binary are for a different tag.
if DZM_UPDATE_SOURCE=dir:$src /usr/local/sbin/dzm-update; then exit 1; fi
jq -e '.last_result == "verify_failed"' /var/lib/dzm/updater/status.json
[[ $(readlink -f /opt/dzm/current) == "$before" ]]
[[ $(sha256sum /var/lib/dzm/dzm.db | cut -d' ' -f1) == "$db_before" ]]
curl -fsS http://127.0.0.1:8080/ready | jq -e '.version == "v0.0.103"'
echo 'CHECKSUM_AND_VERSION_FAILURE_PRESERVE_LIVE_APP_AND_DB=ok'
systemctl start dzm-update.service
