#!/bin/sh
export PYTHONDONTWRITEBYTECODE=1
cat > /tmp/_rl_hidden.py <<'PY'
import json, sys, unittest
sys.path.insert(0, ".")
from ratelimit import TokenBucket, Limiter

class Clock:
    def __init__(self, t=0.0): self.t = float(t)
    def __call__(self): return self.t

class T(unittest.TestCase):
    def test_bucket_basic(self):
        c = Clock(100); b = TokenBucket(5, 1, c)
        self.assertEqual(b.tokens, 5)
        self.assertEqual([b.allow() for _ in range(6)], [True] * 5 + [False])
        c.t += 0.5; self.assertFalse(b.allow()); self.assertEqual(b.tokens, 0.5)
        c.t += 0.5; self.assertTrue(b.allow())
        c.t += 1000; self.assertEqual(b.tokens, 5)
        self.assertTrue(b.allow(5)); self.assertFalse(b.allow(0.5 + 0.5))
        c.t += 2; self.assertTrue(b.allow(2)); self.assertFalse(b.allow(1))
    def test_bucket_fail_takes_nothing(self):
        c = Clock(); b = TokenBucket(4, 2, c)
        self.assertTrue(b.allow(3)); self.assertFalse(b.allow(2)); self.assertEqual(b.tokens, 1)
    def test_cost_over_capacity(self):
        c = Clock(); b = TokenBucket(4, 2, c)
        self.assertFalse(b.allow(5)); self.assertEqual(b.tokens, 4)
        self.assertTrue(b.allow(4))
        c.t += 100; self.assertFalse(b.allow(4.5)); self.assertEqual(b.tokens, 4)
    def test_clock_backwards_bucket(self):
        c = Clock(10); b = TokenBucket(5, 1, c)
        self.assertTrue(b.allow(5))
        c.t = 5; self.assertFalse(b.allow()); self.assertEqual(b.tokens, 0)
        c.t = 7; self.assertEqual(b.tokens, 2); self.assertTrue(b.allow(2)); self.assertFalse(b.allow())
        c.t = 3; self.assertEqual(b.tokens, 0)
        c.t = 4; self.assertEqual(b.tokens, 1)
    def test_limiter(self):
        c = Clock(); L = Limiter(3, 1, c)
        self.assertEqual(len(L), 0)
        self.assertTrue(L.allow("a")); self.assertTrue(L.allow("a", 2)); self.assertFalse(L.allow("a"))
        self.assertTrue(L.allow("b")); self.assertEqual(len(L), 2)
        self.assertEqual(L.retry_after("a"), 1.0); self.assertEqual(L.retry_after("a", 3), 3.0)
        self.assertEqual(L.retry_after("a", 4), float("inf")); self.assertFalse(L.allow("a", 4))
        self.assertEqual(L.retry_after("zz"), 0); self.assertEqual(len(L), 2)  # no side effects
        self.assertEqual(L.retry_after("b", 3), 1.0)
        c.t = 1; self.assertEqual(L.retry_after("a"), 0); self.assertEqual(L.retry_after("a", 3), 2.0)
        self.assertEqual(len(L), 1)  # b refilled to full after 1 s
        c.t = 3; self.assertEqual(len(L), 0)
        self.assertTrue(L.allow("a", 3)); self.assertFalse(L.allow("a"))
        self.assertEqual(L.retry_after("a", 2), 2.0)
    def test_limiter_cost_over_capacity_no_state(self):
        c = Clock(); L = Limiter(3, 1, c)
        self.assertFalse(L.allow("k", 10)); self.assertTrue(L.allow("k", 3))
        self.assertEqual(L.retry_after("q", 10), float("inf"))
    def test_zero_rate(self):
        c = Clock(); L = Limiter(2, 0, c)
        self.assertTrue(L.allow("a", 2)); self.assertEqual(L.retry_after("a"), float("inf"))
        c.t = 1000; self.assertFalse(L.allow("a")); self.assertEqual(len(L), 1)
    def test_limiter_clock_backwards(self):
        c = Clock(100); L = Limiter(4, 1, c)
        self.assertTrue(L.allow("a", 4))
        c.t = 50; self.assertFalse(L.allow("a")); self.assertEqual(L.retry_after("a"), 1.0)
        c.t = 52; self.assertTrue(L.allow("a", 2)); self.assertFalse(L.allow("a"))
        c.t = 1; self.assertEqual(len(L), 1); c.t = 100; self.assertEqual(len(L), 0)
    def test_dump_load(self):
        c = Clock(1000); L = Limiter(5, 2, c)
        L.allow("a", 5); L.allow("b", 2); L.allow("c", 1); c.t += 0.5
        s = json.loads(json.dumps(L.dump()))
        c2 = Clock(7); M = Limiter.load(s, c2)
        self.assertEqual(len(M), len(L))
        for k in "abc":
            self.assertEqual(M.retry_after(k, 5), L.retry_after(k, 5), k)
        self.assertEqual(M.retry_after("a"), 0.0)  # a has 1 token (0.5s * 2)
        c2.t += 100
        self.assertEqual(len(M), 0)
        # identical behaviour step by step
        c3 = Clock(5000); N = Limiter.load(s, c3)
        seq = [("a", 1, 0.25), ("a", 1, 0.0), ("b", 4, 0.5), ("c", 5, 0.5), ("d", 6, 1), ("d", 5, 1)]
        for k, cost, dt in seq:
            self.assertEqual(N.allow(k, cost), L.allow(k, cost))
            c3.t += dt; c.t += dt
        self.assertEqual(N.dump(), L.dump())
    def test_dump_empty_and_roundtrip(self):
        c = Clock(); L = Limiter(2, 1, c)
        s = L.dump(); json.dumps(s)
        M = Limiter.load(s, Clock(3)); self.assertEqual(len(M), 0)
        self.assertTrue(M.allow("x", 2)); self.assertFalse(M.allow("x"))

unittest.main(argv=["x"], verbosity=0)
PY
out=$(python3 /tmp/_rl_hidden.py 2>&1) || { echo "FAIL: ratelimiter: $(echo "$out" | grep -E 'Error|!=|FAIL:' | head -n2 | cut -c1-200 | tr '\n' ' ')"; exit 1; }
out=$(python3 tests/test_ratelimit.py 2>&1) || { echo "FAIL: tests/test_ratelimit.py fails"; exit 1; }
n=$(grep -c "def test" tests/test_ratelimit.py)
[ "$n" -ge 5 ] || { echo "FAIL: only $n tests written in tests/test_ratelimit.py"; exit 1; }
echo "PASS: TokenBucket and Limiter satisfy all three turns"
