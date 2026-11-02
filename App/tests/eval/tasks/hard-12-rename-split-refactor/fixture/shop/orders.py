from dataclasses import dataclass


@dataclass
class Order:
    id: int
    customer_id: int
    amount_cents: int
    status: str = "open"


def place_order(repo, customer_id, amount_cents):
    repo.get_customer(customer_id)  # raises KeyError for an unknown customer
    if amount_cents <= 0:
        raise ValueError("order amount must be positive")
    order = Order(repo.next_order_id(), customer_id, amount_cents)
    repo.add_order(order)
    return order


def orders_for_customer(repo, customer_id):
    return [o for o in repo.orders() if o.customer_id == customer_id]


def total_for_customer(repo, customer_id):
    return sum(o.amount_cents for o in orders_for_customer(repo, customer_id) if o.status != "cancelled")
