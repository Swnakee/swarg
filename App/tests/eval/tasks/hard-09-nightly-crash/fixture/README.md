# Nightly price job

`python3 -m jobs.nightly [data_dir] [out_dir]` (defaults `data` and `out`) reads
`products.csv` (columns `sku,price,currency,discount_pct`) and `rates.json` from data_dir and
writes `prices.csv` (columns `sku,price_eur_cents`) to out_dir. It prints one line: `done: priced=N skipped=M`.

Rules

* Currency codes are case-insensitive and may have stray spaces in the CSV (` usd `, `Eur`).
* `rates.json` maps a currency code to the number of units of that currency per 1 EUR. EUR itself is
  not in the file; its rate is 1. A rate that is missing or not greater than 0 means "no rate".
* A product whose currency has no rate is SKIPPED: the job logs `WARN skip <sku>: no rate for <CODE>`
  (CODE normalised, upper case) and carries on. Skipped products are counted in `skipped`.
* Price in EUR cents = price * (100 - discount_pct) / 100 / rate * 100, rounded to a whole cent
  HALF UP (0.5 cents goes up). An empty discount_pct means 0.
* Output rows are sorted by sku.
* Log lines go to `logs/nightly.log` in the working directory (append) and have the form
  `LEVEL message`.
