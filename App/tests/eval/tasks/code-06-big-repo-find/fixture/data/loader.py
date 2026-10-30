from .rows import ROWS


def load_rows():
    return [dict(r) for r in ROWS]
