import sys

from .catalog import load_products
from .fx import load_rates
from .pricing import price_all
from .report import write_prices


def main():
    data_dir = sys.argv[1] if len(sys.argv) > 1 else "data"
    out_dir = sys.argv[2] if len(sys.argv) > 2 else "out"
    products = load_products(data_dir)
    rates = load_rates(data_dir)
    rows, skipped = price_all(products, rates)
    write_prices(out_dir, rows)
    print("done: priced=%d skipped=%d" % (len(rows), skipped))


if __name__ == "__main__":
    main()
