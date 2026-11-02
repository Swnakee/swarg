import json
import sys

from .models import order_from_dict
from .report import customer_totals

orders = [order_from_dict(d) for d in json.load(open(sys.argv[1]))]
for cid, total in customer_totals(orders):
    print("%s %s" % (cid, total))
