# Tool process protocol (JSON lines)

A tool process reads one JSON object per line from stdin and writes one JSON object per line to
stdout, in order. It must never exit before stdin is closed.

Request:  {"id": 1, "name": "word_count", "args": {"text": "a b c"}}
Response: {"id": 1, "result": {"words": 3, "chars": 5}}

Rules:
- `id` is echoed back unchanged.
- unknown `name`: {"id": <id>, "error": "unknown tool: <name>"}
- a line that is not valid JSON: {"error": "bad request"} and the process keeps running
- `word_count` counts whitespace-separated words and all characters of `text`
- flush stdout after every line
