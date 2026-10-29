"""T4 cross-model scenario matrix: T3+T4 features end to end on real models.

Usage (from App/tests/live): uv run --with pyte python xmodel.py OUTDIR MODEL SCENARIO [SCENARIO ...]
  MODEL     an mlx model id (mlx-community/...)
  SCENARIO  ids from SCENARIOS below, or "all" (model-dependent ones) / "ui" (model-independent)
Every scenario writes its evidence to OUTDIR/<scenario>/ and one JSON line to OUTDIR/results.jsonl:
{"id", "model", "status": PASS|FAIL|PARTIAL, "why", "s"}. Sandbox runs are sequential (the runner
holds the model-server lock); the web scenario runs on the host in a temp HOME (no internet in
the sandbox).
"""

from __future__ import annotations

import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
import time
from pathlib import Path

import final
from final import finish, keep, plain, results_dir, traces
from tui import ENTER, ESC, Term, ctrl

HERE = final.HERE
SANDBOX = final.SANDBOX
APP = HERE.parent.parent

# ---------------------------------------------------------------------------
# Fixtures and trace helpers
# ---------------------------------------------------------------------------


SPIN = "◐◓◑◒"


def idle(s: str) -> bool:
    """No work running: no progress line (a spinner glyph starts it; a long Task line is cut
    before its "esc to stop"), no stopping notice."""
    if any(w in s for w in ("Thinking", "Stopping", "esc to stop", "Running ", "reading prompt",
                            "finishing the current answer")):
        return False
    return not any(l and l[0] in SPIN for l in s.splitlines())


def fixture(name: str, files: dict[str, str]) -> str:
    """A temporary sandbox fixture _xm-<name> with the given files; returns its name."""
    fx = SANDBOX / "fixtures" / f"_xm-{name}"
    shutil.rmtree(fx, ignore_errors=True)
    fx.mkdir(parents=True)
    for rel, body in files.items():
        (fx / rel).write_text(body)
    return fx.name


def drop(name: str) -> None:
    shutil.rmtree(SANDBOX / "fixtures" / name, ignore_errors=True)


def work(dst: Path) -> Path | None:
    """The resulting /work of the scenario's run (plain or live)."""
    for f in ("output.txt", "raw.log"):
        p = dst / f
        if p.exists():
            run = results_dir(p.read_bytes().decode("utf-8", "replace"))
            if run:
                return run / "work"
    return None


def wfile(dst: Path, rel: str) -> str | None:
    w = work(dst)
    p = w / rel if w else None
    return p.read_text() if p and p.exists() else None


def kinds(recs: list[dict], kind: str) -> list[dict]:
    return [r.get("d") or {} for r in recs if r.get("kind") == kind]


def answer(recs: list[dict]) -> str:
    """All streamed assistant text of the Session (not of Tasks)."""
    return "".join(d.get("text") or "" for r in recs if r.get("kind") == "delta" and not r.get("task")
                   for d in [r.get("d") or {}])


def calls(recs: list[dict]) -> list[tuple[str, str]]:
    return [(d.get("name", ""), d.get("args", "")) for d in kinds(recs, "tool_start")]


def warnings(recs: list[dict]) -> list[str]:
    return [str(d.get("text", "")) for d in kinds(recs, "warning")]


def cyr(s: str) -> float:
    letters = [c for c in s if c.isalpha()]
    return sum("а" <= c.lower() <= "я" or c in "ёЁ" for c in letters) / len(letters) if letters else 0.0


def missing_arg(recs: list[dict]) -> str:
    """'' or a note on missing-argument errors and whether the next call of that tool recovered."""
    out = []
    starts = kinds(recs, "tool_start")
    ends = kinds(recs, "tool_end")
    for i, e in enumerate(ends):
        if re.search(r"missing (required )?argument", json.dumps(e)):
            nxt = next((s for s in starts[i + 1:] if s.get("name") == starts[i].get("name")), None)
            ok = nxt is not None and not re.search(r"missing (required )?argument", json.dumps(ends[starts.index(nxt)] if starts.index(nxt) < len(ends) else {}))
            out.append(f"{starts[i].get('name')}: {'recovered' if ok else 'not recovered'}")
    return "; ".join(out)


def res(status: str, why: str, recs: list[dict] | None = None) -> tuple[str, str]:
    if recs is not None:
        m = missing_arg(recs)
        if m:
            why += f" [missing-arg: {m}]"
    return status, why


# ---------------------------------------------------------------------------
# Model-dependent scenarios
# ---------------------------------------------------------------------------


