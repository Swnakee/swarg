# catalog

`Service` (catalog/service.py) is a read-through cache over an in-memory `Store`.

## Service API

* `Service(items=None, clock=time.monotonic, cache_size=64, ttl=30)`; `items` is an optional list of
  item dicts `{"id": int, "name": str, "category": str, "price": int}`.
* `add_item(item)` - `ValueError` if the id exists.
* `update_item(id, **fields)` - `KeyError` if unknown; returns a copy of the new item.
* `delete_item(id)` - `KeyError` if unknown.
* `get_item(id)` - copy of the item, `KeyError` if unknown.
* `list_items(category=None, sort="id", desc=False, page=1, page_size=10)` -> `Page`.

## Listing

* Filter by category (None = all). Sort by `sort` (`id`, `name` or `price`), `desc` reverses the
  order of the sort key only; ties are ALWAYS broken by id ascending.
* Pages are 1-based. `page < 1` or `page_size < 1` -> `ValueError`.
* `Page.total` = number of matching items, `Page.total_pages` = ceil(total / page_size) (0 when
  there are no items), `Page.has_next` = there is a page after this one. A page past the end is
  valid and has no items.

## Cache (catalog/cache.py)

* `Cache(maxsize, ttl, clock)`: LRU with TTL. An entry is expired once `clock() - stored_at >= ttl`.
  `get` marks the entry as most recently used; when full, the least recently used entry is evicted.
* Cached values must not be shared with callers: modifying a returned item or page must not change
  what the next call returns.
* Every cached list/item must reflect the latest data: after any add/update/delete no stale list or
  item may be served. Different arguments (category, sort, desc, page, page_size) are different
  cache entries.
* A repeated identical call within the TTL must be served from the cache, i.e. without scanning the
  store again (`Store.scans` counts full scans).
