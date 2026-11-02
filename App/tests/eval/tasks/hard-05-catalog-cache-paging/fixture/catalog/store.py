class Store:
    def __init__(self, items=None):
        self.items = {}
        self.scans = 0
        for it in items or []:
            self.items[it["id"]] = dict(it)

    def scan(self):
        """All items (full scan)."""
        self.scans += 1
        return [dict(v) for v in self.items.values()]

    def get(self, item_id):
        return self.items[item_id]
