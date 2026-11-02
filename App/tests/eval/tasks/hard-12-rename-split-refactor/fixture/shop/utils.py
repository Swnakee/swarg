import re


def fmt_money(cents):
    sign = "-" if cents < 0 else ""
    c = abs(cents)
    return "%s$%s.%02d" % (sign, format(c // 100, ","), c % 100)


def parse_money(text):
    t = text.strip()
    m = re.fullmatch(r"(-?)\$?(\d{1,3}(?:,\d{3})+|\d+)(?:\.(\d{2}))?", t)
    if not m:
        raise ValueError("bad money value: %r" % text)
    cents = int(m.group(2).replace(",", "")) * 100 + int(m.group(3) or 0)
    return -cents if m.group(1) else cents


def slug(text):
    return re.sub(r"[^a-z0-9]+", "-", text.lower()).strip("-")


def truncate(text, n):
    return text if len(text) <= n else text[:n - 1] + "~"
