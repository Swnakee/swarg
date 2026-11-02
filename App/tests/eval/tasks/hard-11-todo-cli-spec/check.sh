#!/bin/sh
export PYTHONDONTWRITEBYTECODE=1
[ -f todo.py ] || { echo "FAIL: todo.py does not exist"; exit 1; }
cat > /tmp/_todo_hidden.py <<'PY'
import csv, io, json, os, subprocess, sys, tempfile, unittest
W = os.getcwd()
class T(unittest.TestCase):
    def setUp(self):
        self.d = tempfile.mkdtemp(); self.db = os.path.join(self.d, "t.json")
    def run_(self, *args, today="2024-06-15", db=True, stdin=None):
        env = dict(os.environ, TODO_TODAY=today)
        cmd = [sys.executable, os.path.join(W, "todo.py")] + (["--db", self.db] if db else []) + list(args)
        p = subprocess.run(cmd, capture_output=True, text=True, cwd=self.d, env=env, timeout=30)
        return p.returncode, p.stdout, p.stderr
    def ok(self, *a):
        c, o, e = self.run_(*a); self.assertEqual(c, 0, (a, e)); return o
    def fail(self, code, *a):
        before = open(self.db).read() if os.path.exists(self.db) else None
        c, o, e = self.run_(*a)
        self.assertEqual(c, code, (a, c, e)); self.assertEqual(o, "", a); self.assertTrue(e.strip(), a)
        after = open(self.db).read() if os.path.exists(self.db) else None
        self.assertEqual(before, after, "db changed by failing command %r" % (a,))
        return e
    def test_add_list(self):
        self.assertEqual(self.ok("list"), "no tasks\n")
        self.assertEqual(self.ok("add", "  Pay rent  ", "--prio", "1", "--due", "2024-05-01", "--tag", "Home", "--tag", " money ", "--tag", "home", "--tag", " "), "added #1\n")
        self.assertEqual(self.ok("add", "Buy milk"), "added #2\n")
        self.assertEqual(self.ok("add", "Call mom", "--prio", "3", "--due", "2024-07-01"), "added #3\n")
        self.assertEqual(self.ok("add", "Gym", "--prio", "1", "--due", "2024-06-01", "--tag", "HOME"), "added #4\n")
        self.assertEqual(self.ok("list"),
            "  1 [ ] p1 2024-05-01 Pay rent #home #money\n  2 [ ] p2 ---------- Buy milk\n  3 [ ] p3 2024-07-01 Call mom\n  4 [ ] p1 2024-06-01 Gym #home\n")
        self.assertEqual([l[:3] for l in self.ok("list", "--sort", "prio").splitlines()], ["  1", "  4", "  2", "  3"])
        self.assertEqual([l[:3] for l in self.ok("list", "--sort", "due").splitlines()], ["  1", "  4", "  3", "  2"])
        self.assertEqual([l[:3] for l in self.ok("list", "--tag", "HOME").splitlines()], ["  1", "  4"])
        self.assertEqual(self.ok("list", "--tag", "nope"), "no tasks\n")
    def test_done_rm_ids(self):
        self.ok("add", "a"); self.ok("add", "b")
        self.assertEqual(self.ok("done", "1"), "done #1\n")
        self.assertEqual(self.ok("list"), "  2 [ ] p2 ---------- b\n")
        self.assertEqual(self.ok("list", "--all"), "  1 [x] p2 ---------- a\n  2 [ ] p2 ---------- b\n")
        self.assertIn("already done", self.fail(3, "done", "1"))
        self.assertEqual(self.ok("rm", "2"), "removed #2\n")
        self.assertEqual(self.ok("add", "c"), "added #3\n")
        self.fail(4, "done", "9"); self.fail(4, "rm", "2"); self.fail(2, "done", "x"); self.fail(2, "rm")
        self.assertIn("#9", self.fail(4, "rm", "9"))
    def test_usage_errors(self):
        self.ok("add", "seed")
        self.fail(2, "add", "   "); self.fail(2, "add", ""); self.fail(2, "add", "x", "--prio", "4"); self.fail(2, "add", "x", "--prio", "0")
        self.fail(2, "add", "x", "--due", "2024-02-30"); self.fail(2, "add", "x", "--due", "2024-2-3"); self.fail(2, "add", "x", "--due", "tomorrow")
        self.fail(2, "frobnicate"); self.fail(2); self.fail(2, "list", "--sort", "size"); self.fail(2, "export"); self.fail(2, "export", "--format", "xml")
        self.ok("add", "x", "--due", "2024-02-29")
    def test_stats(self):
        self.assertEqual(self.ok("stats"), "total: 0\nopen: 0\ndone: 0\noverdue: 0\n")
        self.ok("add", "a", "--due", "2024-06-14"); self.ok("add", "b", "--due", "2024-06-15"); self.ok("add", "c", "--due", "2024-01-01"); self.ok("add", "d")
        self.ok("done", "3")
        self.assertEqual(self.ok("stats"), "total: 4\nopen: 3\ndone: 1\noverdue: 1\n")
        c, o, e = self.run_("stats", today="2024-06-16"); self.assertEqual(o, "total: 4\nopen: 3\ndone: 1\noverdue: 2\n")
    def test_export(self):
        self.ok("add", 'He said "hi", ok', "--tag", "a", "--tag", "b", "--due", "2024-03-03", "--prio", "3"); self.ok("add", "plain"); self.ok("done", "2")
        rows = list(csv.reader(io.StringIO(self.ok("export", "--format", "csv"))))
        self.assertEqual(rows, [["id", "text", "prio", "due", "done", "tags"], ["1", 'He said "hi", ok', "3", "2024-03-03", "no", "a;b"], ["2", "plain", "2", "", "yes", ""]])
        js = json.loads(self.ok("export", "--format", "json"))
        self.assertEqual(js, [{"id": 1, "text": 'He said "hi", ok', "prio": 3, "due": "2024-03-03", "done": False, "tags": ["a", "b"]},
                              {"id": 2, "text": "plain", "prio": 2, "due": None, "done": True, "tags": []}])
        out = os.path.join(self.d, "o.json")
        self.assertEqual(self.ok("export", "--format", "json", "-o", out), "")
        self.assertEqual(json.load(open(out)), js)
    def test_corrupt_and_default_db(self):
        open(self.db, "w").write("{not json")
        e = self.fail(5, "list"); self.assertIn(self.db, e)
        self.fail(5, "add", "x")
        open(self.db, "w").write("[1,2]"); self.fail(5, "stats")
        c, o, e = self.run_("add", "in cwd", db=False)
        self.assertEqual((c, o), (0, "added #1\n")); self.assertTrue(os.path.exists(os.path.join(self.d, "todo.json")))
        self.assertIn("in cwd", self.run_("list", db=False)[1])

unittest.main(argv=["x"], verbosity=0)
PY
out=$(python3 /tmp/_todo_hidden.py 2>&1) || { echo "FAIL: cli spec: $(echo "$out" | grep -E 'Error|!=|FAIL:|not' | head -n2 | cut -c1-220 | tr '\n' ' ')"; exit 1; }
echo "PASS: todo.py matches the CLI spec (output formats and exit codes)"
