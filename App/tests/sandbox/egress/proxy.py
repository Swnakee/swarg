#!/usr/bin/env python3
"""Egress allow-list proxy for the swarg e2e sandbox (HTTP CONNECT only).

Usage: proxy.py PORT HOST[,HOST...]

Allows CONNECT host:443 for the listed hosts (exact match, case-insensitive) and refuses
everything else (403, or 405 for non-CONNECT methods). Every decision is logged to stdout
as "ALLOW host:port" / "DENY host:port reason". Headers and bodies are never logged.
"""
import asyncio
import sys

ALLOWED_PORT = 443


# ---------------------------------------------------------------------------
# Decision
# ---------------------------------------------------------------------------

def decide(target: str, hosts: set) -> tuple:
    """Return (allowed, host, port, reason) for a CONNECT target "host:port"."""
    host, sep, port_s = target.rpartition(":")
    if not sep or not host or not port_s.isdigit():
        return False, target, 0, "bad target"
    host = host.strip("[]").lower().rstrip(".")
    port = int(port_s)
    if port != ALLOWED_PORT:
        return False, host, port, "port not allowed"
    if host not in hosts:
        return False, host, port, "host not in allow-list"
    return True, host, port, ""


# ---------------------------------------------------------------------------
# Tunnel
# ---------------------------------------------------------------------------

async def _pipe(r: asyncio.StreamReader, w: asyncio.StreamWriter) -> None:
    try:
        while True:
            data = await r.read(65536)
            if not data:
                break
            w.write(data)
            await w.drain()
    except (ConnectionError, asyncio.CancelledError):
        pass
    finally:
        try:
            w.close()
        except Exception:
            pass


async def _handle(r: asyncio.StreamReader, w: asyncio.StreamWriter, hosts: set) -> None:
    try:
        line = await asyncio.wait_for(r.readline(), 15)
        parts = line.decode("latin-1").split()
        # drain the request headers
        while True:
            h = await asyncio.wait_for(r.readline(), 15)
            if h in (b"\r\n", b"\n", b""):
                break
        if len(parts) < 2 or parts[0].upper() != "CONNECT":
            print("DENY %s method not allowed" % (parts[1] if len(parts) > 1 else "?"), flush=True)
            w.write(b"HTTP/1.1 405 Method Not Allowed\r\nContent-Length: 0\r\n\r\n")
            await w.drain()
            return
        ok, host, port, why = decide(parts[1], hosts)
        if not ok:
            print("DENY %s:%s %s" % (host, port, why), flush=True)
            w.write(b"HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\n\r\n")
            await w.drain()
            return
        try:
            ur, uw = await asyncio.wait_for(asyncio.open_connection(host, port), 15)
        except Exception as e:
            print("DENY %s:%s upstream failed: %s" % (host, port, type(e).__name__), flush=True)
            w.write(b"HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n")
            await w.drain()
            return
        print("ALLOW %s:%s" % (host, port), flush=True)
        w.write(b"HTTP/1.1 200 Connection Established\r\n\r\n")
        await w.drain()
        await asyncio.gather(_pipe(r, uw), _pipe(ur, w))
    except Exception:
        pass
    finally:
        try:
            w.close()
        except Exception:
            pass


async def _main(port: int, hosts: set) -> None:
    srv = await asyncio.start_server(lambda r, w: _handle(r, w, hosts), "0.0.0.0", port)
    print("egress proxy on :%d allow=%s" % (port, ",".join(sorted(hosts))), flush=True)
    async with srv:
        await srv.serve_forever()


if __name__ == "__main__":
    if len(sys.argv) != 3:
        sys.exit("usage: proxy.py PORT HOST[,HOST...]")
    allow = {h.strip().lower() for h in sys.argv[2].split(",") if h.strip()}
    if not allow:
        sys.exit("empty allow-list")
    asyncio.run(_main(int(sys.argv[1]), allow))
