package session

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func withSeq(m chat.Message, seq int) chat.Message { m.Seq = seq; return m }
func user(s string) chat.Message                   { return chat.Message{Role: chat.RoleUser, Content: s} }
func assistant(s string) chat.Message              { return chat.Message{Role: chat.RoleAssistant, Content: s} }

func result(id, name, s string) chat.Message {
	return chat.Message{Role: chat.RoleTool, ToolCallID: id, Name: name, Content: s}
}

func calls(content string, cs ...chat.ToolCall) chat.Message {
	return chat.Message{Role: chat.RoleAssistant, Content: content, ToolCalls: cs}
}

func newWriter(t *testing.T) *Writer {
	t.Helper()
	w, err := Create(filepath.Join(t.TempDir(), "d"), Header{Cwd: "/p", Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	return w
}

func msg(t *testing.T, w *Writer, m chat.Message) int {
	t.Helper()
	seq, err := w.Message(m)
	if err != nil {
		t.Fatal(err)
	}
	return seq
}

func compact(t *testing.T, w *Writer, from, to int, summary chat.Message) int {
	t.Helper()
	seq, err := w.Compact(from, to, summary)
	if err != nil {
		t.Fatal(err)
	}
	return seq
}

func repair(t *testing.T, w *Writer, ms ...chat.Message) []int {
	t.Helper()
	seqs, err := w.Repair(ms)
	if err != nil {
		t.Fatal(err)
	}
	return seqs
}

func loadOK(t *testing.T, path string) Loaded {
	t.Helper()
	l, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

// tornCopy copies the file at path into a new file truncated to n bytes.
func tornCopy(t *testing.T, path string, n int) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "torn.jsonl")
	if err := os.WriteFile(out, b[:n], 0o600); err != nil {
		t.Fatal(err)
	}
	return out
}

func texts(es []Entry) []string {
	var out []string
	for _, e := range es {
		out = append(out, e.Msg.Content)
	}
	return out
}

// ---------------------------------------------------------------------------
// Dir
// ---------------------------------------------------------------------------

func TestDir_SlugAndHash(t *testing.T) {
	base := t.TempDir()
	a := Dir(base, "/a/b/proj")
	if filepath.Dir(a) != filepath.Join(base, "sessions") {
		t.Errorf("parent of %q", a)
	}
	name := filepath.Base(a)
	if !strings.HasPrefix(name, "proj-") || len(name) != len("proj-")+8 {
		t.Errorf("name %q", name)
	}
	if Dir(base, "/a/b/proj") != a {
		t.Error("not stable")
	}
	if Dir(base, "/c/d/proj") == a {
		t.Error("different cwd collides")
	}
	for _, cwd := range []string{`C:\Users\Me\My Proj`, "/", "", "/a/b/../b/proj/"} {
		n := filepath.Base(Dir(base, cwd))
		if strings.ContainsAny(n, `/\ :`) || n == "" {
			t.Errorf("cwd %q gives invalid name %q", cwd, n)
		}
	}
	if Dir(base, "/a/b/../b/proj/") != a {
		t.Error("path is not cleaned")
	}
}

func TestDir_FollowsSymlinks(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(t.TempDir(), "proj")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	if Dir(base, real) != Dir(base, link) {
		t.Error("symlink and target differ")
	}
}

func TestTracePath(t *testing.T) {
	got := TracePath(filepath.Join("d", "20260101-101010-abc123.jsonl"))
	want := filepath.Join("d", "traces", "20260101-101010-abc123.trace.jsonl")
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// Create and Writer
// ---------------------------------------------------------------------------

func TestCreate_FileModeAndHeader(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions", "x")
	started := time.Date(2026, 10, 27, 13, 4, 5, 0, time.UTC)
	w, err := Create(dir, Header{Cwd: "/p", Model: "m", Started: started})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	name := filepath.Base(w.Path())
	if !strings.HasPrefix(name, "20261027-130405-") || len(w.ID()) != 6 || !strings.HasSuffix(name, w.ID()+".jsonl") {
		t.Errorf("name %q id %q", name, w.ID())
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(w.Path()); fi.Mode().Perm() != 0o600 {
			t.Errorf("file mode %v", fi.Mode().Perm())
		}
		if fi, _ := os.Stat(dir); fi.Mode().Perm() != 0o700 {
			t.Errorf("dir mode %v", fi.Mode().Perm())
		}
	}
	l := loadOK(t, w.Path())
	h := l.Header
	if h.V != 1 || h.ID != w.ID() || h.Cwd != "/p" || h.Model != "m" || !h.Started.Equal(started) {
		t.Errorf("header %+v", h)
	}
	if lines := readLines(t, w.Path()); len(lines) != 1 || !strings.HasPrefix(lines[0], `{"v":1,`) {
		t.Errorf("lines %q", lines)
	}
}

func TestWriter_OneRecordPerLine(t *testing.T) {
	w := newWriter(t)
	in := []string{"multi\nline\n\"quoted\" <b>&", "Привет, мир", "tab\tand \\ backslash"}
	for _, s := range in {
		msg(t, w, user(s))
	}
	lines := readLines(t, w.Path())
	if len(lines) != 1+len(in) {
		t.Fatalf("%d lines", len(lines))
	}
	l := loadOK(t, w.Path())
	if !reflect.DeepEqual(texts(l.Entries), in) {
		t.Errorf("got %q", texts(l.Entries))
	}
}

func TestWriter_AppendOnly(t *testing.T) {
	w := newWriter(t)
	prev, _ := os.ReadFile(w.Path())
	steps := []func(){
		func() { msg(t, w, user("a")) },
		func() { msg(t, w, assistant("b")) },
		func() { w.Patch([]Patch{{Seq: 1, Stub: "s"}}) },
		func() { w.Clear() },
	}
	for i, step := range steps {
		step()
		cur, _ := os.ReadFile(w.Path())
		if !bytes.HasPrefix(cur, prev) || len(cur) <= len(prev) {
			t.Fatalf("step %d: file did not only grow", i)
		}
		prev = cur
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Message(user("late")); err == nil {
		t.Error("Message after Close succeeded")
	}
	if err := w.Clear(); err == nil {
		t.Error("Clear after Close succeeded")
	}
	if cur, _ := os.ReadFile(w.Path()); !bytes.Equal(cur, prev) {
		t.Error("write after Close changed the file")
	}
}

func TestWriter_ConcurrentSafe(t *testing.T) {
	w := newWriter(t)
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 50 {
				if _, err := w.Message(user(strings.Repeat("x", g*7+i))); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	l := loadOK(t, w.Path())
	if len(l.Entries) != 400 {
		t.Fatalf("%d entries", len(l.Entries))
	}
	seen := map[int]bool{}
	for _, e := range l.Entries {
		if seen[e.Seq] {
			t.Fatalf("duplicate seq %d", e.Seq)
		}
		seen[e.Seq] = true
	}
	if lines := readLines(t, w.Path()); len(lines) != 401 {
		t.Errorf("%d lines", len(lines))
	}
}

func TestWriter_SeqMonotonic(t *testing.T) {
	w := newWriter(t)
	a := msg(t, w, user("a"))
	b := msg(t, w, user("b"))
	c := compact(t, w, a, b, assistant("sum"))
	d := repair(t, w, assistant("r1"), assistant("r2"))
	if a != 1 || b != 2 || c != 3 || !reflect.DeepEqual(d, []int{4, 5}) {
		t.Errorf("seqs %d %d %d %v", a, b, c, d)
	}
}

func TestWriter_FlushSyncs(t *testing.T) {
	w := newWriter(t)
	msg(t, w, user("a"))
	if err := w.Flush(); err != nil {
		t.Errorf("Flush: %v", err)
	}
	// A failing write is returned, not swallowed.
	w.f.Close()
	if _, err := w.Message(user("b")); err == nil {
		t.Error("write to a closed fd returned nil")
	}
	if err := w.Flush(); err == nil {
		t.Error("Flush on a closed fd returned nil")
	}
}

func TestOpen_ContinuesSeqAndCutsTornTail(t *testing.T) {
	w := newWriter(t)
	msg(t, w, user("a"))
	msg(t, w, assistant("b"))
	w.Close()
	f, _ := os.OpenFile(w.Path(), os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString(`{"msg":{"seq":3,"role":"us`)
	f.Close()

	w2, err := Open(w.Path())
	if err != nil {
		t.Fatal(err)
	}
	if seq := msg(t, w2, user("c")); seq != 3 {
		t.Errorf("seq %d", seq)
	}
	w2.Close()
	l := loadOK(t, w.Path())
	if !reflect.DeepEqual(texts(l.Entries), []string{"a", "b", "c"}) {
		t.Errorf("got %q", texts(l.Entries))
	}
}

// ---------------------------------------------------------------------------
// Load
// ---------------------------------------------------------------------------

func TestLoad_RoundTrip(t *testing.T) {
	w := newWriter(t)
	msgs := []chat.Message{
		{Role: chat.RoleUser, Content: "do it", TurnStart: true},
		{Role: chat.RoleAssistant, Content: "partial", Truncated: true, Interrupted: true,
			ToolCalls: []chat.ToolCall{{ID: "c1", Name: "read_file", Args: `{"path":"a"}`}, {ID: "c2", Name: "shell", Args: `{"cmd":"ls"}`}}},
		{Role: chat.RoleTool, ToolCallID: "c1", Name: "read_file", Content: "file text\nline2", Brief: "2 lines", Label: "read a", Stub: "[read a]"},
		{Role: chat.RoleTool, ToolCallID: "c2", Name: "shell", Content: "error: x", Failed: true},
		{Role: chat.RoleAssistant, Content: "[work summary] x", Summary: true},
		{Role: chat.RoleAssistant, Content: "answer", Reasoning: "think\nmore"},
		{Role: chat.RoleUser, Content: "later", Injected: true},
	}
	for i := range msgs {
		msgs[i].Seq = i + 1 // the Writer keeps a Seq above everything written
		if seq := msg(t, w, msgs[i]); seq != i+1 {
			t.Fatalf("seq %d", seq)
		}
	}
	l := loadOK(t, w.Path())
	if !reflect.DeepEqual(l.Messages(), msgs) {
		t.Errorf("round trip differs:\n got %+v\nwant %+v", l.Messages(), msgs)
	}
	for i, e := range l.Entries {
		if e.Seq != i+1 {
			t.Errorf("entry %d seq %d", i, e.Seq)
		}
	}
	if len(l.Dangling) != 0 {
		t.Errorf("dangling %+v", l.Dangling)
	}
}

func TestLoad_TornLastLine(t *testing.T) {
	w := newWriter(t)
	msg(t, w, user("first"))
	msg(t, w, assistant("second"))
	before, _ := os.ReadFile(w.Path())
	msg(t, w, user("третье сообщение, ломается посередине"))
	after, _ := os.ReadFile(w.Path())
	for n := len(before); n < len(after); n++ { // every cut inside the last record, incl. without its newline
		l, err := Load(tornCopy(t, w.Path(), n))
		if err != nil {
			t.Fatalf("cut %d: %v", n, err)
		}
		if !reflect.DeepEqual(texts(l.Entries), []string{"first", "second"}) {
			t.Fatalf("cut %d: got %q", n, texts(l.Entries))
		}
	}
	l := loadOK(t, w.Path())
	if len(l.Entries) != 3 {
		t.Errorf("whole file: %d entries", len(l.Entries))
	}
}

func TestLoad_TornMiddleLineIsError(t *testing.T) {
	w := newWriter(t)
	msg(t, w, user("a"))
	msg(t, w, user("b"))
	b, _ := os.ReadFile(w.Path())
	lines := strings.Split(string(b), "\n")
	lines[1] = `{"msg": garbage`
	p := filepath.Join(t.TempDir(), "bad.jsonl")
	os.WriteFile(p, []byte(strings.Join(lines, "\n")), 0o600)
	_, err := Load(p)
	if err == nil || !strings.Contains(err.Error(), "line 2") || !strings.Contains(err.Error(), p) {
		t.Errorf("err = %v", err)
	}
}

func TestLoad_HeaderProblems(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"empty", "", "no header"},
		{"torn header", `{"v":1,"id":"ab`, "no header"},
		{"first line is a message", `{"msg":{"seq":1,"role":"user"}}` + "\n", "no header"},
		{"unknown version", `{"v":2,"id":"x"}` + "\n", "version 2"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "s.jsonl")
			os.WriteFile(p, []byte(c.body), 0o600)
			if _, err := Load(p); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want %q", err, c.want)
			}
		})
	}
}

