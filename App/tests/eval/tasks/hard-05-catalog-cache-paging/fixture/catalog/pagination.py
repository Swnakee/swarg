from dataclasses import dataclass


@dataclass
class Page:
    items: list
    page: int
    page_size: int
    total: int
    total_pages: int
    has_next: bool


def paginate(rows, page, page_size):
    if page < 1 or page_size < 1:
        raise ValueError("page and page_size must be >= 1")
    total = len(rows)
    total_pages = total // page_size
    start = page * page_size
    return Page(rows[start:start + page_size], page, page_size, total, total_pages, page < total_pages)
