import os

from .config import LOG_PATH


def log(level, message):
    os.makedirs(os.path.dirname(LOG_PATH), exist_ok=True)
    with open(LOG_PATH, "a") as f:
        f.write("%s %s\n" % (level, message))