func TestLoad_HeaderOnlyAndUnknownKeys(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	body := `{"v":1,"id":"abc123","cwd":"/p","model":"m","started":"2026-10-27T10:00:00Z"}` + "\n" +
		`{"future":{"x":1}}` + "\n"
	os.WriteFile(p, []byte(body), 0o600)
	l, err := Load(p)
	if err != nil || len(l.Entries) != 0 || l.Header.ID != "abc123" {
		t.Errorf("l=%+v err=%v", l, err)
	}
}

func TestLoad_ClearEmptiesHistory(t *testing.T) {
	w := newWriter(t)
	msg(t, w, user("a"))
	msg(t, w, user("b"))
	w.Clear()
	s := msg(t, w, user("c"))
	l := loadOK(t, w.Path())
	if !reflect.DeepEqual(texts(l.Entries), []string{"c"}) || s != 3 || l.Entries[0].Seq != 3 {
		t.Errorf("entries %+v seq %d", l.Entries, s)
	}
}

func TestLoad_PatchAppliesStubAndArgs(t *testing.T) {
	w := newWriter(t)
	msg(t, w, user("go"))
	msg(t, w, calls("", chat.ToolCall{ID: "c1", Name: "write_file", Args: `{"path":"a","content":"BIG"}`},
		chat.ToolCall{ID: "c2", Name: "shell", Args: `{"cmd":"ls"}`}))
	msg(t, w, result("c1", "write_file", "ok"))
	msg(t, w, result("c2", "shell", "huge output"))
	err := w.Patch([]Patch{
		{Seq: 2, Args: map[string]string{"c1": `{"path":"a","content":"[3 bytes]"}`}},
		{Seq: 4, Stub: "shell: 1 line"},
	})
	if err != nil {
		t.Fatal(err)
	}
	l := loadOK(t, w.Path())
	if got := l.Entries[1].Msg.ToolCalls[0].Args; got != `{"path":"a","content":"[3 bytes]"}` {
		t.Errorf("args %q", got)
	}
	if got := l.Entries[1].Msg.ToolCalls[1].Args; got != `{"cmd":"ls"}` {
		t.Errorf("untouched call changed: %q", got)
	}
	if l.Entries[3].Msg.Stub != "shell: 1 line" || l.Entries[3].Msg.Content != "huge output" || l.Entries[2].Msg.Stub != "" {
		t.Errorf("stubs %+v", l.Entries)
	}
}

