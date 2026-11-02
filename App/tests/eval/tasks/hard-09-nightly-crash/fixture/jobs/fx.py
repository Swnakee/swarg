import json
import os
from decimal import Decimal

from .log import log


def load_rates(data_dir):
    with open(os.path.join(data_dir, "rates.json")) as f:
        raw = json.load(f)
    return {code.upper(): Decimal(str(v)) for code, v in raw.items()}


def get_rate(rates, currency):
    if currency == "EUR":
        return Decimal(1)
    rate = rates.get(currency, Decimal(0))
    if rate == 0:
        log("INFO", "fx: using fallback rate for %s" % currency)
    return rate
