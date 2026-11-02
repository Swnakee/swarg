import re
from dataclasses import dataclass

EMAIL = re.compile(r"^[^@\s]+@[^@\s]+\.[a-z]{2,}$", re.I)


@dataclass
class Customer:
    id: int
    name: str
    email: str
    balance_cents: int = 0
    tier: str = "basic"


def new_customer(next_id, name, email):
    if not EMAIL.match(email):
        raise ValueError("invalid customer email: %s" % email)
    return Customer(next_id, name.strip(), email.lower())
