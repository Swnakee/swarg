# Module webget (`App/internal/webget`)

Purpose: the shared web access of `web_search` and `web_fetch` (R-47, R-70..R-72, I-15): a guarded HTTP GET and HTML to text. Moved out of `tools` in T14 S11. Imports no swarg package.

API:
- `Getter{SearxURL, AllowPrivate, Timeout, Lookup, SearchURL}` - the zero value is safe: 15 s timeout, private addresses refused, DuckDuckGo. `Lookup` (resolver) and `SearchURL` are test seams; `SearxURL` is read by `web_search`.
- `(Getter) Get(ctx, rawURL, allowPrivate) (body, ctype, err)` - at most `MaxBody` (1 MB) of the body and its Content-Type; HTTP >= 400 is an error. `allowPrivate` is for the search backends the user configured, never for model-chosen URLs (web_fetch passes `Getter.AllowPrivate`).
- `ParseURL(raw) (*url.URL, error)` - absolute http or https only.
- `HTMLToText(r, ctype, base) (string, error)` - readable text: no script/style, nav/aside/footer dropped (R-71), `#` headings, `- ` items, charset-aware; with a non-nil `base` links are written `text (absolute url)` (R-72). `Attr(n, key)` - an attribute value of an html node (shared with the search parser).

Invariants:
- Every address of a host is checked (`blockedIP`: loopback, private, carrier-grade NAT 100.64/10, link-local, multicast, unspecified) and the connection goes to a checked one, for every redirect hop; no proxy (I-15).
- At most 5 redirects; the body limit is read, never more.

Gotchas:
- A refusal is a `refusedError` whose text starts `refused:`; `webError` turns other transport errors into one clear line (`timed out`, `cannot reach the server (...)`).
- SSRF tests live here (`TestBlockedIP`, `TestWebFetch_RefusesPrivateAddresses`, `TestWebFetch_ConfigAllowsPrivate`); the tool-level web_fetch tests stay in `tools`.