def x_chat_ru(dst: Path) -> tuple[str, str]:
    plain("printf '%s\\n' 'Привет! Кто ты и что умеешь? Ответь в двух предложениях.' | swarg --yes", dst)
    r = traces(dst)
    a = answer(r)
    share = cyr(a)
    return res("PASS" if a and share > 0.6 else "FAIL", f"answer {len(a)} chars, Cyrillic {share:.0%}: {a[:80]!r}", r)


def x_chat_en(dst: Path) -> tuple[str, str]:
    plain("printf '%s\\n' 'Hi! In two sentences: who are you and what can you do?' | swarg --yes", dst)
    r = traces(dst)
    a = answer(r)
    ascii_share = sum(c.isascii() for c in a) / len(a) if a else 0
    return res("PASS" if a and ascii_share > 0.9 else "FAIL", f"answer {len(a)} chars, ASCII {ascii_share:.0%}: {a[:80]!r}", r)


def x_files(dst: Path) -> tuple[str, str]:
    fx = fixture("files", {"README.md": "scratch project\n"})
    plain("printf '%s\\n' 'Create notes.txt with three lines: alpha, beta, gamma. Then change the line beta to BETA. "
          "Then append a fourth line delta. Use the file tools.' | swarg --yes", dst, fixture=fx)
    drop(fx)
    r = traces(dst)
    got = wfile(dst, "notes.txt")
    ok = got is not None and got.split() == ["alpha", "BETA", "gamma", "delta"]
    names = sorted({n for n, _ in calls(r)})
    return res("PASS" if ok else "FAIL", f"notes.txt={got!r}; tools {names}", r)


def x_background(dst: Path) -> tuple[str, str]:
    fx = fixture("bg", {"slow.sh": "sleep 15\necho RESULT=4242\n"})  # longer than the 10 s grace
    plain("printf '%s\\n' 'Start `sh slow.sh` as a background job, wait until it has finished, read its output "
          "and tell me the number after RESULT=.' | swarg --yes", dst, fixture=fx)
    drop(fx)
    r = traces(dst)
    cs = calls(r)
    bg = any(n == "shell" and '"background": true' in a.replace("\":true", "\": true") for n, a in cs)
    polled = any(n == "shell_output" for n, _ in cs)
    a = answer(r)
    ok = "4242" in a
    return res("PASS" if ok and bg and polled else ("PARTIAL" if ok else "FAIL"),
               f"answer has 4242={ok}; background job={bg}; shell_output={polled}; {len(cs)} calls", r)


LONGQ = "Explain Go channels in ten numbered points, each with a short code example."


def x_waiting(dst: Path) -> tuple[str, str]:
    t = final.live("swarg", dst)
    t.wait("auto", 120)
    t.type(LONGQ)
    t.send(ENTER)
    t.wait(lambda s: "esc to stop" in s or "Thinking" in s, 120)
    t.type("Second question: reply with the single word KIWI.")
    t.send(ENTER, pause=0.5)
    s = t.snap(dst / "1-waiting.txt")
    shown = "(waiting)" in s and "KIWI" in s
    t.wait(lambda x: idle(x) and "(waiting)" not in x, 600)
    t.snap(dst / "2-done.txt")
    finish(t, dst)
    r = traces(dst)
    a = answer(r)
    return res("PASS" if shown and "KIWI" in a.upper() else "FAIL",
               f"waiting shown at once={shown}; KIWI answered={'KIWI' in a.upper()}", r)


COUNT = {"data.txt": "".join(f"row {i}\n" for i in range(1, 14))}


def x_task1(dst: Path) -> tuple[str, str]:
    fx = fixture("task1", COUNT)
    t = final.live("swarg", dst, fixture=fx)
    t.wait("auto", 120)
    t.type("/task count the lines in data.txt and write only the number into n.txt")
    t.send(ENTER)
    t.wait(lambda s: "esc to stop" in s or "Thinking" in s, 60)
    t.wait(idle, 600)
    s = t.snap(dst / "1-done.txt")
    finish(t, dst)
    drop(fx)
    r = traces(dst)
    ts = kinds(r, "task_start")
    te = kinds(r, "task_end")
    n = (wfile(dst, "n.txt") or "").strip()
    ok = n == "13" and ts and ts[0].get("depth") == 1
    return res("PASS" if ok else "FAIL",
               f"n.txt={n!r}; tasks={[(d.get('depth'), d.get('goal', '')[:30]) for d in ts]}; "
               f"ends={[d.get('status', '')[:30] for d in te]}", r)


NESTED = ("Use the task tool for this job, and inside that task use the task tool again: the inner "
          "task creates a.txt containing A and b.txt containing B. Then tell me what the tasks reported.")


