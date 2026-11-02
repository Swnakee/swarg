from .customer import Customer


class CustomerRepo:
    def __init__(self, customers=None, orders=None):
        self._customers = {c.id: c for c in customers or []}
        self._orders = list(orders or [])

    def add_customer(self, customer: Customer):
        self._customers[customer.id] = customer

    def get_customer(self, customer_id):
        try:
            return self._customers[customer_id]
        except KeyError:
            raise KeyError("customer %d not found" % customer_id)

    def customers(self):
        return sorted(self._customers.values(), key=lambda c: c.id)

    def next_customer_id(self):
        return max(self._customers, default=0) + 1

    def add_order(self, order):
        self._orders.append(order)

    def orders(self):
        return list(self._orders)

    def next_order_id(self):
        return max((o.id for o in self._orders), default=0) + 1
