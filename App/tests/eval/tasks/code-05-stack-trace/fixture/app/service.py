from .client import Client
from .config import load_config


def build_client(cfg):
    return Client(cfg["timeout"], cfg["retries"])


def run():
    cfg = load_config()
    client = build_client(cfg)
    print("%s: %s" % (cfg["name"], client.describe()))
