#!/usr/bin/env python3
"""Tiny MCP server over stdio (stdlib only) for swarg tests and eval tasks.

Tools: lookup(order_id) -> status of a known order (read-only); save(name, text) -> writes the
file name (basename only) in the current directory. Unknown orders are tool errors (isError).
"""
import json
import os
import sys

# ---------------------------------------------------------------------------
# Data and tools
# ---------------------------------------------------------------------------

ORDERS = {"A-100": "shipped on 2026-10-28", "A-200": "packed, waiting for the courier", "A-300": "cancelled by the customer"}

TOOLS = [
    {"name": "lookup", "description": "Status of an order by its id, e.g. A-100.",
     "inputSchema": {"type": "object", "properties": {"order_id": {"type": "string"}}, "required": ["order_id"]},
     "annotations": {"readOnlyHint": True}},
    {"name": "save", "description": "Save text into a file of the project folder.",
     "inputSchema": {"type": "object", "properties": {"name": {"type": "string"}, "text": {"type": "string"}},
                     "required": ["name", "text"]}},
]


def _call(name, args):
    if name == "lookup":
        oid = str(args.get("order_id", "")).strip()
        if oid in ORDERS:
            return "order %s: %s" % (oid, ORDERS[oid]), False
        return "unknown order %r; known ids look like A-100" % oid, True
    if name == "save":
        fname = os.path.basename(str(args.get("name", "")))
        if not fname:
            return "name is empty", True
        with open(fname, "w", encoding="utf-8") as f:
            f.write(str(args.get("text", "")))
        return "saved %s" % fname, False
    return "unknown tool %s" % name, True


# ---------------------------------------------------------------------------
# JSON-RPC loop
# ---------------------------------------------------------------------------

def _reply(rid, result=None, error=None):
    msg = {"jsonrpc": "2.0", "id": rid}
    if error is not None:
        msg["error"] = error
    else:
        msg["result"] = result
    sys.stdout.write(json.dumps(msg) + "\n")
    sys.stdout.flush()


def main():
    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        req = json.loads(line)
        rid, method, params = req.get("id"), req.get("method"), req.get("params") or {}
        if rid is None:
            continue  # a notification
        if method == "initialize":
            _reply(rid, {"protocolVersion": params.get("protocolVersion", "2025-11-25"), "capabilities": {"tools": {}},
                         "serverInfo": {"name": "demo", "version": "1"},
                         "instructions": "lookup answers order ids like A-100; save writes a file."})
        elif method == "tools/list":
            _reply(rid, {"tools": TOOLS})
        elif method == "tools/call":
            text, is_err = _call(params.get("name"), params.get("arguments") or {})
            _reply(rid, {"content": [{"type": "text", "text": text}], "isError": is_err})
        elif method == "ping":
            _reply(rid, {})
        else:
            _reply(rid, error={"code": -32601, "message": "method not found"})


if __name__ == "__main__":
    main()
