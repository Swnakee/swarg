package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"swarg/internal/chat"
	"swarg/internal/tools"
)

// ---------------------------------------------------------------------------
// econTools: a ToolRunner (+ ToolStubber) with the names of the built-in file tools
// ---------------------------------------------------------------------------

// econTools runs instantly. read_file is read-only; edit_file, write_file and shell mutate;
// shell needs approval. Output of call id is out[id], else "out:<id>"; a failing id errors.
type econTools struct {
	mu    sync.Mutex
	out   map[string]string
	fail  map[string]bool
	onRun func(id string)
}

var (
	_ ToolRunner  = (*econTools)(nil)
	_ ToolStubber = (*econTools)(nil)
)

func newEconTools() *econTools {
	return &econTools{out: map[string]string{}, fail: map[string]bool{}}
}

func (e *econTools) Specs() []chat.ToolSpec {
	return []chat.ToolSpec{{Name: "read_file", Description: "read", Schema: json.RawMessage(`{"type":"object"}`)}}
}

func (e *econTools) Kind(name string) (known, readOnly, confirm bool) {
	switch name {
	case "read_file":
		return true, true, false
	case "edit_file", "write_file":
		return true, false, false
	case "shell":
		return true, false, true
	}
	return false, false, false
}

func (e *econTools) Detachable(string) bool { return false }

func (e *econTools) Label(c chat.ToolCall) string { return "L:" + c.Name }

