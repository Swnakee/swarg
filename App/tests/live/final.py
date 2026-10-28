"""T3 final regression: the owner's failed runs, replayed in the sandbox on a real model.

Usage (from App/tests/live): uv run --with pyte python final.py OUTDIR CASE [CASE ...]
Cases: c1 c1before c2a c2b c2c c2d c3 c4 c5 c6. Every case writes its evidence (screens, plain output,
the container's sessions and traces) to OUTDIR/<case>/ and one line to OUTDIR/results.txt.
Runs are sequential (the sandbox runner holds the model-server lock).
"""

from __future__ import annotations

import hashlib
import json
import re
import shutil
import subprocess
import sys
import time
from pathlib import Path

from tui import BACKSPACE, ENTER, ESC, Term, ctrl

HERE = Path(__file__).resolve().parent
SANDBOX = HERE.parent / "sandbox"
M2B, M4B, M08 = ("mlx-community/Qwen3.5-2B-4bit", "mlx-community/Qwen3.5-4B-4bit",
                 "mlx-community/Qwen3.5-0.8B-4bit")
SESSDIR = "$HOME/.config/swarg/sessions/work-" + hashlib.sha256(b"/work").hexdigest()[:8]
SAVE = 'mkdir -p /work/.out && cp -r "$HOME/.config/swarg/sessions" /work/.out/ 2>/dev/null; true'
STORY = "Напиши подробный рассказ про кота, не меньше 400 слов."  # the 2B loops on it (c2a)
LONG = ("Напиши на Python пять функций сортировки: пузырьком, вставками, слиянием, быструю и "
        "кучей, с комментарием к каждой строке и примером вызова.")  # a long text-only answer (c1)

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def results_dir(text: str) -> Path | None:
    """The run's Export/e2e directory, from the runner's last line."""
    m = re.findall(r"results: (\S+)", text)
    return Path(m[-1]) if m else None


def keep(run: Path | None, dst: Path) -> None:
    """Copy the container's saved sessions/traces (work/.out) of a run into the evidence."""
    if run and (run / "work" / ".out").exists():
        shutil.copytree(run / "work" / ".out", dst / "home", dirs_exist_ok=True)


def plain(cmd: str, dst: Path, fixture: str = "hello", run_sh: Path = SANDBOX / "run.sh") -> str:
    """A non-interactive sandbox run; returns the runner's output (also saved)."""
    p = subprocess.run([str(run_sh), fixture, "--", "sh", "-c", f"{cmd}; {SAVE}"],
                       capture_output=True, text=True)
    out = p.stdout + p.stderr
    (dst / "output.txt").write_text(out)
    keep(results_dir(out), dst)
    return out


def live(cmd: str, dst: Path, cols: int = 110, rows: int = 34, run_sh: Path = SANDBOX / "run.sh",
         fixture: str = "hello") -> Term:
    """An interactive sandbox run under the PTY driver; the raw stream goes to raw.log."""
    t = Term([str(run_sh), "-i", fixture, "--", "sh", "-c", f"{cmd}; {SAVE}"],
             cols=cols, rows=rows, log=dst / "raw.log")
    return t


def finish(t: Term, dst: Path) -> None:
    """Quit an idle UI (one Ctrl+C), wait for the container and keep its sessions."""
    t.send(ctrl("c"), pause=1)
    t.close(180)
    raw = (dst / "raw.log").read_bytes().decode("utf-8", "replace")
    keep(results_dir(raw), dst)


def idle(s: str) -> bool:
    return "Thinking" not in s and "Stopping" not in s and "esc to stop" not in s


def traces(dst: Path) -> list[dict]:
    """All trace records the case saved."""
    recs = []
    for f in sorted((dst / "home").rglob("*.trace.jsonl")):
        recs += [json.loads(line) for line in f.read_text().splitlines() if line.strip()]
    return recs


def stuck_fixture(name: str, session: Path) -> str:
    """A temporary fixture: hello plus the stuck SessionFile in .stuck/."""
    fx = SANDBOX / "fixtures" / name
    shutil.rmtree(fx, ignore_errors=True)
    shutil.copytree(SANDBOX / "fixtures" / "hello", fx)
    (fx / ".stuck").mkdir()
    shutil.copy(session, fx / ".stuck" / session.name)
    return name


