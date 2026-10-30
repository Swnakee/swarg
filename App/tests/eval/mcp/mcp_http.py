#!/usr/bin/env python3
"""Tiny MCP server over Streamable HTTP (stdlib only) for the mcp-http-* eval tasks.

  mcp_http.py [port] [--token TOKEN] [--sse]

Same tools as mcp_demo.py (lookup, save). Endpoint /mcp: POST a JSON-RPC message, the answer is
application/json (or an SSE stream with --sse); notifications get 202; a session id is handed out
at initialize and required afterwards (400 missing, 404 unknown); DELETE ends the session. With
--token every request needs "Authorization: Bearer TOKEN" (401 otherwise).
"""
import json
import os
import sys
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import mcp_demo  # noqa: E402  (the tools and the data)

ARGS = sys.argv[1:]
PORT = int(ARGS[0]) if ARGS and ARGS[0].isdigit() else 80
TOKEN = ARGS[ARGS.index("--token") + 1] if "--token" in ARGS else ""
SSE = "--sse" in ARGS
SESSIONS = set()


def _handle(req):
    """Answer one JSON-RPC request (None for a notification)."""
    rid, method, params = req.get("id"), req.get("method"), req.get("params") or {}
    if rid is None:
        return None
    if method == "initialize":
        result = {"protocolVersion": params.get("protocolVersion", "2025-11-25"), "capabilities": {"tools": {}},
                  "serverInfo": {"name": "orders", "version": "1"},
                  "instructions": "lookup answers order ids like A-100; save writes a file."}
    elif method == "tools/list":
        result = {"tools": mcp_demo.TOOLS}
    elif method == "tools/call":
        text, is_err = mcp_demo._call(params.get("name"), params.get("arguments") or {})
        result = {"content": [{"type": "text", "text": text}], "isError": is_err}
    elif method == "ping":
        result = {}
    else:
        return {"jsonrpc": "2.0", "id": rid, "error": {"code": -32601, "message": "method not found"}}
    return {"jsonrpc": "2.0", "id": rid, "result": result}


class Handler(BaseHTTPRequestHandler):
    def _fail(self, code, text):
        self.send_response(code)
        self.send_header("Content-Type", "text/plain")
        self.end_headers()
        self.wfile.write(text.encode())

    def _authorized(self):
        if TOKEN and self.headers.get("Authorization") != "Bearer " + TOKEN:
            self._fail(401, "unauthorized")
            return False
        return True

    def do_DELETE(self):
        if not self._authorized():
            return
        SESSIONS.discard(self.headers.get("Mcp-Session-Id", ""))
        self.send_response(200)
        self.end_headers()

    def do_POST(self):
        if not self._authorized():
            return
        if self.path != "/mcp":
            return self._fail(404, "not found")
        body = self.rfile.read(int(self.headers.get("Content-Length", "0")))
        try:
            req = json.loads(body)
        except ValueError:
            return self._fail(400, "bad json")
        sid = self.headers.get("Mcp-Session-Id", "")
        new_session = ""
        if req.get("method") == "initialize":
            new_session = uuid.uuid4().hex
            SESSIONS.add(new_session)
        elif sid == "":
            return self._fail(400, "session id required")
        elif sid not in SESSIONS:
            return self._fail(404, "session not found")
        reply = _handle(req)
        if reply is None:
            self.send_response(202)
            self.end_headers()
            return
        data = json.dumps(reply).encode()
        self.send_response(200)
        if new_session:
            self.send_header("Mcp-Session-Id", new_session)
        if SSE:
            self.send_header("Content-Type", "text/event-stream")
            self.end_headers()
            self.wfile.write(b"id: 1\ndata: \n\ndata: " + data + b"\n\n")
        else:
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)

    def log_message(self, fmt, *args):
        sys.stderr.write("%s %s\n" % (self.address_string(), fmt % args))


if __name__ == "__main__":
    ThreadingHTTPServer(("", PORT), Handler).serve_forever()
