package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Context economy: Stub batches, staleness, argument shrinking (PLAN 5.1)
//
// A Stub replaces a ToolResult in the request only. Stubs are set in batches
// because a changed prefix costs a full re-prefill on the server: at the end of
// a Turn (free, the next user Message re-renders the span anyway), when text is
// injected (also a miss) and once per Turn in an emergency.
// ---------------------------------------------------------------------------

// Names of the built-in tools the economy knows about.
const (
	toolReadFile  = "read_file"
	toolEditFile  = "edit_file"
	toolWriteFile = "write_file"
)

const (
	emergencyPercent = 65 // estimate share of ContextLimit that triggers the emergency batch
	targetPercent    = 50 // the emergency batch stops once the estimate is at or below this share
	keepRecent       = 2  // results at the end of the Turn an emergency batch never stubs
	shrinkMinBytes   = 1024
	defaultReadLimit = 200 // mirrors the read_file default, for range coverage
)

// stubber returns the runner's optional history policy, or nil.
func (a *Agent) stubber() ToolStubber {
	s, _ := a.deps.Tools.(ToolStubber)
	return s
}

// ---------------------------------------------------------------------------
// Finding the Turn and its calls
// ---------------------------------------------------------------------------

// turnStartIndex is the index of the TurnStart Message of the last Turn, or 0.
func turnStartIndex(h []chat.Message) int {
	for i := len(h) - 1; i >= 0; i-- {
		if h[i].Role == chat.RoleUser && h[i].TurnStart {
			return i
		}
	}
	return 0
}

// callOf returns the ToolCall that the tool Message at index i answers.
func callOf(h []chat.Message, i int) (chat.ToolCall, bool) {
	for j := i - 1; j >= 0; j-- {
		if h[j].Role != chat.RoleAssistant {
			continue
		}
		for _, c := range h[j].ToolCalls {
			if c.ID == h[i].ToolCallID {
				return c, true
			}
		}
		return chat.ToolCall{}, false
	}
	return chat.ToolCall{}, false
}

// ran reports whether a ToolResult is the output of a call that actually ran: not an error,
// not a denial, skip or cancel note. Only those may be stubbed or change a file.
func ran(m chat.Message) bool {
	if m.Failed {
		return false
	}
	for _, p := range []string{resDenied, resSkipped, resCancelled} {
		if strings.HasPrefix(m.Content, p) {
			return false
		}
	}
	return true
}

// cleanPath compares paths as given, after Clean, in slash form.
func cleanPath(p string) string { return filepath.ToSlash(filepath.Clean(p)) }

// readArgs are the parts of read_file arguments that decide staleness.
type readArgs struct {
	Path   string `json:"path"`
	Offset *int   `json:"offset"`
	Limit  *int   `json:"limit"`
}

// span is the line range [from, to) a read covers.
func (r readArgs) span() (from, to int) {
	from, n := 1, defaultReadLimit
	if r.Offset != nil {
		from = *r.Offset
	}
	if r.Limit != nil {
		n = *r.Limit
	}
	return from, from + n
}

// ---------------------------------------------------------------------------
// Candidates
// ---------------------------------------------------------------------------

// cand is a ToolResult that a batch may stub.
type cand struct {
	idx   int
	stub  string
	stale bool
}

// policy tells which results of the Turn a batch leaves alone.
type policy struct {
	keepLast  int  // the last N tool Messages of the Turn
	keepReads bool // a read that is not stale (the model may still need its text for edit_file)
}

// candidates lists the results of c.history[from:] that the policy allows to stub, in history order.
func (a *Agent) candidates(c *conversation, from int, pol policy) []cand {
	st := a.stubber()
	if st == nil {
		return nil
	}
	var idxs []int
	for i := from; i < len(c.history); i++ {
		if c.history[i].Role == chat.RoleTool {
			idxs = append(idxs, i)
		}
	}
	protected := map[int]bool{}
	for _, i := range idxs[max(0, len(idxs)-pol.keepLast):] {
		protected[i] = true
	}
	var out []cand
	for _, i := range idxs {
		m := c.history[i]
		if m.Stub != "" || !ran(m) || protected[i] {
			continue
		}
		call, ok := callOf(c.history, i)
		if !ok {
			continue
		}
		if why := staleReason(st, c.history, idxs, i, call); why != "" {
			var ra readArgs
			_ = json.Unmarshal([]byte(call.Args), &ra)
			out = append(out, cand{idx: i, stub: fmt.Sprintf("[read_file %s: stale, %s]", ra.Path, why), stale: true})
			continue
		}
		if call.Name == toolReadFile && pol.keepReads {
			continue
		}
		if s := st.Stub(call, m.Content, m.Brief); s != "" {
			out = append(out, cand{idx: i, stub: s})
		}
	}
	return out
}

// staleReason says why the read_file result at index i is stale, or "" when it is not: the
// file was changed afterwards by a successful edit/write, or a later successful read of the
// same path covers the same or a larger range. idxs are the tool Message indexes of the Turn in h.
func staleReason(st ToolStubber, h []chat.Message, idxs []int, i int, call chat.ToolCall) string {
	if call.Name != toolReadFile {
		return ""
	}
	var ra readArgs
	if json.Unmarshal([]byte(call.Args), &ra) != nil || ra.Path == "" {
		return ""
	}
	path := cleanPath(ra.Path)
	from, to := ra.span()
	for _, j := range idxs {
		if j <= i || !ran(h[j]) {
			continue
		}
		later, ok := callOf(h, j)
		if !ok {
			continue
		}
		switch later.Name {
		case toolEditFile, toolWriteFile:
			if p := st.Touched(later); p != "" && cleanPath(p) == path {
				return "the file changed later"
			}
		case toolReadFile:
			var rb readArgs
			if json.Unmarshal([]byte(later.Args), &rb) != nil || rb.Path == "" || cleanPath(rb.Path) != path {
				continue
			}
			if f2, t2 := rb.span(); f2 <= from && t2 >= to {
				return "read again later"
			}
		}
	}
	return ""
}

