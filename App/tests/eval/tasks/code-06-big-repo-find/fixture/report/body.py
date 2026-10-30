from core.settings import CURRENCY
from util.money import fmt_money
from util.text import pad


def render_body(rows):
    lines = [pad(r["region"], 8) + fmt_money(r["amount"], CURRENCY) for r in rows]
    total = sum(r["amount"] for r in rows)
    lines.append(pad("total", 8) + fmt_money(total, CURRENCY))
    return "\n".join(lines)