def resume_cmd(file: str, text: str, model: str | None) -> str:
    """Install the stuck file as a saved Session of /work and continue it with text."""
    flag = f" --model {model}" if model else ""
    sid = json.loads((SANDBOX / "fixtures" / "final-stuck" / ".stuck" / file).read_text().splitlines()[0])["id"]
    return (f'mkdir -p {SESSDIR} && cp /work/.stuck/{file} {SESSDIR}/ && '
            f'(printf "%s\\n" "{text}" | swarg --yes -r {sid}{flag})')


def trace_to_session(trace: Path, upto_msgs: int, out: Path, model: str) -> Path:
    """Rebuild a SessionFile from a Trace: the messages sent up to the request that carried
    upto_msgs messages, without that request's new user message."""
    msgs = []
    for line in trace.read_text().splitlines():
        r = json.loads(line)
        if r["kind"] == "request":
            msgs += [m for m in r["d"]["added"] if m.get("role") != "system"]
            if r["d"]["msgs"] >= upto_msgs:
                break
    if msgs and msgs[-1]["role"] == "user":
        msgs.pop()
    lines = [json.dumps({"v": 1, "id": "b2b2b2", "cwd": "/work", "model": model,
                         "started": "2026-10-27T15:00:00Z"})]
    prev = None
    for m in msgs:
        rec = {k: m[k] for k in ("seq", "role", "content", "tool_call_id", "name") if m.get(k)}
        rec.setdefault("content", "")
        if m.get("tool_calls"):
            rec["tool_calls"] = m["tool_calls"]
        if m["role"] == "user" and prev != "user":
            rec["turn_start"] = True
        prev = m["role"]
        lines.append(json.dumps({"msg": rec}, ensure_ascii=False))
    out.write_text("\n".join(lines) + "\n")
    return out


# ---------------------------------------------------------------------------
# Cases
# ---------------------------------------------------------------------------


def c1(dst: Path, run_sh: Path = SANDBOX / "run.sh") -> str:
    """Item 4: a message sent during a long text-only answer is on screen at once."""
    t = live("swarg", dst, run_sh=run_sh)
    t.wait("auto", 120)
    t.type(LONG)
    t.send(ENTER)
    t.wait("Thinking", 120)
    t.wait(lambda s: s.count("\n  ") > 8, 120)  # several answer rows are on screen
    streaming = not idle(t.snap(dst / "1-streaming.txt"))
    t.type("второе сообщение")
    t.send(ENTER, pause=0.5)
    s = t.snap(dst / "2-half-second-after-enter.txt")
    shown, still = "второе сообщение" in s, not idle(s)
    t.wait(lambda x: idle(x) and "(waiting)" not in x, 600)
    t.snap(dst / "3-end.txt")
    finish(t, dst)
    return (f"answer streaming when sent={streaming and still}; "
            f"message on screen 0.5 s after Enter={shown}")


def c2a(dst: Path) -> str:
    """Loop guard on a real loop: the story prompt makes the 2B repeat a phrase inside one line."""
    res = []
    for model in (M2B, M4B):
        d = dst / model.split("/")[1]
        d.mkdir(parents=True, exist_ok=True)
        out = plain(f'printf "%s\\n" "{STORY}" | swarg --yes --model {model}', d)
        cut = [r["d"] for r in traces(d) if r["kind"] == "loop_guard"]
        res.append(f"{model.split('/')[1]}: guard cut={bool(cut)} "
                   f"warning={'started repeating' in out}")
    return "; ".join(res)


def c2b(dst: Path) -> str:
    """Repeat guard: the owner's 'cat /etc/hostname' x4 turn, resumed from his trace."""
    res = []
    for model in (M2B, M4B):
        d = dst / model.split("/")[1]
        d.mkdir(parents=True, exist_ok=True)
        f = trace_to_session(Path("/tmp/capsrv/t2.jsonl"), 20, d / "20261027-150000-b2b2b2.jsonl", M2B)
        fx = stuck_fixture("final-stuck", f)
        out = plain(resume_cmd(f.name, "ты же внутри докера работаешь вверно?", model), d, fixture=fx)
        calls = [r["d"]["args"] for r in traces(d) if r["kind"] == "tool_start"]
        rep = max((calls.count(c) for c in set(calls)), default=0)
        res.append(f"{model.split('/')[1]}: calls={len(calls)} max identical={rep} "
                   f"note={'2 times in a row' in json.dumps(traces(d), ensure_ascii=False)} "
                   f"stop={'kept returning the same result' in out}")
    shutil.rmtree(SANDBOX / "fixtures" / "final-stuck", ignore_errors=True)
    return "; ".join(res)


