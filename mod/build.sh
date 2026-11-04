#!/usr/bin/env bash
# Builds dist/DZMBridge.zip (unsigned server-side mod) without DayZ Tools.
set -euo pipefail
cd "$(dirname "$0")"
VERSION="${1:-dev}"
out=../dist/mod
rm -rf "$out" && mkdir -p "$out/@DZMBridge/addons"
python3 tools/pack_pbo.py DZMBridge/src "$out/@DZMBridge/addons/DZMBridge.pbo" --prefix DZMBridge >/dev/null
sed "s/^version = .*/version = \"${VERSION#v}\";/" DZMBridge/mod.cpp > "$out/@DZMBridge/mod.cpp"
cp DZMBridge/LICENSE-THIRD-PARTY.txt "$out/@DZMBridge/"
python3 tools/check_pbo.py "$out/@DZMBridge/addons/DZMBridge.pbo"
(cd "$out" && rm -f ../DZMBridge.zip && python3 -c "import shutil; shutil.make_archive('../DZMBridge', 'zip', '.', '@DZMBridge')")
echo "built dist/DZMBridge.zip"
