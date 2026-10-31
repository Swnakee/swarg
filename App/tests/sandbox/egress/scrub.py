#!/usr/bin/env python3
"""Replace the API key value (env KEY_VALUE) with *** in every file under the given folder."""
import os
import sys


def scrub(root: str, key: bytes) -> int:
    """Return the number of files changed. Keys shorter than 8 bytes are refused (too ambiguous)."""
    if len(key) < 8:
        raise ValueError("key too short to scrub safely")
    changed = 0
    for dp, _, names in os.walk(root):
        for n in names:
            p = os.path.join(dp, n)
            if os.path.islink(p):
                continue
            with open(p, "rb") as f:
                data = f.read()
            if key in data:
                with open(p, "wb") as f:
                    f.write(data.replace(key, b"***"))
                changed += 1
    return changed


if __name__ == "__main__":
    if len(sys.argv) != 2:
        sys.exit("usage: KEY_VALUE=... scrub.py DIR")
    n = scrub(sys.argv[1], os.environ.get("KEY_VALUE", "").encode())
    if n:
        print("[e2e] key value scrubbed from %d result file(s)" % n)