def c2d(dst: Path) -> str:
    """The owner's `find . -type d | wc -l` x16: no saved session, so asked afresh on 2B and 4B."""
    res = []
    for model in (M2B, M4B):
        d = dst / model.split("/")[1]
        d.mkdir(parents=True, exist_ok=True)
        out = plain(f'printf "%s\\n" "Сколько папок в проекте? Посчитай командой find." | swarg --yes --model {model}', d)
        calls = [r["d"]["args"] for r in traces(d) if r["kind"] == "tool_start"]
        rep = max((calls.count(c) for c in set(calls)), default=0)
        res.append(f"{model.split('/')[1]}: calls={len(calls)} max identical={rep} "
                   f"stop={'kept returning the same result' in out}")
    return "; ".join(res)


def c2c(dst: Path) -> str:
    """The 'no access' answer copied over turns (sess.jsonl): next turn on 2B and 4B."""
    res = []
    for model in (M2B, M4B):
        d = dst / model.split("/")[1]
        d.mkdir(parents=True, exist_ok=True)
        f = d / "20261027-140000-c2c2c2.jsonl"
        shutil.copy("/tmp/capsrv/sess.jsonl", f)
        fx = stuck_fixture("final-stuck", f)
        out = plain(resume_cmd(f.name, "используй инструменты что бы найти способ получить доступ", model),
                    d, fixture=fx)
        calls = [r["d"]["name"] for r in traces(d) if r["kind"] == "tool_start"]
        res.append(f"{model.split('/')[1]}: 'no access' again={'не имею доступа' in out} tools={calls}")
    shutil.rmtree(SANDBOX / "fixtures" / "final-stuck", ignore_errors=True)
    return "; ".join(res)


def c3(dst: Path) -> str:
    """User messages readable in 256 and 16 colours (fitProfile)."""
    res = []
    for name, env in (("256", ""), ("16", "TERM=xterm COLORTERM= ")):
        d = dst / name
        d.mkdir(parents=True, exist_ok=True)
        t = live(f"{env}swarg", d)
        t.wait("auto", 120)
        t.type("привет")
        t.send(ENTER)
        t.wait(lambda s: idle(s) and "▸ привет" in s, 300)
        t.snap(d / "screen.txt")
        x, y = t.find("▸ привет") or (0, 0)
        cells = t.cells(y)[x:x + 8]
        bgs = sorted({c.bg for c in cells})
        fgs = sorted({c.fg for c in cells})
        (d / "cells.txt").write_text(f"row {y}: bg={bgs} fg={fgs}\n")
        finish(t, d)
        res.append(f"{name}: user row bg={bgs} fg={fgs}")
    return "; ".join(res)


def c4(dst: Path) -> str:
    """Sessions saved in the sandbox; the -r picker lists them, Ctrl+D deletes one, Enter
    resumes another; in the app /sessions deletes a third by mouse (✕, [y] delete)."""
    t = live('for w in A B C; do printf "say %s\\n" $w | swarg --yes >/dev/null 2>>/tmp/err; sleep 1; done; '
             'cat /tmp/err; swarg -r', dst)
    t.wait(lambda s: "say C" in s or "no saved sessions" in s, 300)
    s0 = t.snap(dst / "1-picker.txt")
    t.send(ctrl("d"), pause=0.5)
    t.snap(dst / "2-confirm.txt")
    t.send("y", pause=0.8)
    s2 = t.snap(dst / "3-after-ctrl-d.txt")
    t.send(ENTER, pause=2)
    t.wait("auto", 60)
    s3 = t.snap(dst / "4-resumed.txt")
    t.type("/sessions")
    t.send(ENTER, pause=1)
    s4 = t.snap(dst / "5-in-app-picker.txt")
    other = next((w for w in ("say A", "say B") if w in s4 and "(current)" not in
                  next(l for l in s4.splitlines() if w in l)), None)
    deleted = False
    if other:
        y = next(i for i, l in enumerate(s4.splitlines()) if other in l)
        x = s4.splitlines()[y].find("✕")
        t.click(x, y)
        t.snap(dst / "6-delete-asked.txt")
        pos = t.find("[y] delete")
        if pos:
            t.click(pos[0] + 1, pos[1])
        s6 = t.snap(dst / "7-after-mouse-delete.txt")
        deleted = other not in s6
    t.send(ESC, pause=0.5)
    t.type("what did I ask you to say?")
    t.send(ENTER)
    t.wait(lambda s: idle(s) and "what did I ask" in s, 300)
    t.snap(dst / "8-answered.txt")
    finish(t, dst)
    return (f"not saved warning={'not saved' in s0}; picker listed A, B, C="
            f"{all(w in s0 for w in ('say A', 'say B', 'say C'))}; Ctrl+D removed one="
            f"{sum(w in s2 for w in ('say A', 'say B', 'say C')) == 2}; resumed shows history="
            f"{'say' in s3}; in-app picker={'(current)' in s4}; mouse delete of {other}={deleted}")


