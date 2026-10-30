Refactor: move a helper duplicated in three scripts into a shared module.
The scripts tools/a.py, tools/b.py and tools/c.py each contain their own copy of parse_kv. Move it into tools/common.py and make all three import it from there, so the function is defined exactly once. The output of each script (python3 tools/a.py etc.) must not change.
