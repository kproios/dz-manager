#!/bin/bash
# On the NEW disposable application host, before real releases/data exist.
set -euo pipefail
[[ $(cat /opt/dzm/current/VERSION) == v0.0.100 ]]
systemctl stop dzm-update.timer
src=/root/dzm-rehearsal
export DZM_UPDATE_SOURCE=dir:$src
echo '=== A installed ==='
cat /var/lib/dzm/updater/status.json
echo v0.0.101 >"$src/latest"
/usr/local/sbin/dzm-update
echo '=== B installed ==='
cat /var/lib/dzm/updater/status.json
sqlite3 /var/lib/dzm/dzm.db 'PRAGMA journal_mode=WAL; DROP TABLE IF EXISTS rehearsal; CREATE TABLE rehearsal(value TEXT); INSERT INTO rehearsal VALUES ("before-update"); PRAGMA wal_checkpoint(TRUNCATE);'
chown dzm:dzm /var/lib/dzm/dzm.db*
before=$(sha256sum /var/lib/dzm/dzm.db | cut -d' ' -f1)
echo "DB_BEFORE=$before"
echo v0.0.102 >"$src/latest"
if /usr/local/sbin/dzm-update; then echo 'ERROR: broken version unexpectedly succeeded'; exit 1; fi
echo '=== C rolled back ==='
cat /var/lib/dzm/updater/status.json
after=$(sha256sum /var/lib/dzm/dzm.db | cut -d' ' -f1)
echo "DB_AFTER=$after"
snapshot=$(sha256sum /var/lib/dzm/backups/pre-v0.0.102.db | cut -d' ' -f1)
echo "SNAPSHOT=$snapshot"
# SQLite .backup may change header counters; the restored bytes must match the snapshot.
[[ $snapshot == "$after" ]]
[[ $(sqlite3 /var/lib/dzm/dzm.db 'SELECT COUNT(*) FROM rehearsal;') == 1 ]]
[[ $(cat /opt/dzm/current/VERSION) == v0.0.101 ]]
/usr/local/sbin/dzm-update
jq -e '.last_result == "skipped_blocked"' /var/lib/dzm/updater/status.json
touch /etc/dzm/hold
/usr/local/sbin/dzm-update
jq -e '.last_result == "skipped_hold"' /var/lib/dzm/updater/status.json
rm /etc/dzm/hold
systemctl start dzm-backup.service
copy=$(mktemp /var/lib/dzm/backups/.restore-test.XXXXXX)
sqlite3 /var/lib/dzm/backups/daily-$(date +%Y%m%d).db ".backup '$copy'"
[[ $(sqlite3 "$copy" 'PRAGMA integrity_check;') == ok ]]
[[ $(sqlite3 "$copy" 'SELECT value FROM rehearsal;') == before-update ]]
rm "$copy"
echo 'DAILY_SNAPSHOT_AND_COPY_RESTORE=ok'
unset DZM_UPDATE_SOURCE
systemctl start dzm-update.timer
echo 'Rehearsal finished; source override exists only in this ended process'
