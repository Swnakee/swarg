# intervals

A tiny library for sets of real-number intervals with open or closed ends.

## Interval

`Interval(lo, hi, lo_closed=True, hi_closed=True)` is a frozen dataclass (already defined in
`intervals.py`). Bounds are int or float. An infinite bound (`float("inf")`, `-float("inf")`) is
ALWAYS open: `Interval(0, inf)` must behave exactly like `Interval(0, inf, True, False)`; this is
done in `__post_init__` (already written, do not change it).

An interval is EMPTY if `lo > hi`, or if `lo == hi` and at least one end is open.
`[3,3]` is a valid single point; `[3,3)` and `(3,3)` are empty.

## parse(text) -> Interval

Text looks like `[1,5)`, `(-inf, 3]`, `(2,+inf)`, `[0.5, 2]`. Spaces around the numbers are
allowed. `[` and `]` mean closed, `(` and `)` mean open. Numbers without a dot (and not inf) become
`int`, others `float`. `inf`, `+inf`, `-inf` are accepted. Anything malformed (missing bracket,
three numbers, not a number, `lo` text missing) raises `ValueError`. An empty interval such as `(3,3)`
or `[5,1]` parses fine (it is not an error).

## format(interval) -> str

The inverse of parse, with no spaces: `[1,5)`, `(-inf,3]`, `(2,inf)`. Numbers are written with
`str()`, infinities as `inf` / `-inf`. An empty interval is written `empty`.

## merge(intervals) -> list[Interval]

Takes any iterable of intervals (any order, may overlap, may contain empty ones) and returns the
minimal sorted list of non-empty, pairwise disjoint intervals covering the same points.
Empty ones are dropped. Two intervals that TOUCH at one point are joined only if the point belongs
to at least one of them:

* `[1,2)` + `[2,3]` -> `[1,3]` (2 belongs to the second)
* `[1,2]` + `(2,3]` -> `[1,3]` (2 belongs to the first)
* `[1,2)` + `(2,3]` -> stays two intervals (2 belongs to neither)

The result is sorted by lower bound. Do not modify the input.

## intersect(a, b) -> list[Interval]

`a` and `b` are iterables of intervals (each may be unsorted and overlapping). Returns the merged
list of points that are in some interval of `a` AND in some interval of `b`.
`[1,5)` and `[5,9]` do not intersect; `[1,5]` and `[5,9]` intersect in `[5,5]`.

## subtract(a, b) -> list[Interval]

Points that are in `a` but not in `b`, as a merged list. Removing a closed end leaves an open end
and the other way around: `[0,10]` minus `(3,5)` is `[0,3]`, `[5,10]`; `[0,10]` minus `[3,5]` is
`[0,3)`, `(5,10]`. Removing exactly one point from the middle splits the interval in two.
Removing from an unbounded interval works too: `(-inf,inf)` minus `[0,0]` gives
`(-inf,0)`, `(0,inf)`.