// apply sets the stubs on c and makes its meter fall back to the estimate.
func (a *Agent) apply(c *conversation, cs []cand) {
	for _, cd := range cs {
		c.history[cd.idx].Stub = cd.stub
		c.batch = append(c.batch, chat.Patch{Seq: c.history[cd.idx].Seq, Stub: cd.stub})
	}
	if len(cs) > 0 {
		c.meter.hasExact = false
	}
}

// ---------------------------------------------------------------------------
// The three batches (all called with mu held)
// ---------------------------------------------------------------------------

// finishTurnLocked is the Session's Turn-end batch: every result of the finished Turn gets its
// Stub and the big arguments of written files shrink. It reports whether anything changed.
func (a *Agent) finishTurnLocked() bool {
	s := &a.conversation
	from := turnStartIndex(a.history)
	cs := a.candidates(s, from, policy{})
	a.apply(s, cs)
	shrunk := a.shrinkArgs(from)
	if shrunk {
		a.meter.hasExact = false
	}
	a.flushBatchLocked(s, "turn_end")
	return len(cs) > 0 || shrunk
}

// stubInjectLocked is the Session's batch at inject time (the prefix changes anyway): the Turn's
// results get their Stubs except the ones of the last Step, which the model has not answered yet,
// and reads still needed for edits. It reports whether anything changed.
func (a *Agent) stubInjectLocked() bool {
	s := &a.conversation
	trailing := 0
	for i := len(a.history) - 1; i >= 0 && a.history[i].Role == chat.RoleTool; i-- {
		trailing++
	}
	cs := a.candidates(s, turnStartIndex(a.history), policy{keepLast: trailing, keepReads: true})
	a.apply(s, cs)
	a.flushBatchLocked(s, "inject")
	return len(cs) > 0
}

// stubEmergencyLocked runs the emergency batch of c at most once per Turn, when the next request
// is estimated above emergencyPercent of the limit: stale reads first, then the largest results,
// never the last keepRecent results and never a read that is not stale, until the estimate is
// at or below targetPercent. It reports whether anything changed.
func (a *Agent) stubEmergencyLocked(c *conversation) bool {
	limit := c.meter.limit
	if limit <= 0 || c.emergency {
		return false
	}
	estimate := func() int { return estimateRequest(a.request(c)).Total() }
	if estimate()*100 <= emergencyPercent*limit {
		return false
	}
	cs := a.candidates(c, turnStartIndex(c.history), policy{keepLast: keepRecent, keepReads: true})
	slices.SortStableFunc(cs, func(x, y cand) int {
		if x.stale != y.stale {
			if x.stale {
				return -1
			}
			return 1
		}
		return len(c.history[y.idx].Content) - len(c.history[x.idx].Content)
	})
	done := 0
	for _, cd := range cs {
		if done > 0 && estimate()*100 <= targetPercent*limit {
			break
		}
		a.apply(c, []cand{cd})
		done++
	}
	c.emergency = done > 0
	a.flushBatchLocked(c, "emergency")
	return done > 0
}

// ---------------------------------------------------------------------------
// Argument shrinking
// ---------------------------------------------------------------------------

// shrinkFields names the big string arguments that shrink, per tool.
var shrinkFields = map[string][]string{
	toolWriteFile: {"content"},
	toolEditFile:  {"old", "new"},
}

// shrinkArgs rewrites the big arguments of write_file and edit_file calls of h[from:] to a
// one-line note. The new Args are valid JSON with the same keys. Messages are copied, never
// edited in place, so a History() copy held elsewhere is not touched.
func (a *Agent) shrinkArgs(from int) (changed bool) {
	for i := from; i < len(a.history); i++ {
		m := a.history[i]
		if m.Role != chat.RoleAssistant || len(m.ToolCalls) == 0 {
			continue
		}
		calls := slices.Clone(m.ToolCalls)
		args := map[string]string{}
		for k, c := range calls {
			if shrunk, ok := shrinkCallArgs(c); ok {
				calls[k].Args = shrunk
				args[c.ID] = shrunk
			}
		}
		if len(args) > 0 {
			a.history[i].ToolCalls, changed = calls, true
			a.batch = append(a.batch, chat.Patch{Seq: m.Seq, Args: args})
		}
	}
	return changed
}

// shrinkCallArgs returns the shrunk arguments of a call, or false when nothing is to shrink.
func shrinkCallArgs(c chat.ToolCall) (string, bool) {
	fields := shrinkFields[c.Name]
	if fields == nil {
		return "", false
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal([]byte(c.Args), &obj) != nil || obj == nil {
		return "", false
	}
	changed := false
	for _, f := range fields {
		var text string
		if json.Unmarshal(obj[f], &text) != nil || len(text) <= shrinkMinBytes {
			continue
		}
		note, _ := marshalNoEscape(omittedNote(text))
		obj[f], changed = note, true
	}
	if !changed {
		return "", false
	}
	out, err := marshalNoEscape(obj)
	if err != nil {
		return "", false
	}
	return string(out), true
}

// omittedNote is the text that takes the place of a big argument.
func omittedNote(text string) string {
	lines := strings.Count(text, "\n")
	if !strings.HasSuffix(text, "\n") {
		lines++
	}
	return fmt.Sprintf("(omitted from history: %d lines, %.1f KB; the file on disk has it)", lines, float64(len(text))/1024)
}

// marshalNoEscape is json.Marshal without HTML escaping and without the trailing newline.
func marshalNoEscape(v any) (json.RawMessage, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}
