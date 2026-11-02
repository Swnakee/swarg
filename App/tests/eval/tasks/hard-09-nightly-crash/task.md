Debug a crashing nightly price job from a traceback and last night's log; the root cause is not where it crashes.
Our nightly price job died last night. Here is the end of the traceback:

Traceback (most recent call last):
  File "/work/jobs/nightly.py", line 20, in <module>
    main()
  File "/work/jobs/nightly.py", line 14, in main
    rows, skipped = price_all(products, rates)
  File "/work/jobs/pricing.py", line 19, in price_all
    cents = price_one(p, rates)
  File "/work/jobs/pricing.py", line 12, in price_one
    return to_cents(net / rate)
decimal.DivisionByZero: [<class 'decimal.DivisionByZero'>]

The log of that run is in logs/nightly.log and README.md says how the job is meant to behave. Please make `python3 -m jobs.nightly` work correctly on the data in data/ and in general (not only for this one SKU), and tell me what the real causes were.
