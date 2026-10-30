import json

DEFAULTS = {"name": "demo", "retries": 3}


def load_config(path="settings.json"):
    with open(path, encoding="utf-8") as f:
        cfg = json.load(f)
    for k, v in DEFAULTS.items():
        cfg.setdefault(k, v)
    return cfg