func TestLoad_PatchUnknownSeqIsError(t *testing.T) {
	w := newWriter(t)
	msg(t, w, user("a"))
	w.Patch([]Patch{{Seq: 9, Stub: "x"}})
	if _, err := Load(w.Path()); err == nil || !strings.Contains(err.Error(), "seq 9") {
		t.Errorf("err = %v", err)
	}
}

func TestLoad_CompactReplacesRange(t *testing.T) {
	w := newWriter(t)
	for _, s := range []string{"m0", "m1", "m2", "m3", "m4", "m5"} {
		msg(t, w, user(s))
	}
	sum := compact(t, w, 2, 4, assistant("SUM"))
	l := loadOK(t, w.Path())
	if !reflect.DeepEqual(texts(l.Entries), []string{"m0", "SUM", "m4", "m5"}) {
		t.Errorf("got %q", texts(l.Entries))
	}
	if l.Entries[1].Seq != sum || l.Entries[2].Seq != 5 {
		t.Errorf("seqs %+v", l.Entries)
	}
}

func TestLoad_CompactAddressesBySeqNotLine(t *testing.T) {
	w := newWriter(t)
	for _, s := range []string{"a", "b", "c"} {
		msg(t, w, user(s))
	}
	w.Clear() // history empty; later messages get seq 4.. while their index is 0..
	msg(t, w, user("d"))
	msg(t, w, user("e"))
	msg(t, w, user("f"))
	compact(t, w, 5, 6, assistant("S"))
	l := loadOK(t, w.Path())
	if !reflect.DeepEqual(texts(l.Entries), []string{"d", "S"}) {
		t.Errorf("got %q", texts(l.Entries))
	}
	// A second compaction may span a summary and its neighbours.
	compact(t, w, 4, l.Entries[1].Seq, assistant("S2"))
	l = loadOK(t, w.Path())
	if !reflect.DeepEqual(texts(l.Entries), []string{"S2"}) {
		t.Errorf("got %q", texts(l.Entries))
	}
}

