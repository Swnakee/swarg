# Module guard (`App/internal/agent/guard`)

Purpose: the two repetition guards of the agent, moved out of `agent` (T14 S5). Stdlib only, no import of `agent`; the agent reaches them through this small API.

API:
- `Loop` (zero value ready; `Feed(chunk) (cut string, tripped bool)`) - the loop guard of one model Step (line rule, tail rule, period rule; PLAN 2.10). On a trip `cut` is the text before the second copy. Behaviour is described in `agent.md` (Context economy, "Loop guard").
- `Repeat` (zero value ready, one goroutine) - the repeat guard of one Turn (R-24, R-44). `NewStep()` starts a Step (one decision counts a call+outcome once, F12); `Observe(Call, Outcome) (note string, stop bool)` records one call; `Warning() string` is the stop message after `stop`. Behaviour: `agent.md` ("Repeat guard").
- `Call{Name, Args}` and `Outcome{Output, Err, Denied}` - the guard's own input types (it does not see the agent's `pendingCall` / `result`).
- `RepeatNote` - the note appended to a failing repeated ToolResult (exported because agent tests assert the history text).

Invariants:
- No behaviour or text differs from the pre-move code in `agent`; the tests moved with their names and rule comments.
- Users in `agent`: `turn.go` (`guard.Loop` field, `guard.Repeat` per Turn), `tools.go` (`runCalls` builds `Call`/`Outcome`), `task.go`, `subagents.go` (pass a `*guard.Repeat`).

Gotchas:
- `Observe` takes the raw call arguments; `canonicalArgs` re-encodes the JSON so key order and spacing do not matter.
