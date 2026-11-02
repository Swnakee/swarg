import csv
import os


def write_prices(out_dir, rows):
    os.makedirs(out_dir, exist_ok=True)
    with open(os.path.join(out_dir, "prices.csv"), "w", newline="") as f:
        w = csv.writer(f)
        w.writerow(["sku", "price_eur_cents"])
        w.writerows(rows)