func TestLoad_CompactBadRange(t *testing.T) {
	cases := []struct {
		name     string
		from, to int
	}{{"from after to", 3, 2}, {"to missing", 1, 9}, {"from missing", 9, 9}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWriter(t)
			for _, s := range []string{"a", "b", "c"} {
				msg(t, w, user(s))
			}
			w.Compact(c.from, c.to, assistant("S"))
			if _, err := Load(w.Path()); err == nil || !strings.Contains(err.Error(), "compact") {
				t.Errorf("err = %v", err)
			}
		})
	}
}

func TestLoad_DanglingCalls(t *testing.T) {
	w := newWriter(t)
	msg(t, w, user("go"))
	msg(t, w, calls("", chat.ToolCall{ID: "c1", Name: "a"}, chat.ToolCall{ID: "c2", Name: "b"}))
	msg(t, w, result("c1", "a", "ok"))
	l := loadOK(t, w.Path())
	if len(l.Dangling) != 1 || l.Dangling[0].Call.ID != "c2" || l.Dangling[0].Seq != 2 {
		t.Fatalf("dangling %+v", l.Dangling)
	}
	if len(l.Entries) != 3 { // nothing invented
		t.Errorf("%d entries", len(l.Entries))
	}
	// A repair record resolves it and is part of the history.
	repair(t, w, result("c2", "b", "cancelled by the user"))
	l = loadOK(t, w.Path())
	if len(l.Dangling) != 0 || len(l.Entries) != 4 || l.Entries[3].Msg.ToolCallID != "c2" {
		t.Errorf("after repair: %+v", l)
	}
}

