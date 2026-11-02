import csv
import os
from dataclasses import dataclass
from decimal import Decimal


@dataclass
class Product:
    sku: str
    price: Decimal
    currency: str
    discount_pct: Decimal


def load_products(data_dir):
    products = []
    with open(os.path.join(data_dir, "products.csv"), newline="") as f:
        for row in csv.DictReader(f):
            disc = row["discount_pct"].strip()
            products.append(Product(row["sku"].strip(), Decimal(row["price"].strip()),
                                    row["currency"], Decimal(disc) if disc else Decimal(0)))
    return products