def x_nested(dst: Path) -> tuple[str, str]:
    fx = fixture("nested", {"README.md": "scratch\n"})
    plain(f"printf '%s\\n' '{NESTED}' | swarg --yes", dst, fixture=fx)
    drop(fx)
    r = traces(dst)
    depths = [d.get("depth", 0) for d in kinds(r, "task_start")]
    a, b = (wfile(dst, "a.txt") or "").strip(), (wfile(dst, "b.txt") or "").strip()
    mx = max(depths, default=0)
    ok = mx >= 2 and a == "A" and b == "B"
    st = "PASS" if ok else ("PARTIAL" if mx >= 1 and a == "A" and b == "B" else "FAIL")
    return res(st, f"task depths={depths}; a.txt={a!r} b.txt={b!r}", r)


def x_esc(dst: Path) -> tuple[str, str]:
    fx = fixture("esc", {"README.md": "scratch\n", **COUNT})
    t = final.live("swarg", dst, fixture=fx)
    t.wait("auto", 120)
    t.type("/task read data.txt, then for each of its lines append that line in upper case to up.txt, one call per line")
    t.send(ENTER)
    started = t.wait(lambda s: "task" in s.lower() and ("esc to stop" in s or "Thinking" in s), 120)
    time.sleep(4)
    t.snap(dst / "1-running.txt")
    t.send(ESC, pause=0.6)
    t.wait(idle, 300)
    s = t.snap(dst / "2-after-esc.txt")
    t.type("say OK")
    t.send(ENTER)
    alive = t.wait(lambda x: idle(x) and "say OK" in x, 300)
    t.snap(dst / "3-alive.txt")
    finish(t, dst)
    drop(fx)
    r = traces(dst)
    te = [d.get("status", "") for d in kinds(r, "task_end")]
    interrupted = any("interrupt" in x for x in te) or "interrupted" in s
    return res("PASS" if started and interrupted and alive else "FAIL",
               f"task started={started}; ended={te}; interrupted shown={interrupted}; answers after={alive}", r)


def batched(r: list[dict], n: int) -> bool:
    """All n calls came in one Step (a batch, one decision: F12) and the model then answered."""
    steps = [len(d.get("calls") or []) for d in kinds(r, "delta")]
    return n > 0 and max(steps, default=0) >= n and sum(1 for c in steps if c) == 1 and bool(answer(r).strip())


def x_repeat(dst: Path) -> tuple[str, str]:
    plain("printf '%s\\n' 'Run the command `cat /etc/hostname` exactly 5 times, one tool call per run, "
          "then tell me what it printed.' | swarg --yes", dst)
    r = traces(dst)
    n = sum(1 for nm, a in calls(r) if nm == "shell" and "hostname" in a)
    w = [x for x in warnings(r) if "same tool call" in x]
    if n >= 3 and batched(r, n):
        return res("PASS", f"{n} identical calls in one Step (a batch counts once, F12) and the answer followed; warnings={w}", r)
    if n < 3:
        return res("NT", f"guard not triggered: the model made {n} identical call(s); warnings={w}", r)
    return res("PASS" if n == 3 and w else "FAIL", f"{n} identical calls; guard warnings={w}", r)


def x_cycle(dst: Path) -> tuple[str, str]:
    plain("printf '%s\\n' 'Alternate two commands, one tool call at a time: `echo ping`, then `echo pong`, "
          "and repeat that pair 5 times.' | swarg --yes", dst)
    r = traces(dst)
    n = sum(1 for nm, a in calls(r) if nm == "shell" and ("ping" in a or "pong" in a))
    w = [x for x in warnings(r) if "kept repeating" in x or "same tool call" in x]
    if n >= 4 and batched(r, n):
        return res("PASS", f"{n} ping/pong calls in one Step (a batch counts once, F12) and the answer followed; warnings={w}", r)
    if n < 4:
        return res("NT", f"guard not triggered: {n} ping/pong call(s); warnings={w}", r)
    return res("PASS" if n <= 6 and (n < 6 or w) else "FAIL", f"{n} ping/pong calls; guard warnings={w}", r)


def x_loop(dst: Path) -> tuple[str, str]:
    plain("printf '%s\\n' 'Write the phrase \"go build, go test, \" repeated 200 times on one single line. "
          "Output nothing else.' | swarg --yes", dst)
    r = traces(dst)
    cut = any("repeating itself" in x for x in warnings(r)) or bool(kinds(r, "loop_guard"))
    a = answer(r)
    fin = [d.get("finish") for d in kinds(r, "delta")]
    if cut:
        return res("PASS", f"loop guard cut the line; answer {len(a)} chars", r)
    if "length" in fin and not a.strip():
        return res("FAIL", "the model looped inside a tool call: the server buffered it to max_tokens and sent "
                   "nothing, so the loop guard saw no text (F11); the user only sees 'answer cut off'", r)
    return res("PASS" if len(a) < 600 else "FAIL", f"no loop: the model wrote a short answer ({len(a)} chars)", r)


