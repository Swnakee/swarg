"""Drive a full-screen terminal program through a pseudo terminal and read its screen.

Ghostty scripting cannot send letters, ctrl+letter or the mouse; this driver can. It runs a
command (usually `App/tests/sandbox/run.sh -i ...`) under a PTY of a fixed size, feeds the
output into a pyte terminal emulator and lets a script type, press keys, click (SGR mouse) and
wait for text on the screen. Run with: uv run --with pyte python <script>.
"""

from __future__ import annotations

import fcntl
import os
import pty
import struct
import subprocess
import termios
import threading
import time
from pathlib import Path

import pyte

# ---------------------------------------------------------------------------
# Keys (what a terminal sends)
# ---------------------------------------------------------------------------

ENTER = "\r"
ESC = "\x1b"
TAB = "\t"
BACKSPACE = "\x7f"
UP, DOWN = "\x1b[A", "\x1b[B"


def ctrl(letter: str) -> str:
    """Ctrl+letter as a control byte (ctrl("d") is EOT)."""
    return chr(ord(letter.lower()) - ord("a") + 1)


# ---------------------------------------------------------------------------
# Emulator: pyte plus the scroll sequences Bubble Tea's renderer uses
# ---------------------------------------------------------------------------


class _Screen(pyte.Screen):
    """pyte ignores CSI S / CSI T (scroll up / down inside the margins); the renderer scrolls
    the content with them, so without these the screen shows rows of earlier frames."""

    def scroll_up(self, count: int | None = None) -> None:
        self._scroll(count or 1, up=True)

    def scroll_down(self, count: int | None = None) -> None:
        self._scroll(count or 1, up=False)

    def _scroll(self, n: int, up: bool) -> None:
        top, bottom = self.margins or (0, self.lines - 1)
        x, y = self.cursor.x, self.cursor.y
        for _ in range(n):
            self.cursor.y = bottom if up else top
            self.index() if up else self.reverse_index()
        self.cursor.x, self.cursor.y = x, y


class _Stream(pyte.ByteStream):
    csi = dict(pyte.ByteStream.csi, S="scroll_up", T="scroll_down")


# ---------------------------------------------------------------------------
# Terminal
# ---------------------------------------------------------------------------


class Term:
    """One program under a PTY with an emulated screen of cols x rows."""

    def __init__(self, argv: list[str], cols: int = 100, rows: int = 30,
                 env: dict | None = None, log: Path | None = None) -> None:
        self.screen = _Screen(cols, rows)
        self._stream = _Stream(self.screen)
        self._lock = threading.Lock()
        self._log = open(log, "wb") if log else None
        master, slave = pty.openpty()
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))
        self.proc = subprocess.Popen(argv, stdin=slave, stdout=slave, stderr=slave, env=env,
                                     start_new_session=True, close_fds=True)
        os.close(slave)
        self._fd = master
        self._reader = threading.Thread(target=self._read, daemon=True)
        self._reader.start()

    def _read(self) -> None:
        while True:
            try:
                data = os.read(self._fd, 65536)
            except OSError:
                return
            if not data:
                return
            with self._lock:
                self._stream.feed(data)
                if self._log:
                    self._log.write(data)

    # --- input ---------------------------------------------------------------

    def send(self, s: str, pause: float = 0.15) -> None:
        """Write raw input; pause lets the program handle it (a lone ESC needs a gap)."""
        os.write(self._fd, s.encode())
        time.sleep(pause)

    def type(self, text: str) -> None:
        """Type text one key at a time (not a paste)."""
        for ch in text:
            self.send(ch, pause=0.01)
        time.sleep(0.1)

    def click(self, x: int, y: int) -> None:
        """Left press and release on screen cell (x, y), 0-based (SGR mouse)."""
        self.send(f"\x1b[<0;{x + 1};{y + 1}M", pause=0.05)
        self.send(f"\x1b[<0;{x + 1};{y + 1}m")

    def hover(self, x: int, y: int) -> None:
        """Pointer motion without a button to cell (x, y)."""
        self.send(f"\x1b[<35;{x + 1};{y + 1}M")

    # --- screen --------------------------------------------------------------

    def lines(self) -> list[str]:
        with self._lock:
            return [line.rstrip() for line in self.screen.display]

    def text(self) -> str:
        return "\n".join(self.lines())

    def find(self, part: str) -> tuple[int, int] | None:
        """The (x, y) cell where part first appears on screen, or None."""
        for y, line in enumerate(self.lines()):
            x = line.find(part)
            if x >= 0:
                return x, y
        return None

    def cells(self, y: int):
        """The pyte cells of screen row y (char, fg, bg, underscore, ...)."""
        with self._lock:
            return [self.screen.buffer[y][x] for x in range(self.screen.columns)]

    def wait(self, cond, timeout: float = 60, every: float = 0.2) -> bool:
        """Wait until cond holds: a string on screen, or a callable taking the screen text."""
        end = time.time() + timeout
        while time.time() < end:
            t = self.text()
            if (cond in t) if isinstance(cond, str) else cond(t):
                return True
            if self.proc.poll() is not None:
                time.sleep(0.3)
                t = self.text()
                return (cond in t) if isinstance(cond, str) else bool(cond(t))
            time.sleep(every)
        return False

    def snap(self, path: Path) -> str:
        """Write the screen text to path and return it."""
        t = self.text()
        path.write_text(t + "\n")
        return t

    def close(self, timeout: float = 120) -> int | None:
        """Wait for the program to end (never kill a client mid-request: the runner waits)."""
        try:
            return self.proc.wait(timeout=timeout)
        except subprocess.TimeoutExpired:
            return None
        finally:
            if self._log:
                self._log.flush()
