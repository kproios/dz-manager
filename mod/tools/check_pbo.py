#!/usr/bin/env python3
"""Structural checks for the built PBO (CI): header, entries, trailer SHA1,
config.cpp module paths, and cheap Enforce Script sanity checks (balanced
braces/parens, no 'class' field names). It cannot replace a real DayZ compile."""
import hashlib, re, struct, sys

def read_cstr(b, i):
    j = b.index(0, i)
    return b[i:j].decode("ascii"), j + 1

def main(path):
    b = open(path, "rb").read()
    body, trailer = b[:-21], b[-21:]
    assert trailer[0] == 0 and hashlib.sha1(body).digest() == trailer[1:], "bad SHA1 trailer"
    name, i = read_cstr(b, 0)
    assert name == "" and struct.unpack_from("<I", b, i)[0] == 0x56657273, "missing Vers header"
    i += 20
    props = {}
    while True:
        k, i = read_cstr(b, i)
        if not k:
            break
        v, i = read_cstr(b, i)
        props[k] = v
    assert props.get("prefix") == "DZMBridge", props
    entries = []
    while True:
        n, i = read_cstr(b, i)
        _, _, _, _, size = struct.unpack_from("<IIIII", b, i)
        i += 20
        if not n:
            break
        entries.append((n, size))
    off = i
    files = {}
    for n, size in entries:
        files[n] = b[off:off + size].decode("utf-8")
        off += size
    assert "config.cpp" in files, "config.cpp missing"
    for mod in ("3_Game", "4_World", "5_Mission"):
        assert f"DZMBridge/scripts/{mod}" in files["config.cpp"], mod
        assert any(n.startswith(f"scripts\\{mod}\\") for n in files), f"no scripts in {mod}"
    for n, src in files.items():
        if not n.endswith(".c"):
            continue
        code = re.sub(r'"(\\.|[^"\\])*"', '""', re.sub(r"//.*", "", src))
        for o, c in ("{}", "()", "[]"):
            assert code.count(o) == code.count(c), f"{n}: unbalanced {o}{c}"
        assert not re.search(r"\b(string|int|float|bool)\s+class\s*;", code), f"{n}: 'class' used as identifier"
    print(f"ok: {len(entries)} files, prefix {props['prefix']}")

if __name__ == "__main__":
    main(sys.argv[1])