// ---------------------------------------------------------------------------
// Round trip of a scripted session
// ---------------------------------------------------------------------------

func TestRoundTrip_ScriptedSession(t *testing.T) {
	w := newWriter(t)
	var want []Entry
	add := func(m chat.Message) int {
		seq := msg(t, w, m)
		m.Seq = seq
		want = append(want, Entry{Seq: seq, Msg: m})
		return seq
	}
	add(user("first question"))
	add(calls("", chat.ToolCall{ID: "c1", Name: "write_file", Args: `{"content":"BIG"}`}))
	add(result("c1", "write_file", "wrote"))
	add(assistant("answer one"))
	// stub + arg shrink
	w.Patch([]Patch{{Seq: 2, Args: map[string]string{"c1": `{"content":"[3 bytes]"}`}}, {Seq: 3, Stub: "wrote a"}})
	want[1].Msg.ToolCalls[0].Args = `{"content":"[3 bytes]"}`
	want[2].Msg.Stub = "wrote a"
	// compact seq 2..3
	sum := compact(t, w, 2, 3, assistant("work summary"))
	want = append([]Entry{want[0], {Seq: sum, Msg: withSeq(assistant("work summary"), sum)}}, want[3:]...)
	add(user("second question"))
	// clear, then new life
	w.Clear()
	want = nil
	add(user("after clear"))
	add(calls("", chat.ToolCall{ID: "x", Name: "t"}))
	rep := repair(t, w, result("x", "t", "cancelled by the user"))
	want = append(want, Entry{Seq: rep[0], Msg: withSeq(result("x", "t", "cancelled by the user"), rep[0])})

	l := loadOK(t, w.Path())
	if !reflect.DeepEqual(l.Entries, want) {
		t.Errorf("replay differs:\n got %+v\nwant %+v", l.Entries, want)
	}
}

