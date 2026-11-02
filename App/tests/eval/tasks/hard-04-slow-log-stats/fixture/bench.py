"""Usage: python3 bench.py [N]   (default 2000)"""
import random
import sys
import time

import logstats

n = int(sys.argv[1]) if len(sys.argv) > 1 else 2000
rng = random.Random(1)
ts = 0
events = []
for _ in range(n):
    ts += rng.randint(0, 3)
    events.append((ts, "u%d" % rng.randint(1, 500), rng.choice(["login", "view", "buy"])))
t = time.time()
kept = logstats.dedupe(events, 60)
print("dedupe: kept %d of %d in %.2fs" % (len(kept), n, time.time() - t))
t = time.time()
r = logstats.rolling_unique(events, 300)
print("rolling_unique: max %d in %.2fs" % (max(r), time.time() - t))
