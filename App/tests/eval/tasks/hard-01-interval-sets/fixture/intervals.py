"""Sets of intervals with open/closed ends. See README.md."""
from dataclasses import dataclass

INF = float("inf")


@dataclass(frozen=True)
class Interval:
    lo: float
    hi: float
    lo_closed: bool = True
    hi_closed: bool = True

    def __post_init__(self):
        if self.lo in (INF, -INF):
            object.__setattr__(self, "lo_closed", False)
        if self.hi in (INF, -INF):
            object.__setattr__(self, "hi_closed", False)

    def is_empty(self) -> bool:
        raise NotImplementedError


def parse(text):
    raise NotImplementedError


def format(interval):
    raise NotImplementedError


def merge(intervals):
    raise NotImplementedError


def intersect(a, b):
    raise NotImplementedError


def subtract(a, b):
    raise NotImplementedError