BIG = "\n".join(f"{i:04d} the quick brown fox jumps over the lazy dog near the river bank, then it runs back across the old stone bridge to the farm {i * 7}" for i in range(1, 200))  # ~6k tokens a file
WORDS = ["APPLE", "BIRCH", "CEDAR", "DAISY", "EMBER", "FROST", "GROVE"]


def x_compact(dst: Path) -> tuple[str, str]:
    """One Turn reads seven 200-line files (~4.5k tokens each, ~31k in all) against the 32k
    limit: the context economy must keep every request under the limit, and the fact of the
    first file must survive. Stub batches / compactions are recorded."""
    fx = fixture("compact", {f"big{i}.txt": f"CODEWORD-{i}: {w}\n{BIG}\n" for i, w in enumerate(WORDS, 1)})
    plain("printf '%s\\n' 'Read big1.txt, big2.txt, big3.txt, big4.txt, big5.txt, big6.txt and big7.txt with "
          "read_file, one file per call, all lines. Then tell me the word after CODEWORD-1: in big1.txt.' "
          "| swarg --yes", dst, fixture=fx)
    drop(fx)
    r = traces(dst)
    comp, stubs = kinds(r, "compact"), kinds(r, "stub_batch")
    peak = max((d.get("prompt", 0) for d in kinds(r, "usage")), default=0)
    reads = sum(1 for d in kinds(r, "tool_end") if d.get("name") == "read_file" and not d.get("error"))
    a = answer(r)
    err = [d.get("text", "") for d in kinds(r, "error")]
    kept = "APPLE" in a.upper()
    st = "PASS" if kept and peak < 32768 and not err and reads >= 7 else ("PARTIAL" if peak < 32768 and not err and reads >= 7 else "FAIL")
    return res(st, f"{reads} successful reads; peak prompt={peak}; stub batches={[d.get('why') for d in stubs]}; "
               f"compactions={len(comp)}; errors={err}; APPLE in answer={kept}", r)


def x_web(dst: Path, model: str) -> tuple[str, str]:
    """On the host (the sandbox has no internet): a temp HOME and project, approvals by key."""
    binp = dst / "swarg"
    subprocess.run(["go", "build", "-o", str(binp), "./cmd/swarg"], cwd=APP, check=True)
    home = Path(tempfile.mkdtemp(prefix="xm-web-home-"))
    proj = Path(tempfile.mkdtemp(prefix="xm-web-proj-"))
    if os.environ.get("SWARG_PROMPT_DIR"):  # the prompt variant, like run.sh does for the sandbox
        shutil.copytree(os.environ["SWARG_PROMPT_DIR"], home / ".config" / "swarg" / "blocks")
    env = dict(os.environ, HOME=str(home), XDG_CONFIG_HOME=str(home / ".config"), TERM="xterm-256color")
    here = os.getcwd()
    os.chdir(proj)  # the PTY child starts in the temp project
    try:
        t = Term([str(binp), "--base-url", "http://127.0.0.1:8080/v1", "--model", model],
                 cols=110, rows=34, env=env, log=dst / "raw.log")
    finally:
        os.chdir(here)
    t.wait("auto", 60) or t.wait("ask", 5)
    t.type("Use web_search to find which Go release added the built-in min and max functions, then use "
           "web_fetch on one result page to confirm it. Tell me the version.")
    t.send(ENTER)
    approvals, seen = 0, 0
    end = time.time() + 900
    while time.time() < end:
        s = t.text()
        n = s.count("[y]es / [n]o")
        if n and approvals < 10:
            time.sleep(0.6)  # y/n/a are ignored for 300 ms after a prompt appears (keys.go askDelay)
            approvals += 1
            t.snap(dst / f"ask-{approvals}.txt")
            t.send(ctrl("u"), pause=0.2)  # the keys answer only with an empty input
            t.send("y", pause=1.5)
            continue
        if not n and idle(s) and approvals and "Tell me the version" in s:
            time.sleep(3)
            if idle(t.text()) and "[y]es / [n]o" not in t.text():
                break
        time.sleep(0.5)
    t.snap(dst / "done.txt")
    t.send(ctrl("c"), pause=1)
    t.close(60)
    recs = []
    for f in home.rglob("*.trace.jsonl"):
        shutil.copy(f, dst / f.name)
        recs += [json.loads(x) for x in f.read_text().splitlines() if x.strip()]
    shutil.rmtree(home, ignore_errors=True)
    shutil.rmtree(proj, ignore_errors=True)
    cs = [n for n, _ in calls(recs)]
    a = answer(recs)
    ok = "web_search" in cs and "web_fetch" in cs and "1.21" in a
    st = "PASS" if ok else ("PARTIAL" if "1.21" in a or "web_search" in cs else "FAIL")
    errs = [str(d.get("brief", ""))[:60] for d in kinds(recs, "tool_end") if d.get("error")]
    return res(st, f"calls={cs}; approvals={approvals}; 1.21 in answer={'1.21' in a}; errors={errs}", recs)


