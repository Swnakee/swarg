#!/usr/bin/env python3
"""Deterministic fake web for the web-* eval tasks (runs in its own container, see README.md).

  server.py <site_dir> [port]

Pages: site/<host>/<path> served for http://<host>/<path> (the Host header picks the folder,
"/" and "/dir/" mean index.html). A page with "noindex" in its first 400 bytes is
reachable only through links. Search: http://search.test/search?q=..&format=json answers like
the SearXNG JSON API; pages are ranked by how many query words they contain (ties by url).
"""
import html
import json
import os
import re
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlsplit

SITE = os.path.abspath(sys.argv[1])
PORT = int(sys.argv[2]) if len(sys.argv) > 2 else 80

# ---------------------------------------------------------------------------
# Index: every page as (url, title, description, words)
# ---------------------------------------------------------------------------


def _page(path):
    raw = open(path, encoding="utf-8").read()
    title = re.search(r"<title>(.*?)</title>", raw, re.S)
    desc = re.search(r'<meta name="description" content="(.*?)"', raw, re.S)
    body = re.sub(r"<(script|style)[^>]*>.*?</\1>", " ", raw, flags=re.S)
    text = html.unescape(re.sub(r"<[^>]+>", " ", body))
    return (html.unescape(title.group(1).strip()) if title else "",
            html.unescape(desc.group(1).strip()) if desc else "",
            set(re.findall(r"[a-z0-9.]+", text.lower())))


INDEX = []
for host in sorted(os.listdir(SITE)):
    for root, _, files in os.walk(os.path.join(SITE, host)):
        for f in sorted(files):
            if f.endswith(".html") and "noindex" not in open(os.path.join(root, f), encoding="utf-8").read(400):
                rel = os.path.relpath(os.path.join(root, f), os.path.join(SITE, host))
                url = "http://%s/%s" % (host, "" if rel == "index.html" else rel)
                INDEX.append((url,) + _page(os.path.join(root, f)))


def search(q):
    words = [w for w in re.findall(r"[a-z0-9.]+", q.lower()) if len(w) > 2]
    scored = []
    for url, title, desc, text in INDEX:
        n = sum(1 for w in words if w in text or any(t.startswith(w[:5]) for t in text if len(w) > 5))
        if n * 2 >= len(words) and n > 0:  # at least half of the words
            scored.append((-n, url, title, desc))
    scored.sort()
    return [{"title": t, "url": u, "content": d} for _, u, t, d in scored[:8]]

# ---------------------------------------------------------------------------
# HTTP
# ---------------------------------------------------------------------------


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        host = (self.headers.get("Host") or "").split(":")[0]
        u = urlsplit(self.path)
        if host == "search.test" and u.path == "/search":
            q = parse_qs(u.query).get("q", [""])[0]
            return self._send(200, "application/json", json.dumps({"query": q, "results": search(q)}))
        path = u.path + ("index.html" if u.path.endswith("/") else "")
        full = os.path.normpath(os.path.join(SITE, host, path.lstrip("/")))
        if not full.startswith(os.path.join(SITE, host) + os.sep) or not os.path.isfile(full):
            return self._send(404, "text/html", "<html><body><h1>404 Not Found</h1></body></html>")
        self._send(200, "text/html; charset=utf-8", open(full, encoding="utf-8").read())

    def _send(self, code, ctype, body):
        data = body.encode("utf-8")
        self.send_response(code)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def log_message(self, fmt, *args):
        sys.stderr.write("[web] %s\n" % (fmt % args))


if __name__ == "__main__":
    ThreadingHTTPServer(("0.0.0.0", PORT), Handler).serve_forever()
