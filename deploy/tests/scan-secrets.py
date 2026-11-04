"""Run on the build host with a source tar.gz argument. Prints filenames, never secret values."""
import io
import shlex
import subprocess
import sys
import tarfile

secrets = []
for path, names in [('/etc/dzm/dzm.env', {'DZM_SECRET_KEY', 'DZM_OIDC_CLIENT_SECRET'}),
                    ('/etc/dzm/updater.env', {'GITHUB_TOKEN'})]:
    content = subprocess.check_output(['cat', path], text=True)
    for line in content.splitlines():
        key, _, value = line.partition('=')
        if key in names:
            parsed = shlex.split(value)
            if parsed:
                secrets.append(parsed[0].encode())
secrets.append(subprocess.check_output(['cat', '/etc/dzm/cloudflared-token']).strip())
assert len(secrets) == 4 and all(len(value) >= 20 for value in secrets)
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
print('PASS: no live encryption key, OIDC client secret, GitHub token or tunnel token in source archive')
