class Cart:
    def __init__(self):
        self.items = []

    def add(self, name, price, qty=1):
        self.items.append((name, float(price), int(qty)))

    def subtotal(self):
        return sum(price * qty for _, price, qty in self.items)