# ---------------------------------------------------------------------------
# Model-independent UI scenarios (run on one model)
# ---------------------------------------------------------------------------


def u_sessions(dst: Path) -> tuple[str, str]:
    out = final.c4(dst)
    bad = [p for p in out.split("; ") if p.endswith("=False") and "not saved warning" not in p]
    return ("PASS" if not bad and "not saved warning=False" in out else "FAIL"), out


def u_features(dst: Path) -> tuple[str, str]:
    out = final.c6(dst)
    bad = [p for p in out.split("; ") if p.endswith("=False")]
    return ("PASS" if not bad else "FAIL"), out


def u_tab_hover(dst: Path) -> tuple[str, str]:
    """Tab completes a command; hover underlines a tool block and the panel model."""
    fx = fixture("hover", COUNT)
    t = final.live("swarg", dst, fixture=fx)
    t.wait("auto", 120)
    t.type("/he")
    t.send("\t", pause=0.5)
    s = t.snap(dst / "1-tab.txt")
    tab = any(l.strip("│ ").rstrip() == "/help" for l in s.splitlines()[-4:])
    t.send(ctrl("u"), pause=0.2)
    t.type("Read data.txt and tell me its first line.")
    t.send(ENTER)
    t.wait(lambda x: idle(x) and "read_file" in x, 300)
    s = t.snap(dst / "2-answer.txt")
    pos = t.find("read_file")
    under_block = False
    if pos:
        t.hover(pos[0] + 2, pos[1])
        time.sleep(0.5)
        under_block = any(c.underscore for c in t.cells(pos[1]))
    rows = t.lines()
    py = len(rows) - 1
    mx = rows[py].find("Qwen") if "Qwen" in rows[py] else rows[py].find("mlx")
    under_panel = False
    if mx >= 0:
        t.hover(mx + 2, py)
        time.sleep(0.5)
        under_panel = any(c.underscore for c in t.cells(py))
    t.snap(dst / "3-hover.txt")
    finish(t, dst)
    drop(fx)
    ok = tab and under_block and under_panel
    return ("PASS" if ok else "FAIL"), f"Tab completes /help={tab}; hover underlines block={under_block}, panel model={under_panel}"


MODEL_SCENARIOS = {
    "chat_ru": x_chat_ru, "chat_en": x_chat_en, "files": x_files, "background": x_background,
    "waiting": x_waiting, "task1": x_task1, "nested": x_nested, "esc": x_esc, "repeat": x_repeat,
    "cycle": x_cycle, "loop": x_loop, "compact": x_compact, "web": x_web,
}
UI_SCENARIOS = {"sessions": u_sessions, "features": u_features, "tab_hover": u_tab_hover}


def main() -> None:
    out, model = Path(sys.argv[1]).resolve(), sys.argv[2]
    names = []
    for a in sys.argv[3:]:
        names += list(MODEL_SCENARIOS) if a == "all" else list(UI_SCENARIOS) if a == "ui" else [a]
    os.environ["SWARG_MODEL"] = model
    final.M4B = model  # c6 waits on the panel model
    for name in names:
        dst = out / name
        shutil.rmtree(dst, ignore_errors=True)
        dst.mkdir(parents=True)
        t0 = time.time()
        fn = MODEL_SCENARIOS.get(name) or UI_SCENARIOS[name]
        try:
            st, why = fn(dst, model) if name == "web" else fn(dst)
        except Exception as e:  # a harness failure is recorded, the next scenario still runs
            st, why = "ERROR", f"harness: {type(e).__name__}: {e}"
        row = {"id": name, "model": model, "status": st, "why": why, "s": round(time.time() - t0)}
        print(json.dumps(row, ensure_ascii=False), flush=True)
        with open(out / "results.jsonl", "a") as f:
            f.write(json.dumps(row, ensure_ascii=False) + "\n")


if __name__ == "__main__":
    main()
