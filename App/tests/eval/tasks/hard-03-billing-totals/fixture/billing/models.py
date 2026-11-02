from dataclasses import dataclass, field
from decimal import Decimal


@dataclass
class Line:
    sku: str
    qty: int
    unit_price: Decimal
    taxable: bool = True


@dataclass
class Order:
    id: str
    customer_id: str
    customer_name: str
    region: str
    loyalty: bool = False
    lines: list = field(default_factory=list)


def order_from_dict(d):
    return Order(
        id=d["id"],
        customer_id=d["customer_id"],
        customer_name=d["customer_name"],
        region=d["region"],
        loyalty=bool(d.get("loyalty", False)),
        lines=[Line(x["sku"], int(x["qty"]), Decimal(str(x["unit_price"])), x.get("taxable", True))
               for x in d.get("lines", [])],
    )
