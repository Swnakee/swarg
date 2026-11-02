# textutil

## slugify(text) -> str
* Unicode is decomposed (NFKD) and everything that is not ASCII afterwards is dropped (`"Café"` -> `"cafe"`).
* Lower-cased. Every run of one or more characters that are not `a-z` or `0-9` becomes ONE `-`.
* Leading and trailing `-` are removed.
* The result is at most 50 characters; it is cut at 50 and then trailing `-` are removed again.
* Empty or symbol-only input gives `""`.

## truncate(text, width, suffix="...") -> str
* If `len(text) <= width` the text is returned unchanged (a text of exactly `width` is not cut).
* Otherwise the result is `text[:width - len(suffix)] + suffix`, so its length is exactly `width`.
* If `width < len(suffix)` the result is the first `width` characters of the suffix.
* `width < 0` raises `ValueError`.

## parse_duration(s) -> int
Seconds from strings like `1h30m`, `2d4h`, `90s`, `1w`.
* Units: `w` = 604800, `d` = 86400, `h` = 3600, `m` = 60, `s` = 1. Case-insensitive (`1H` works).
* Spaces between the parts are allowed (`1h 30m`), leading and trailing spaces too.
* Each part is digits followed by a unit. Units must appear in strictly descending order of size and
  at most once each: `30m1h` and `1h1h` are errors.
* Anything else (empty string, no unit, unknown unit, a sign, decimals) raises `ValueError`.

## chunk(seq, n) -> list
Splits a sequence into lists of `n` items; the last list may be shorter. An empty sequence gives
`[]`. `n < 1` raises `ValueError`. The input is not modified.

## moving_average(values, k) -> list[float]
Averages of every window of `k` consecutive values: the result has `len(values) - k + 1` items.
If `k > len(values)` the result is `[]`. `k < 1` raises `ValueError`.
