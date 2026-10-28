#!/usr/bin/env python3
"""Host-side helper of the swarg eval suite (see README.md).

  eval.py prep <task_dir> <dest_dir>       build the fixture copy for one task
  eval.py collect <task> <e2e_out_dir> <jsonl>   parse one finished run, append a JSON line
  eval.py report <run_dir> <model>         results.jsonl -> report.md + results.csv
"""
import csv
import json
import os
import shutil
import sys

# ---------------------------------------------------------------------------
# prep
# ---------------------------------------------------------------------------


def split_task(path):
    """Return (description, [turn texts]) from a task.md."""
    lines = open(path, encoding="utf-8").read().split("\n")
    desc = lines[0].strip()
    turns, cur = [], []
    for ln in lines[1:]:
        if ln.strip() == "---":
            turns.append("\n".join(cur).strip())
            cur = []
        else:
            cur.append(ln)
    turns.append("\n".join(cur).strip())
    return desc, [t for t in turns if t]


def prep(task_dir, dest):
    desc, turns = split_task(os.path.join(task_dir, "task.md"))
    if not turns:
        sys.exit("no user turns in %s/task.md" % task_dir)
    fx = os.path.join(task_dir, "fixture")
    if os.path.isdir(fx):
        shutil.copytree(fx, dest, dirs_exist_ok=True)
    os.makedirs(os.path.join(dest, ".eval"), exist_ok=True)
    here = os.path.dirname(os.path.abspath(__file__))
    shutil.copy(os.path.join(task_dir, "check.sh"), os.path.join(dest, ".eval", "check.sh"))
    shutil.copy(os.path.join(here, "driver.sh"), os.path.join(dest, ".eval", "driver.sh"))
    for i, t in enumerate(turns, 1):
        with open(os.path.join(dest, ".eval", "turn-%02d.txt" % i), "w", encoding="utf-8") as f:
            f.write(t + "\n")
    # docker cp keeps the host owner; the container user (uid 1000) must be able to write
    for root, dirs, files in os.walk(dest):
        for n in dirs:
            os.chmod(os.path.join(root, n), 0o777)
        for n in files:
            os.chmod(os.path.join(root, n), 0o666)
    os.chmod(dest, 0o777)
    print(len(turns))


# ---------------------------------------------------------------------------
# collect
# ---------------------------------------------------------------------------


def read(path):
    try:
        return open(path, encoding="utf-8", errors="replace").read()
    except OSError:
        return ""


def trace_stats(tdir):
    s = dict(trace=False, steps=0, calls=0, failed=0, denied=0, prompt=0, completion=0,
             cached=0, compactions=0, loop_guard=0)
    if not os.path.isdir(tdir):
        return s
    for name in sorted(os.listdir(tdir)):
        for ln in read(os.path.join(tdir, name)).splitlines():
            try:
                r = json.loads(ln)
            except ValueError:
                continue
            k, d = r.get("kind"), r.get("d") or {}
            if not isinstance(d, dict):
                d = {}
            s["trace"] = True
            if k == "request":
                s["steps"] += 1
            elif k == "tool_start":
                s["calls"] += 1
            elif k == "tool_end" and d.get("error"):
                s["failed"] += 1
            elif k == "approval" and d.get("allow") is False:
                s["denied"] += 1
            elif k == "usage":
                s["prompt"] += int(d.get("prompt") or 0)
                s["completion"] += int(d.get("completion") or 0)
                s["cached"] += int(d.get("cached") or 0)
            elif k == "compact":
                s["compactions"] += 1
            elif k == "loop_guard":
                s["loop_guard"] += 1
    return s


def collect(task, out, jsonl):
    ev = os.path.join(out, "work", ".eval")
    res = read(os.path.join(ev, "result.txt")).strip()
    code = read(os.path.join(ev, "check_exit.txt")).strip()
    status = "PASS" if code == "0" and res.startswith("PASS") else ("FAIL" if code else "ERROR")
    reason = res.replace("\n", " | ")[:300] if res else "no result.txt (run did not finish or timed out)"
    t0 = t1 = 0
    parts = read(os.path.join(ev, "time.txt")).split()
    if len(parts) == 2:
        t0, t1 = int(parts[0]), int(parts[1])
    turns_txt = read(os.path.join(ev, "turns.txt")).strip().splitlines()
    row = dict(task=task, category=task.split("-")[0], status=status, reason=reason,
               turns=len(turns_txt), wall_s=t1 - t0, bad_exit=sum(1 for t in turns_txt if not t.endswith("exit=0")))
    row.update(trace_stats(os.path.join(ev, "traces")))
    with open(jsonl, "a", encoding="utf-8") as f:
        f.write(json.dumps(row, ensure_ascii=False) + "\n")
    print("%s %s" % (status, reason))


