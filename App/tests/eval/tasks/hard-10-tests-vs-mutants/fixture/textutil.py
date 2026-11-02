"""Small text and list helpers. Behaviour is documented in README.md."""
import re
import unicodedata

_UNITS = (("w", 604800), ("d", 86400), ("h", 3600), ("m", 60), ("s", 1))


def slugify(text):
    text = unicodedata.normalize("NFKD", text).encode("ascii", "ignore").decode("ascii")
    text = re.sub(r"[^a-z0-9]+", "-", text.lower()).strip("-")
    return text[:50].rstrip("-")


def truncate(text, width, suffix="..."):
    if width < 0:
        raise ValueError("width must be >= 0")
    if len(text) <= width:
        return text
    if width < len(suffix):
        return suffix[:width]
    return text[:width - len(suffix)] + suffix


def parse_duration(s):
    s = s.strip().lower()
    total = 0
    pos = 0
    prev = -1
    while pos < len(s):
        m = re.compile(r"(\d+)([a-z])\s*").match(s, pos)
        if not m:
            raise ValueError("bad duration %r" % s)
        idx = next((i for i, (u, _) in enumerate(_UNITS) if u == m.group(2)), None)
        if idx is None:
            raise ValueError("unknown unit %r" % m.group(2))
        if idx <= prev:
            raise ValueError("units out of order in %r" % s)
        prev = idx
        total += int(m.group(1)) * _UNITS[idx][1]
        pos = m.end()
    if prev < 0:
        raise ValueError("empty duration")
    return total


def chunk(seq, n):
    if n < 1:
        raise ValueError("n must be >= 1")
    return [list(seq[i:i + n]) for i in range(0, len(seq), n)]


def moving_average(values, k):
    if k < 1:
        raise ValueError("k must be >= 1")
    return [sum(values[i:i + k]) / k for i in range(len(values) - k + 1)]
