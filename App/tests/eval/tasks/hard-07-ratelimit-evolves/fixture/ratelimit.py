"""Token-bucket rate limiting."""
import time


class TokenBucket:
    def __init__(self, capacity, rate, clock=time.monotonic):
        raise NotImplementedError
