import json
import os

from .models import Item

PATH = "inventory.json"


def load():
    if not os.path.exists(PATH):
        return []
    with open(PATH, encoding="utf-8") as f:
        return [Item.from_dict(d) for d in json.load(f)]


def save(items):
    with open(PATH, "w", encoding="utf-8") as f:
        json.dump([i.to_dict() for i in items], f, indent=2)


def add(name, qty):
    items = load()
    for it in items:
        if it.name == name:
            it.qty += qty
            break
    else:
        items.append(Item(name, qty))
    save(items)
