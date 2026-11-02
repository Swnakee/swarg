#!/bin/sh
export PYTHONDONTWRITEBYTECODE=1
[ -f tests/test_textutil.py ] || { echo "FAIL: tests/test_textutil.py does not exist"; exit 1; }
cat > /tmp/_mut_run.py <<'PY'
import os, shutil, subprocess, sys, tempfile
W = os.getcwd()
src = open(os.path.join(W, "textutil.py")).read()
GOOD_TEXT = '''"""Small text and list helpers. Behaviour is documented in README.md."""'''
MUT = [
 ("slug 51", 'text[:50].rstrip("-")', 'text[:51].rstrip("-")'),
 ("slug no rstrip after cut", 'text[:50].rstrip("-")', 'text[:50]'),
 ("slug no run collapse", '[^a-z0-9]+', '[^a-z0-9]'),
 ("truncate exact width", 'len(text) <= width', 'len(text) < width'),
 ("truncate narrow width", 'return suffix[:width]', 'return ""'),
 ("truncate width -1", 'if width < 0:', 'if width < -1:'),
 ("duration repeated unit", 'if idx <= prev:', 'if idx < prev:'),
 ("duration week", '("w", 604800)', '("w", 604000)'),
 ("duration case", 's = s.strip().lower()', 's = s.strip()'),
 ("chunk n<-1", 'if n < 1:', 'if n < -1:'),
 ("chunk not lists", 'list(seq[i:i + n])', 'seq[i:i + n]'),
 ("average window", 'len(values) - k + 1', 'len(values) - k'),
 ("average int division", 'sum(values[i:i + k]) / k', 'sum(values[i:i + k]) // k'),
]
def run(module_text, label):
    d = tempfile.mkdtemp()
    open(os.path.join(d, "textutil.py"), "w").write(module_text)
    shutil.copytree(os.path.join(W, "tests"), os.path.join(d, "tests"), ignore=shutil.ignore_patterns("__pycache__"))
    p = subprocess.run([sys.executable, "-m", "unittest", "discover", "-s", "tests", "-p", "test*.py"],
                       cwd=d, env=dict(os.environ, PYTHONPATH=d), capture_output=True, text=True, timeout=60)
    return p.returncode, p.stderr
rc, err = run(src, "good")
if rc != 0: sys.exit("tests fail on the correct module: %s" % err.strip().splitlines()[-1:])
ran = [l for l in err.splitlines() if l.startswith("Ran ")]
if not ran or int(ran[0].split()[1]) < 8: sys.exit("too few tests ran: %s" % ran)
live = []
for name, old, new in MUT:
    assert old in src, "mutant %s not applicable" % name
    rc, _ = run(src.replace(old, new, 1), name)
    if rc == 0: live.append(name)
print("killed %d of %d; survived: %s" % (len(MUT) - len(live), len(MUT), live))
if len(live) > 1: sys.exit("%d mutants survived: %s" % (len(live), ", ".join(live)))
PY
out=$(python3 /tmp/_mut_run.py 2>&1) || { echo "FAIL: $(echo "$out" | tail -n1 | cut -c1-240)"; exit 1; }
echo "PASS: tests pass on the real module and kill the seeded bugs ($(echo "$out" | head -n1))"
