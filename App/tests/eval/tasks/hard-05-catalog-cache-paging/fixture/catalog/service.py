import copy
import time

from .cache import Cache
from .pagination import paginate
from .store import Store


class Service:
    def __init__(self, items=None, clock=time.monotonic, cache_size=64, ttl=30):
        self.store = Store(items)
        self.cache = Cache(cache_size, ttl, clock)

    # -- writes ------------------------------------------------------------
    def add_item(self, item):
        if item["id"] in self.store.items:
            raise ValueError("duplicate id %r" % item["id"])
        self.store.items[item["id"]] = dict(item)

    def update_item(self, item_id, **fields):
        if item_id not in self.store.items:
            raise KeyError(item_id)
        self.store.items[item_id].update(fields)
        self.cache.invalidate(("item", item_id))
        return dict(self.store.items[item_id])

    def delete_item(self, item_id):
        if item_id not in self.store.items:
            raise KeyError(item_id)
        del self.store.items[item_id]

    # -- reads -------------------------------------------------------------
    def get_item(self, item_id):
        key = ("item", item_id)
        hit = self.cache.get(key)
        if hit is not None:
            return hit
        item = dict(self.store.get(item_id))
        self.cache.put(key, item)
        return item

    def list_items(self, category=None, sort="id", desc=False, page=1, page_size=10):
        key = ("list", category, page)
        hit = self.cache.get(key)
        if hit is not None:
            return hit
        rows = [i for i in self.store.scan() if category is None or i["category"] == category]
        rows.sort(key=lambda i: i[sort], reverse=desc)
        result = paginate(rows, page, page_size)
        self.cache.put(key, result)
        return result
