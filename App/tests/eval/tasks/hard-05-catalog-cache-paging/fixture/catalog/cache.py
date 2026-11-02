from collections import OrderedDict
import time


class Cache:
    def __init__(self, maxsize, ttl, clock=time.monotonic):
        self.maxsize = maxsize
        self.ttl = ttl
        self.clock = clock
        self.data = OrderedDict()

    def get(self, key):
        """Return the value or None."""
        entry = self.data.get(key)
        if entry is None:
            return None
        value, stored_at = entry
        if self.clock() - stored_at > self.ttl:
            del self.data[key]
            return None
        return value

    def put(self, key, value):
        self.data[key] = (value, self.clock())
        self.data.move_to_end(key)
        while len(self.data) > self.maxsize:
            self.data.popitem(last=False)

    def invalidate(self, key):
        self.data.pop(key, None)

    def clear(self):
        self.data.clear()