func (e *econTools) Run(_ context.Context, c chat.ToolCall, _ ToolHooks) (string, string, error) {
	if e.onRun != nil {
		e.onRun(c.ID)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.fail[c.ID] {
		return "", "", errors.New("boom " + c.ID)
	}
	if s, ok := e.out[c.ID]; ok {
		return s, "brief:" + c.ID, nil
	}
	return "out:" + c.ID, "brief:" + c.ID, nil
}

// Stub mimics tools.Set: "" when the stub would not be shorter than the output.
func (e *econTools) Stub(c chat.ToolCall, output, brief string) string {
	var stub string
	switch c.Name {
	case "read_file":
		stub = "[read_file " + pathArg(c) + ": S]"
	case "shell":
		stub = "[shell S " + c.ID + "]"
	}
	if len(stub) >= len(output) {
		return ""
	}
	return stub
}

func (e *econTools) Touched(c chat.ToolCall) string {
	if c.Name == "edit_file" || c.Name == "write_file" {
		return pathArg(c)
	}
	return ""
}

func pathArg(c chat.ToolCall) string {
	var a struct{ Path string }
	_ = json.Unmarshal([]byte(c.Args), &a)
	return a.Path
}

func readCall(i int, path string) chat.ToolCall {
	return toolCall(i, "read_file", fmt.Sprintf(`{"path":%q}`, path))
}

func editCall(i int, path string) chat.ToolCall {
	return toolCall(i, "edit_file", fmt.Sprintf(`{"path":%q,"old":"a","new":"b"}`, path))
}

// shellCall is call number i; every call differs, as the repeat guard stops identical ones.
func shellCall(i int) chat.ToolCall {
	return toolCall(i, "shell", fmt.Sprintf(`{"command":"go test ./p%d"}`, i))
}

// big returns a text of n bytes.
func big(n int) string { return strings.Repeat("x", n) }

// script runs one Turn "user" whose Steps are the given call lists and whose last answer is
// final; it returns every request of the Turn in order.
func script(t *testing.T, r *rig, user string, steps [][]chat.ToolCall, final string) []chat.Request {
	t.Helper()
	r.a.Submit(user)
	var reqs []chat.Request
	for _, calls := range steps {
		c := r.p.next(t)
		reqs = append(reqs, c.req)
		c.toolStep("", calls...)
	}
	c := r.p.next(t)
	reqs = append(reqs, c.req)
	c.reply(final)
	wait(t, r.a)
	assertHistoryValid(t, r.a.History())
	return reqs
}

// nextTurn runs a text-only Turn and returns its request.
func nextTurn(t *testing.T, r *rig, user string) chat.Request {
	t.Helper()
	r.a.Submit(user)
	c := r.p.next(t)
	c.reply("ok")
	wait(t, r.a)
	return c.req
}

// econRig starts a rig with econTools and the allow-all approval.
func econRig(t *testing.T, et *econTools, mod ...func(*Deps)) *rig {
	t.Helper()
	return toolRigWith(t, et, mod...)
}

func toolRigWith(t *testing.T, tr ToolRunner, mod ...func(*Deps)) *rig {
	t.Helper()
	return startRig(t, append([]func(*Deps){func(d *Deps) { d.Tools = tr }}, mod...)...)
}

// isPrefix reports whether a is a prefix of b, message by message.
func isPrefix(a, b []chat.Message) bool {
	return len(a) <= len(b) && reflect.DeepEqual(a, b[:len(a)])
}

// toolContents returns the Content of the tool Messages of a request.
func toolContents(req chat.Request) (out []string) {
	for _, m := range req.Messages {
		if m.Role == chat.RoleTool {
			out = append(out, m.Content)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Request: Stub instead of Content
// ---------------------------------------------------------------------------

func TestRequest_SendsStubInsteadOfContent(t *testing.T) {
	et := newEconTools()
	et.out["c1"] = big(300)
	r := econRig(t, et)
	script(t, r, "go", [][]chat.ToolCall{{readCall(1, "a.go")}}, "done")

	req := nextTurn(t, r, "next")
	if got := toolContents(req); len(got) != 1 || got[0] != "[read_file a.go: S]" {
		t.Fatalf("request tool contents = %q, want the Stub", got)
	}
	var m chat.Message
	for _, h := range r.a.History() {
		if h.Role == chat.RoleTool {
			m = h
		}
	}
	if m.Content != big(300) || m.Stub != "[read_file a.go: S]" {
		t.Errorf("history keeps Content %d bytes and Stub %q", len(m.Content), m.Stub)
	}
}

func TestRequest_StubCountsAsStubInTheEstimate(t *testing.T) {
	et := newEconTools()
	et.out["c1"] = big(400)
	r := econRig(t, et)
	script(t, r, "go", [][]chat.ToolCall{{readCall(1, "a.go")}}, "done")
	bd := r.rec.lastMeter(t).Breakdown
	if bd.Stubs == 0 || bd.ResultsFull != 0 {
		t.Errorf("breakdown = %+v, want the result counted as a stub", bd)
	}
}

// ---------------------------------------------------------------------------
// Append-only inside a Turn
// ---------------------------------------------------------------------------

func TestRequest_AppendOnlyInsideTurn(t *testing.T) {
	et := newEconTools()
	r := econRig(t, et)
	reqs := script(t, r, "go", [][]chat.ToolCall{
		{readCall(1, "a.go")}, {readCall(2, "b.go"), readCall(3, "c.go")}, {editCall(4, "a.go")},
	}, "done")
	if len(reqs) != 4 {
		t.Fatalf("requests = %d, want 4", len(reqs))
	}
	for n := 1; n < len(reqs); n++ {
		if !isPrefix(reqs[n-1].Messages, reqs[n].Messages) {
			t.Errorf("request %d does not start with request %d", n, n-1)
		}
	}
}

func TestRequest_AppendOnlyAcrossFailedCall(t *testing.T) {
	et := newEconTools()
	et.fail["c1"] = true
	r := econRig(t, et)
	reqs := script(t, r, "go", [][]chat.ToolCall{{readCall(1, "missing.go")}, {readCall(2, "a.go")}}, "done")
	for n := 1; n < len(reqs); n++ {
		if !isPrefix(reqs[n-1].Messages, reqs[n].Messages) {
			t.Errorf("request %d does not start with request %d", n, n-1)
		}
	}
}

// ---------------------------------------------------------------------------
// Turn-end batch
// ---------------------------------------------------------------------------

func TestEndTurn_StubsFinishedTurn(t *testing.T) {
	et := newEconTools()
	for _, id := range []string{"c1", "c2", "c3"} {
		et.out[id] = big(200)
	}
	r := econRig(t, et)
	script(t, r, "go", [][]chat.ToolCall{{readCall(1, "a.go"), readCall(2, "b.go")}, {readCall(3, "c.go")}}, "final answer")

	req := nextTurn(t, r, "next")
	want := []string{"[read_file a.go: S]", "[read_file b.go: S]", "[read_file c.go: S]"}
	if got := toolContents(req); !reflect.DeepEqual(got, want) {
		t.Fatalf("tool contents = %q, want %q", got, want)
	}
	var user, answer bool
	for _, m := range req.Messages {
		user = user || (m.Role == chat.RoleUser && m.Content == "go" && m.Stub == "")
		answer = answer || (m.Role == chat.RoleAssistant && m.Content == "final answer" && m.Stub == "")
	}
	if !user || !answer {
		t.Errorf("user Message and final answer must be unchanged: %+v", req.Messages)
	}
}

func TestEndTurn_EmptyStubKeepsContent(t *testing.T) {
	et := newEconTools()
	r := econRig(t, et)
	script(t, r, "go", [][]chat.ToolCall{{editCall(1, "a.go")}}, "done")
	if got := toolContents(nextTurn(t, r, "next")); len(got) != 1 || got[0] != "out:c1" {
		t.Errorf("contents = %q, want the edit result unchanged", got)
	}
}

func TestEndTurn_TinyResultStaysFull(t *testing.T) {
	et := newEconTools() // "out:c1" is shorter than any stub
	r := econRig(t, et)
	script(t, r, "go", [][]chat.ToolCall{{readCall(1, "a.go")}}, "done")
	if got := toolContents(nextTurn(t, r, "next")); got[0] != "out:c1" {
		t.Errorf("contents = %q", got)
	}
}

func TestEndTurn_NoStubberNoBatch(t *testing.T) {
	ft := newFakeTools() // does not implement ToolStubber
	r := toolRig(t, ft)
	r.a.Submit("go")
	r.p.next(t).toolStep("", toolCall(1, toolRead, ""))
	r.p.next(t).reply("done")
	wait(t, r.a)
	for _, m := range r.a.History() {
		if m.Stub != "" {
			t.Errorf("message %+v got a Stub without a ToolStubber", m)
		}
	}
}

func TestEndTurn_TextOnlyTurnKeepsExactMeter(t *testing.T) {
	r := econRig(t, newEconTools())
	r.a.Submit("hi")
	c := r.p.next(t)
	c.usage(300, 40)
	c.reply("ok")
	wait(t, r.a)
	if m := r.rec.lastMeter(t); m.Estimated || m.Used != 340 {
		t.Errorf("meter = %+v, want the exact 340", m)
	}
}

func TestEndTurn_BatchMakesMeterAnEstimate(t *testing.T) {
	et := newEconTools()
	et.out["c1"] = big(2000)
	r := econRig(t, et)
	r.a.Submit("go")
	c := r.p.next(t)
	c.toolStep("", readCall(1, "a.go"))
	c = r.p.next(t)
	c.usage(900, 10)
	c.reply("done")
	wait(t, r.a)
	if m := r.rec.lastMeter(t); !m.Estimated || m.Used >= 900 {
		t.Errorf("meter = %+v, want the smaller stub-aware estimate", m)
	}
}

func TestEndTurn_ErrorTurnStillStubsCompletedResults(t *testing.T) {
	et := newEconTools()
	et.out["c1"] = big(200)
	r := econRig(t, et)
	r.a.Submit("go")
	r.p.next(t).toolStep("", readCall(1, "a.go"))
	r.p.next(t).end(chat.FinishError, errors.New("boom"))
	wait(t, r.a)
	assertHistoryValid(t, r.a.History())
	if got := toolContents(nextTurn(t, r, "again")); len(got) != 1 || got[0] != "[read_file a.go: S]" {
		t.Errorf("contents = %q, want the stub", got)
	}
}

func TestEndTurn_DeniedResultIsNotStubbed(t *testing.T) {
	et := newEconTools()
	deny := func(d *Deps) { d.Approve = func(context.Context, chat.ToolCall) Approval { return Approval{} } }
	r := econRig(t, et, deny)
	script(t, r, "go", [][]chat.ToolCall{{shellCall(1)}}, "done")
	got := toolContents(nextTurn(t, r, "next"))
	if len(got) != 1 || !strings.HasPrefix(got[0], resDenied) {
		t.Errorf("contents = %q, want the denial text unchanged", got)
	}
}

func TestEndTurn_FailedResultIsNotStubbed(t *testing.T) {
	et := newEconTools()
	et.fail["c1"] = true
	r := econRig(t, et)
	script(t, r, "go", [][]chat.ToolCall{{shellCall(1)}}, "done")
	if got := toolContents(nextTurn(t, r, "next")); len(got) != 1 || !strings.HasPrefix(got[0], "error: boom") {
		t.Errorf("contents = %q", got)
	}
}

func TestEndTurn_PreviousTurnsAreNotTouchedAgain(t *testing.T) {
	et := newEconTools()
	et.out["c1"] = big(200)
	r := econRig(t, et)
	script(t, r, "one", [][]chat.ToolCall{{readCall(1, "a.go")}}, "done")
	first := nextTurn(t, r, "two")
	second := nextTurn(t, r, "three")
	if !isPrefix(first.Messages, second.Messages[:len(first.Messages)]) {
		t.Error("a text-only Turn changed the earlier Messages")
	}
}

// ---------------------------------------------------------------------------
// Staleness
// ---------------------------------------------------------------------------

func staleTurn(t *testing.T, et *econTools, steps [][]chat.ToolCall) []string {
	t.Helper()
	for i := 1; i <= 9; i++ {
		id := fmt.Sprintf("c%d", i)
		if _, ok := et.out[id]; !ok {
			et.out[id] = big(200)
		}
	}
	r := econRig(t, et)
	script(t, r, "go", steps, "done")
	return toolContents(nextTurn(t, r, "next"))
}

func TestStale_ReadThenEdit(t *testing.T) {
	got := staleTurn(t, newEconTools(), [][]chat.ToolCall{{readCall(1, "a.go")}, {editCall(2, "a.go")}})
	if got[0] != "[read_file a.go: stale, the file changed later]" {
		t.Errorf("read result = %q", got[0])
	}
}

func TestStale_ReadTwiceLeavesTheOlderStale(t *testing.T) {
	got := staleTurn(t, newEconTools(), [][]chat.ToolCall{{readCall(1, "a.go")}, {readCall(2, "a.go")}})
	if !strings.Contains(got[0], "stale") || got[1] != "[read_file a.go: S]" {
		t.Errorf("contents = %q, want the older stale and the newer a plain Stub", got)
	}
}

func TestStale_NarrowerLaterReadDoesNotMakeTheWiderStale(t *testing.T) {
	narrow := toolCall(2, "read_file", `{"path":"a.go","offset":1,"limit":5}`)
	got := staleTurn(t, newEconTools(), [][]chat.ToolCall{{readCall(1, "a.go")}, {narrow}})
	if strings.Contains(got[0], "stale") {
		t.Errorf("a 200-line read became stale after a 5-line read: %q", got[0])
	}
}

func TestStale_WiderLaterReadMakesTheNarrowerStale(t *testing.T) {
	narrow := toolCall(1, "read_file", `{"path":"a.go","offset":10,"limit":5}`)
	got := staleTurn(t, newEconTools(), [][]chat.ToolCall{{narrow}, {readCall(2, "a.go")}})
	if !strings.Contains(got[0], "stale") {
		t.Errorf("contents = %q, want the narrow read stale", got)
	}
}

func TestStale_OtherFileIsNotStale(t *testing.T) {
	got := staleTurn(t, newEconTools(), [][]chat.ToolCall{{readCall(1, "a.go")}, {editCall(2, "b.go")}})
	if got[0] != "[read_file a.go: S]" {
		t.Errorf("read result = %q", got[0])
	}
}

func TestStale_FailedEditDoesNotStale(t *testing.T) {
	et := newEconTools()
	et.fail["c2"] = true
	if got := staleTurn(t, et, [][]chat.ToolCall{{readCall(1, "a.go")}, {editCall(2, "a.go")}}); got[0] != "[read_file a.go: S]" {
		t.Errorf("read result = %q", got[0])
	}
}

func TestStale_FailedLaterReadDoesNotStale(t *testing.T) {
	et := newEconTools()
	et.fail["c2"] = true
	if got := staleTurn(t, et, [][]chat.ToolCall{{readCall(1, "a.go")}, {readCall(2, "a.go")}}); got[0] != "[read_file a.go: S]" {
		t.Errorf("read result = %q", got[0])
	}
}

func TestStale_PathsCompareAfterClean(t *testing.T) {
	got := staleTurn(t, newEconTools(), [][]chat.ToolCall{{readCall(1, "./pkg//a.go")}, {editCall(2, "pkg/a.go")}})
	if !strings.Contains(got[0], "stale") {
		t.Errorf("read result = %q, want stale", got[0])
	}
}

func TestStale_CaseDiffersIsADifferentFile(t *testing.T) {
	got := staleTurn(t, newEconTools(), [][]chat.ToolCall{{readCall(1, "A.go")}, {editCall(2, "a.go")}})
	if strings.Contains(got[0], "stale") {
		t.Errorf("read result = %q, paths are compared as given", got[0])
	}
}

// ---------------------------------------------------------------------------
// Argument shrinking
// ---------------------------------------------------------------------------

func historyCall(r *rig, name string) chat.ToolCall {
	for _, m := range r.a.History() {
		for _, c := range m.ToolCalls {
			if c.Name == name {
				return c
			}
		}
	}
	return chat.ToolCall{}
}

func TestShrink_WriteFileArgsAreValidJSON(t *testing.T) {
	content := strings.Repeat("line of code <&> \"q\" \\ ☃\n", 60)
	args, _ := json.Marshal(map[string]string{"path": `d"ir/ф.go`, "content": content})
	r := econRig(t, newEconTools())
	script(t, r, "go", [][]chat.ToolCall{{toolCall(1, "write_file", string(args))}}, "done")

	got := historyCall(r, "write_file").Args
	if !json.Valid([]byte(got)) {
		t.Fatalf("args are not valid JSON: %s", got)
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(got), &m); err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("(omitted from history: 60 lines, %.1f KB; the file on disk has it)", float64(len(content))/1024)
	if m["path"] != `d"ir/ф.go` || m["content"] != want || len(m) != 2 {
		t.Errorf("args = %v, want path kept and content = %q", m, want)
	}
	if strings.Contains(got, `<`) {
		t.Errorf("HTML escaping in %s", got)
	}
}

func TestShrink_EditFileOldAndNew(t *testing.T) {
	args, _ := json.Marshal(map[string]string{"path": "a.go", "old": big(2000), "new": big(1500)})
	r := econRig(t, newEconTools())
	script(t, r, "go", [][]chat.ToolCall{{toolCall(1, "edit_file", string(args))}}, "done")
	var m map[string]string
	if err := json.Unmarshal([]byte(historyCall(r, "edit_file").Args), &m); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(m["old"], "(omitted from history: 1 lines, 2.0 KB") || !strings.HasPrefix(m["new"], "(omitted from history: 1 lines, 1.5 KB") {
		t.Errorf("args = %v", m)
	}
}

func TestShrink_SmallValuesStay(t *testing.T) {
	args, _ := json.Marshal(map[string]string{"path": "a.go", "content": big(1024)})
	r := econRig(t, newEconTools())
	script(t, r, "go", [][]chat.ToolCall{{toolCall(1, "write_file", string(args))}}, "done")
	if got := historyCall(r, "write_file").Args; got != string(args) {
		t.Errorf("args of exactly 1 KB changed: %s", got)
	}
}

func TestShrink_OtherToolsAndMalformedArgsAreUntouched(t *testing.T) {
	cmd, _ := json.Marshal(map[string]string{"command": big(5000)})
	r := econRig(t, newEconTools(), func(d *Deps) { d.Approve = nil })
	script(t, r, "go", [][]chat.ToolCall{{toolCall(1, "shell", string(cmd)), toolCall(2, "write_file", "not json")}}, "done")
	if got := historyCall(r, "shell").Args; got != string(cmd) {
		t.Errorf("shell args changed")
	}
	if got := historyCall(r, "write_file").Args; got != "{}" {
		t.Errorf("malformed args became %s, want the normalized {}", got)
	}
}

func TestShrink_NotInsideTheRunningTurn(t *testing.T) {
	args, _ := json.Marshal(map[string]string{"path": "a.go", "content": big(3000)})
	r := econRig(t, newEconTools())
	reqs := script(t, r, "go", [][]chat.ToolCall{{toolCall(1, "write_file", string(args))}, {readCall(2, "a.go")}}, "done")
	for _, req := range reqs {
		for _, m := range req.Messages {
			for _, c := range m.ToolCalls {
				if c.Name == "write_file" && c.Args != string(args) {
					t.Fatalf("args shrank inside the Turn: %s", c.Args)
				}
			}
		}
	}
}

func TestShrink_RequestAfterTheTurnCarriesShrunkArgs(t *testing.T) {
	args, _ := json.Marshal(map[string]string{"path": "a.go", "content": big(3000)})
	r := econRig(t, newEconTools())
	script(t, r, "go", [][]chat.ToolCall{{toolCall(1, "write_file", string(args))}}, "done")
	req := nextTurn(t, r, "next")
	for _, m := range req.Messages {
		for _, c := range m.ToolCalls {
			if strings.Contains(c.Args, big(100)) || !json.Valid([]byte(c.Args)) {
				t.Errorf("request carries args %.80s", c.Args)
			}
		}
	}
}

func TestShrink_HistoryCopyIsNotMutated(t *testing.T) {
	args, _ := json.Marshal(map[string]string{"path": "a.go", "content": big(3000)})
	r := econRig(t, newEconTools())
	r.a.Submit("go")
	r.p.next(t).toolStep("", toolCall(1, "write_file", string(args)))
	c := r.p.next(t)
	snap := r.a.History() // taken while the Turn runs
	c.reply("done")
	wait(t, r.a)
	for _, m := range snap {
		for _, tc := range m.ToolCalls {
			if tc.Args != string(args) {
				t.Errorf("a History() copy changed under the reader: %.60s", tc.Args)
			}
		}
	}
}

// TestShrink_DecodesWithTheRealToolSchemas feeds the shrunk args through the schemas of the
// real write_file and edit_file: only declared keys, all required keys present, strings.
func TestShrink_DecodesWithTheRealToolSchemas(t *testing.T) {
	set, err := tools.NewSet(tools.All(), nil)
	if err != nil {
		t.Fatal(err)
	}
	specs := map[string]chat.ToolSpec{}
	for _, s := range set.Specs() {
		specs[s.Name] = s
	}
	cases := map[string]map[string]string{
		"write_file": {"path": "a.go", "content": big(2000)},
		"edit_file":  {"path": "a.go", "old": big(1200), "new": big(1300)},
	}
	for name, in := range cases {
		raw, _ := json.Marshal(in)
		out, ok := shrinkCallArgs(chat.ToolCall{Name: name, Args: string(raw)})
		if !ok {
			t.Fatalf("%s: nothing shrank", name)
		}
		var schema struct {
			Properties map[string]struct{ Type string } `json:"properties"`
			Required   []string                         `json:"required"`
		}
		if err := json.Unmarshal(specs[name].Schema, &schema); err != nil {
			t.Fatal(err)
		}
		var args map[string]any
		if err := json.Unmarshal([]byte(out), &args); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for k, v := range args {
			p, known := schema.Properties[k]
			if _, isStr := v.(string); !known || (p.Type == "string" && !isStr) {
				t.Errorf("%s: argument %q (%T) does not fit the schema", name, k, v)
			}
		}
		for _, k := range schema.Required {
			if _, ok := args[k]; !ok {
				t.Errorf("%s: required %q missing in %s", name, k, out)
			}
		}
	}
}