def c5(dst: Path) -> str:
    """Plain `swarg` with no flags reaches the model through SWARG_BASE_URL/SWARG_MODEL."""
    out = plain('printf "say ok\\n" | swarg --yes', dst)
    return f"connection refused={'connection refused' in out}; answered={'session saved' in out}"


def c6(dst: Path) -> str:
    """T3 features on a real model: /help, suggestions, /rename, Progress line, tok/s,
    /model mid-Session, resume keeps the model."""
    t = live("swarg; swarg -c", dst)
    t.wait("auto", 120)
    notes = []
    t.type("/mo")
    notes.append(f"suggestion for /mo={'/model' in t.snap(dst / '01-suggest.txt')}")
    t.send(BACKSPACE * 3)
    t.type("/help")
    t.send(ENTER, pause=0.8)
    notes.append(f"/help lists /model={'/model' in t.snap(dst / '02-help.txt')}")
    t.type("/rename кошачья сессия")
    t.send(ENTER, pause=0.5)
    t.type("Explain Go in ten numbered points, each with a short code example.")
    t.send(ENTER)
    t.wait(lambda x: "Thinking" in x and "1." in x, 120)  # text streams
    s = t.snap(dst / "03-progress.txt")
    lines = s.splitlines()
    p = next((i for i, l in enumerate(lines) if "Thinking" in l or "esc to stop" in l), -1)
    notes.append(f"progress line right under content={p > 0 and lines[p - 1].strip() != ''}")
    t.wait(idle, 300)
    s = t.snap(dst / "04-answered.txt")
    notes.append(f"panel tok/s={bool(re.search(r'[0-9.]+ tok/s', s.splitlines()[-1]))}")
    t.type("/model")
    t.send(ENTER)
    t.wait("/model N", 60)
    s = t.snap(dst / "05-model-list.txt")
    n = next((m.group(1) for m in re.finditer(r"^\s*(\d+) .\s*" + re.escape(M08) + r"$", s, re.M)), None)
    t.type(f"/model {n or M08}")
    t.send(ENTER, pause=0.8)
    t.type("А теперь одной фразой: что такое Rust?")
    t.send(ENTER)
    t.wait(lambda x: idle(x) and "Rust" in x, 300)
    s = t.snap(dst / "06-after-switch.txt")
    notes.append(f"panel shows 0.8B after /model={M08 in s.splitlines()[-1]}")
    t.send(ctrl("c"), pause=6)  # the first swarg ends, `swarg -c` starts (it clears the screen)
    t.wait("auto", 120)
    s = t.snap(dst / "07-resumed.txt")
    notes.append(f"resume keeps 0.8B={M08 in s.splitlines()[-1]}")
    t.type("/sessions")
    t.send(ENTER, pause=1)
    notes.append(f"Name in session list={'кошачья сессия' in t.snap(dst / '08-sessions.txt')}")
    t.send(ESC, pause=0.5)
    finish(t, dst)
    reqs = [r["d"].get("model") for r in traces(dst) if r["kind"] == "request"]
    notes.append(f"request models={reqs}")
    return "; ".join(notes)


CASES = {"c1": c1, "c2a": c2a, "c2b": c2b, "c2c": c2c, "c2d": c2d, "c3": c3, "c4": c4, "c5": c5, "c6": c6}


def main() -> None:
    out = Path(sys.argv[1]).resolve()
    for name in sys.argv[2:]:
        dst = out / name
        dst.mkdir(parents=True, exist_ok=True)
        if name == "c1before":  # the same case on the tree before T3 (c14efeb)
            res = c1(dst, run_sh=Path("/tmp/t3final/before/App/tests/sandbox/run.sh"))
        else:
            res = CASES[name](dst)
        line = f"{name}: {res}"
        print(line, flush=True)
        with open(out / "results.txt", "a") as f:
            f.write(line + "\n")


if __name__ == "__main__":
    main()
