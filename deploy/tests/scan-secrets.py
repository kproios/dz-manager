"""Check that a source archive contains none of this host's live secrets.

Run as root on the DZ Manager host:  python3 scan-secrets.py <source.tar.gz>
Reads the secrets from /etc/dzm and prints only file names, never secret values.
"""
import os
import shlex
import sys
import tarfile

ENV_FILES = [('/etc/dzm/dzm.env', {'DZM_SECRET_KEY', 'DZM_OIDC_CLIENT_SECRET'}),
             ('/etc/dzm/updater.env', {'GITHUB_TOKEN'})]
TOKEN_FILE = '/etc/dzm/cloudflared-token'

secrets = []
for path, names in ENV_FILES:
    if not os.path.exists(path):
        continue
    with open(path, encoding='utf-8') as f:
        for line in f:
            key, _, value = line.strip().partition('=')
            if key in names:
                parsed = shlex.split(value)
                if parsed and len(parsed[0]) >= 20:
                    secrets.append(parsed[0].encode())
if os.path.exists(TOKEN_FILE):
    with open(TOKEN_FILE, 'rb') as f:
        token = f.read().strip()
    if len(token) >= 20:
        secrets.append(token)
if not secrets:
    raise SystemExit('No secrets found under /etc/dzm to compare against')

bad = []
with tarfile.open(sys.argv[1], 'r:gz') as archive:
    for member in archive.getmembers():
        if not member.isfile():
            continue
        data = archive.extractfile(member).read()
        if any(value in data for value in secrets):
            bad.append(member.name)
if bad:
    print('Secret scan FAILED in files:', ', '.join(bad))
    raise SystemExit(1)
print(f'PASS: none of {len(secrets)} live secrets appear in the source archive')
