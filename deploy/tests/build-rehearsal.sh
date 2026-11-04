#!/bin/bash
# Run on a Linux BUILD host with gcc and static libc, never on the dzm LXC.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
out=${1:?Usage: build-rehearsal.sh OUTPUT_DIRECTORY}
mkdir -p "$out"
for n in 100 101 102 103; do
    tag=v0.0.$n
    stage=$(mktemp -d)
    broken=0
    [[ $n != 102 ]] || broken=1
    gcc -O2 -static -Wall -Wextra -DVERSION=\"$tag\" -DBROKEN=$broken "$here/stub.c" -o "$stage/dzm"
    printf '%s\n' "$tag" >"$stage/VERSION"
    mkdir "$stage/LICENSES"
    printf 'Infrastructure rehearsal stub; not application code.\n' >"$stage/LICENSES/stub.txt"
    mkdir -p "$out/$tag"
    tar -czf "$out/$tag/dzm_linux_amd64.tar.gz" -C "$stage" dzm VERSION LICENSES
    (cd "$out/$tag" && sha256sum dzm_linux_amd64.tar.gz >SHA256SUMS)
    rm -rf "$stage"
done
echo v0.0.100 >"$out/latest"
