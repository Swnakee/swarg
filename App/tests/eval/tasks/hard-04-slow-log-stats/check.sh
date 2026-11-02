#!/bin/sh
export PYTHONDONTWRITEBYTECODE=1
cat > /tmp/_perf_hidden.py <<'PY'
import random, sys, time
sys.path.insert(0, ".")
from logstats import dedupe, rolling_unique

def ref_dedupe(events, window):
    last = {}
    out = []
    for ev in events:
        k = (ev[1], ev[2])
        if k in last and ev[0] - last[k] < window:
            continue
        last[k] = ev[0]
        out.append(ev)
    return out

def ref_rolling(events, window):
    from collections import Counter, deque
    c = Counter(); dq = deque(); out = []
    for ev in events:
        dq.append(ev); c[ev[1]] += 1
        while dq[0][0] <= ev[0] - window:
            o = dq.popleft(); c[o[1]] -= 1
            if not c[o[1]]: del c[o[1]]
        out.append(len(c))
    return out

def brute_rolling(events, window):
    return [len({events[j][1] for j in range(i + 1) if events[j][0] > events[i][0] - window}) for i in range(len(events))]

def gen(n, users, seed, step=3):
    rng = random.Random(seed); ts = 0; ev = []
    for _ in range(n):
        ts += rng.randint(0, step)
        ev.append((ts, "u%d" % rng.randint(1, users), rng.choice(["a", "b", "c"])))
    return ev

for seed in range(40):
    ev = gen(60, 6, seed)
    for w in (0, 1, 2, 7, 50, 1000):
        assert dedupe(ev, w) == ref_dedupe(ev, w), ("dedupe small", seed, w)
        if w:
            assert rolling_unique(ev, w) == brute_rolling(ev, w), ("rolling small", seed, w)
assert dedupe([], 5) == [] and rolling_unique([], 5) == []
ev = gen(300000, 5000, 3)
t = time.time(); got = dedupe(ev, 60); d1 = time.time() - t
assert got == ref_dedupe(ev, 60), "dedupe big result differs"
t = time.time(); got = rolling_unique(ev, 300); d2 = time.time() - t
assert got == ref_rolling(ev, 300), "rolling big result differs"
ev2 = gen(300000, 40, 4, step=0)  # many equal timestamps
t = time.time(); dedupe(ev2, 5); d3 = time.time() - t
t = time.time(); rolling_unique(ev2[:100000], 5); d4 = time.time() - t
print("times %.1f %.1f %.1f %.1f" % (d1, d2, d3, d4))
assert max(d1, d2, d3, d4) < 20, "too slow: %.1f %.1f %.1f %.1f" % (d1, d2, d3, d4)
PY
cat > /tmp/_perf_driver.py <<'PY'
import subprocess, sys
try:
    p = subprocess.run([sys.executable, "/tmp/_perf_hidden.py"], capture_output=True, text=True, timeout=60)
except subprocess.TimeoutExpired:
    print("TIMEOUT"); sys.exit(2)
print(p.stdout + p.stderr); sys.exit(p.returncode)
PY
out=$(python3 /tmp/_perf_driver.py 2>&1) || { echo "FAIL: $(echo "$out" | tail -n2 | tr '\n' ' ' | cut -c1-200)"; exit 1; }
out=$(python3 tests/test_logstats.py 2>&1) || { echo "FAIL: tests/test_logstats.py fails"; exit 1; }
echo "PASS: results unchanged and 300000 events handled fast"