// ---------------------------------------------------------------------------
// List and Prune
// ---------------------------------------------------------------------------

func mkSession(t *testing.T, dir string, started time.Time, first string, n int) *Writer {
	t.Helper()
	w, err := Create(dir, Header{Model: "m-" + first, Started: started})
	if err != nil {
		t.Fatal(err)
	}
	for i := range n {
		if i == 0 {
			msg(t, w, user(first))
		} else {
			msg(t, w, assistant("x"))
		}
	}
	w.Close()
	return w
}

func TestList_NewestFirstAndTitle(t *testing.T) {
	dir := t.TempDir()
	t0 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	mkSession(t, dir, t0, "oldest", 1)
	long := strings.Repeat("я", 100) + "\nnext line"
	mkSession(t, dir, t0.Add(2*time.Hour), long, 3)
	mkSession(t, dir, t0.Add(time.Hour), "middle", 2)
	bad := filepath.Join(dir, "20260101-000000-bad000.jsonl")
	os.WriteFile(bad, []byte("not json\nmore\n"), 0o600)
	os.MkdirAll(filepath.Join(dir, "traces"), 0o700)
	os.WriteFile(filepath.Join(dir, "traces", "x.trace.jsonl"), []byte("{}\n"), 0o600)

	got, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("%d infos", len(got))
	}
	if got[0].Title != strings.Repeat("я", 60) || got[0].Messages != 3 || got[0].Model != "m-"+long {
		t.Errorf("first %+v", got[0])
	}
	if got[1].Title != "middle" || got[2].Title != "oldest" {
		t.Errorf("order %q %q", got[1].Title, got[2].Title)
	}
	if got[3].Path != bad || got[3].Err == "" {
		t.Errorf("broken file not marked: %+v", got[3])
	}
	for _, in := range got[:3] {
		if len(in.ID) != 6 || in.Err != "" {
			t.Errorf("info %+v", in)
		}
	}
}

func TestList_EmptyDir(t *testing.T) {
	got, err := List(filepath.Join(t.TempDir(), "missing"))
	if err != nil || len(got) != 0 {
		t.Errorf("got %v err %v", got, err)
	}
}

func TestPrune_KeepsNewestAndRemovesTraces(t *testing.T) {
	dir := t.TempDir()
	t0 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	var ws []*Writer
	for i := range 5 {
		w := mkSession(t, dir, t0.Add(time.Duration(i)*time.Minute), "s", 1)
		tp := TracePath(w.Path())
		os.MkdirAll(filepath.Dir(tp), 0o700)
		os.WriteFile(tp, []byte("{}\n"), 0o600)
		old := time.Now().Add(-48 * time.Hour)
		os.Chtimes(w.Path(), old, old)
		ws = append(ws, w)
	}
	if err := Prune(dir, 3); err != nil {
		t.Fatal(err)
	}
	for i, w := range ws {
		_, e1 := os.Stat(w.Path())
		_, e2 := os.Stat(TracePath(w.Path()))
		gone := i < 2
		if (e1 != nil) != gone || (e2 != nil) != gone {
			t.Errorf("file %d: session err=%v trace err=%v, want gone=%v", i, e1, e2, gone)
		}
	}
}

func TestPrune_GuardsKeepAndRecentFiles(t *testing.T) {
	dir := t.TempDir()
	t0 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	for i := range 4 {
		w := mkSession(t, dir, t0.Add(time.Duration(i)*time.Minute), "s", 1)
		if i < 2 {
			old := time.Now().Add(-48 * time.Hour)
			os.Chtimes(w.Path(), old, old)
		}
	}
	for _, keep := range []int{0, -1} {
		Prune(dir, keep)
		if got, _ := List(dir); len(got) != 4 {
			t.Fatalf("keep %d deleted files", keep)
		}
	}
	// keep 1 would delete 3, but the two newest were modified just now.
	Prune(dir, 1)
	if got, _ := List(dir); len(got) != 2 {
		t.Errorf("%d left, want 2 (old ones removed, recent ones spared)", len(got))
	}
	if err := Prune(filepath.Join(dir, "none"), 1); err != nil {
		t.Errorf("missing dir: %v", err)
	}
}
