Fix the cause of a crash from a stack trace (the optional setting must get a default).
This traceback came from a user's machine:

Traceback (most recent call last):
  File "<frozen runpy>", line 198, in _run_module_as_main
  File "<frozen runpy>", line 88, in _run_code
  File "/work/app/__main__.py", line 3, in <module>
    run()
  File "/work/app/service.py", line 10, in run
    client = build_client(cfg)
  File "/work/app/service.py", line 6, in build_client
    return Client(cfg["timeout"], cfg["retries"])
KeyError: 'timeout'

The setting timeout is optional and must default to 30 like the other defaults. Fix the cause (not a try/except around the crash) and run python3 -m app to confirm.