# ---------------------------------------------------------------------------
# report
# ---------------------------------------------------------------------------

COLS = ["task", "status", "turns", "steps", "calls", "failed", "denied", "prompt", "completion",
        "cache_pct", "compactions", "loop_guard", "wall_s", "trace", "reason"]


def cache_pct(r):
    return round(100.0 * r["cached"] / r["prompt"]) if r["prompt"] else 0


def report(run_dir, model):
    rows = [json.loads(ln) for ln in read(os.path.join(run_dir, "results.jsonl")).splitlines() if ln.strip()]
    for r in rows:
        r["cache_pct"] = cache_pct(r)
    with open(os.path.join(run_dir, "results.csv"), "w", newline="", encoding="utf-8") as f:
        w = csv.writer(f)
        w.writerow(COLS)
        for r in rows:
            w.writerow([r[c] for c in COLS])

    def line(r):
        return "| %s | %s | %d | %d | %d | %d/%d | %dk/%dk | %d%% | %d | %d | %ds |" % (
            r["task"], r["status"], r["turns"], r["steps"], r["calls"], r["failed"], r["denied"],
            r["prompt"] // 1000, r["completion"] // 1000, r["cache_pct"], r["compactions"],
            r["loop_guard"], r["wall_s"])

    head = ("| task | result | turns | steps | calls | failed/denied | tokens in/out | cache | compact | loop | time |\n"
            "|---|---|---|---|---|---|---|---|---|---|---|")
    out = ["# Eval report: %s" % model, "", "Run: `%s`" % os.path.basename(run_dir.rstrip("/")), ""]
    cats = []
    for r in rows:
        if r["category"] not in cats:
            cats.append(r["category"])
    out += ["## Summary", "", "| category | passed | total |", "|---|---|---|"]
    for c in cats:
        cr = [r for r in rows if r["category"] == c]
        out.append("| %s | %d | %d |" % (c, sum(r["status"] == "PASS" for r in cr), len(cr)))
    out.append("| **all** | **%d** | **%d** |" % (sum(r["status"] == "PASS" for r in rows), len(rows)))
    out += ["", "Totals: %d steps, %d tool calls (%d failed, %d denied), %dk prompt / %dk completion tokens, "
            "%d compactions, %d loop-guard hits, %d s wall." % (
                sum(r["steps"] for r in rows), sum(r["calls"] for r in rows), sum(r["failed"] for r in rows),
                sum(r["denied"] for r in rows), sum(r["prompt"] for r in rows) // 1000,
                sum(r["completion"] for r in rows) // 1000, sum(r["compactions"] for r in rows),
                sum(r["loop_guard"] for r in rows), sum(r["wall_s"] for r in rows))]
    if any(not r["trace"] for r in rows):
        out += ["", "Note: some runs have no trace (this swarg build predates sessions/trace): "
                "steps, tokens and counters are 0 for them."]
    for c in cats:
        out += ["", "## %s" % c, "", head]
        out += [line(r) for r in rows if r["category"] == c]
    bad = [r for r in rows if r["status"] != "PASS"]
    if bad:
        out += ["", "## Failures", ""]
        out += ["- `%s` %s: %s" % (r["task"], r["status"], r["reason"]) for r in bad]
    out += ["", "Per-task artifacts (output.log, work/, diff.patch, work/.eval/trace.txt): `runs/<task>/`.", ""]
    with open(os.path.join(run_dir, "report.md"), "w", encoding="utf-8") as f:
        f.write("\n".join(out))


def main(a):
    if len(a) == 3 and a[0] == "prep":
        prep(a[1], a[2])
    elif len(a) == 4 and a[0] == "collect":
        collect(a[1], a[2], a[3])
    elif len(a) == 3 and a[0] == "report":
        report(a[1], a[2])
    else:
        sys.exit(__doc__)


if __name__ == "__main__":
    main(sys.argv[1:])
