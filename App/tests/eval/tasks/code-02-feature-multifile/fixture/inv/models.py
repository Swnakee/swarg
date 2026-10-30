from dataclasses import dataclass


@dataclass
class Item:
    name: str
    qty: int

    def to_dict(self):
        return {"name": self.name, "qty": self.qty}

    @staticmethod
    def from_dict(d):
        return Item(d["name"], int(d["qty"]))
