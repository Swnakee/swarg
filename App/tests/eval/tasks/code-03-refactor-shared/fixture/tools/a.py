"""Script a: reads a key=value line and prints something from it."""


def parse_kv(line):
    """'a=1 b=2' -> {'a': '1', 'b': '2'}; tokens without '=' are skipped."""
    out = {}
    for tok in line.split():
        if "=" not in tok:
            continue
        k, v = tok.split("=", 1)
        out[k.strip()] = v.strip()
    return out


if __name__ == "__main__":
    kv = parse_kv("host=db1 port=5432 user=app")
    print(kv["host"], kv["port"])
