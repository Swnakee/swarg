//go:build !windows

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"swarg/internal/chat"
	"swarg/internal/config"
	"swarg/internal/llm/llmtest"
	"swarg/internal/session"
)

// ---------------------------------------------------------------------------
// The real main() re-executed (see main_unix_test.go) in plain mode against a fake model,
// with HOME and XDG_CONFIG_HOME pointing into a temp dir so config.Dir() is the same
// in the test and in the child.
// ---------------------------------------------------------------------------

// env is one temp user: config dir, work dir.
type env struct {
	home, work string
}

func newEnv(t *testing.T) env {
	t.Helper()
	e := env{home: t.TempDir(), work: t.TempDir()}
	t.Setenv("HOME", e.home)
	t.Setenv("XDG_CONFIG_HOME", e.home)
	return e
}

// fixedNow is a start time old enough for Prune (it spares files modified in the last hour).
func (e env) fixedNow() time.Time { return time.Now().Add(-48 * time.Hour) }

// sdir is the SessionFile directory of the work dir.
func (e env) sdir(t *testing.T) string {
	t.Helper()
	base, err := config.Dir()
	if err != nil {
		t.Fatal(err)
	}
	return session.Dir(base, e.work)
}

// swarg runs the helper process with stdin and args; srv may be nil for commands that must not ask the model.
func (e env) swarg(t *testing.T, srv *llmtest.Server, stdin string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	if srv != nil {
		args = append(args, "--base-url", srv.URL, "--model", "m")
	}
	cmd := exec.Command(os.Args[0], args...)
	cmd.Dir = e.work
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Env = append(os.Environ(), helperEnv+"=1", "HOME="+e.home, "XDG_CONFIG_HOME="+e.home, "GORACE=atexit_sleep_ms=0")
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	code = exitCode(t, cmd.Run())
	return so.String(), se.String(), code
}

// files lists the names in dir matching the glob, failing the test on a glob error.
func files(t *testing.T, dir, glob string) []string {
	t.Helper()
	m, err := filepath.Glob(filepath.Join(dir, glob))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// say returns a one-chunk assistant reply.
func say(s string) llmtest.Reply { return llmtest.Reply{Chunks: []string{s}} }

// roleContent summarizes the messages of a recorded request as "role:content".
func roleContent(req map[string]any) []string {
	var out []string
	for _, m := range req["messages"].([]any) {
		mm := m.(map[string]any)
		c, _ := mm["content"].(string)
		out = append(out, mm["role"].(string)+":"+c)
	}
	return out
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestPlain_SavesSession(t *testing.T) {
	e := newEnv(t)
	srv := llmtest.New(t, say("hello"))
	out, errOut, code := e.swarg(t, srv, "go", "--yes")
	if code != 0 || !strings.Contains(out, "hello") {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	if !strings.Contains(errOut, "session saved: swarg --continue") {
		t.Errorf("stderr %q lacks the saved hint", errOut)
	}
	fs := files(t, e.sdir(t), "*.jsonl")
	if len(fs) != 1 {
		t.Fatalf("session files = %v, want 1", fs)
	}
	l, err := session.Load(fs[0])
	if err != nil {
		t.Fatal(err)
	}
	msgs := l.Messages()
	if len(msgs) != 2 || msgs[0].Role != chat.RoleUser || msgs[0].Content != "go" ||
		msgs[1].Role != chat.RoleAssistant || msgs[1].Content != "hello" {
		t.Fatalf("replayed messages = %+v", msgs)
	}
	tr := session.TracePath(fs[0])
	if fi, err := os.Stat(tr); err != nil || fi.Size() == 0 || fi.Mode().Perm() != 0o600 {
		t.Errorf("trace %s: %v, %v", tr, fi, err)
	}
	if fi, err := os.Stat(fs[0]); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("session file mode: %v, %v", fi, err)
	}
}

func TestMain_ContinueResumesNewestOfThisProject(t *testing.T) {
	e := newEnv(t)
	srv := llmtest.New(t, say("hello"), say("again-answer"))
	if _, errOut, code := e.swarg(t, srv, "go", "--yes"); code != 0 {
		t.Fatalf("first run: exit %d, %q", code, errOut)
	}
	// a session of ANOTHER project must be ignored
	base, _ := config.Dir()
	seed(t, session.Dir(base, t.TempDir()), "other1", "foreign", e.fixedNow())

	out, errOut, code := e.swarg(t, srv, "again", "--yes", "--continue")
	if code != 0 || !strings.Contains(out, "again-answer") {
		t.Fatalf("second run: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	reqs := srv.Requests()
	if len(reqs) != 2 {
		t.Fatalf("requests = %d, want 2", len(reqs))
	}
	got := roleContent(reqs[1])
	for _, want := range []string{"user:go", "assistant:hello", "user:again"} {
		if !contains(got, want) {
			t.Errorf("second request %v lacks %q", got, want)
		}
	}
	for _, m := range got {
		if m == "user:foreign" {
			t.Error("resumed a session of another project")
		}
	}
	fs := files(t, e.sdir(t), "*.jsonl")
	if len(fs) != 1 {
		t.Fatalf("session files = %v, want the one file continued", fs)
	}
	l, err := session.Load(fs[0])
	if err != nil {
		t.Fatal(err)
	}
	if n := len(l.Messages()); n != 4 { // go, hello, again, again-answer: nothing stored twice
		t.Errorf("messages in the file = %d, want 4: %+v", n, l.Messages())
	}
}

func TestMain_ContinueWithoutSessionStartsFresh(t *testing.T) {
	e := newEnv(t)
	srv := llmtest.New(t, say("hi"))
	_, errOut, code := e.swarg(t, srv, "go", "--yes", "--continue")
	if code != 0 || !strings.Contains(errOut, "no previous session") {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	if len(files(t, e.sdir(t), "*.jsonl")) != 1 {
		t.Error("a new session should have been saved")
	}
}

func TestMain_ResumeByPrefixAndErrors(t *testing.T) {
	e := newEnv(t)
	sd := e.sdir(t)
	now := e.fixedNow()
	seed(t, sd, "abc111", "old question", now)
	seed(t, sd, "abc222", "other question", now.Add(time.Second))
	srv := llmtest.New(t, say("ok"))

	_, errOut, code := e.swarg(t, srv, "x", "--yes", "--resume", "abc")
	if code != 1 || !strings.Contains(errOut, "ambiguous") || !strings.Contains(errOut, "abc111") {
		t.Errorf("ambiguous: exit %d, stderr %q", code, errOut)
	}
	_, errOut, code = e.swarg(t, srv, "x", "--yes", "--resume", "zzz")
	if code != 1 || !strings.Contains(errOut, "abc222") {
		t.Errorf("unknown: exit %d, stderr %q", code, errOut)
	}
	_, _, code = e.swarg(t, srv, "x", "--yes", "--resume")
	if code != 2 {
		t.Errorf("--resume without id: exit %d, want 2", code)
	}
	if n := len(srv.Requests()); n != 0 {
		t.Errorf("errors must not reach the model, got %d requests", n)
	}

	if _, errOut, code = e.swarg(t, srv, "next", "--yes", "--resume", "abc1"); code != 0 {
		t.Fatalf("resume abc1: exit %d, %q", code, errOut)
	}
	if got := roleContent(srv.Requests()[0]); !contains(got, "user:old question") || !contains(got, "user:next") {
		t.Errorf("request %v lacks the resumed history", got)
	}
}

func TestMain_ResumeCorruptSession(t *testing.T) {
	e := newEnv(t)
	sd := e.sdir(t)
	seed(t, sd, "bad000", "q", e.fixedNow())
	p := files(t, sd, "*.jsonl")[0]
	data, _ := os.ReadFile(p)
	lines := strings.SplitN(string(data), "\n", 2)
	if err := os.WriteFile(p, []byte(lines[0]+"\nnot json\n"+lines[1]), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := llmtest.New(t, say("ok"))
	_, errOut, code := e.swarg(t, srv, "x", "--yes", "--resume", "bad000")
	if code != 1 || !strings.Contains(errOut, filepath.Base(p)) {
		t.Errorf("exit %d, stderr %q, want a non-zero exit naming the file", code, errOut)
	}
}

func TestMain_NoSaveWritesNothing(t *testing.T) {
	e := newEnv(t)
	srv := llmtest.New(t, say("hello"))
	out, errOut, code := e.swarg(t, srv, "go", "--yes", "--no-save")
	if code != 0 || !strings.Contains(out, "hello") {
		t.Fatalf("exit %d, %q, %q", code, out, errOut)
	}
	if strings.Contains(errOut, "session saved") {
		t.Errorf("stderr %q must not announce a saved session", errOut)
	}
	if _, err := os.Stat(e.sdir(t)); !os.IsNotExist(err) {
		t.Errorf("session dir exists with --no-save: %v", err)
	}
}

func TestMain_ConfigSaveFalseWritesNothing(t *testing.T) {
	e := newEnv(t)
	cfgDir, _ := config.Dir()
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), []byte(`{"session":{"save":false}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := llmtest.New(t, say("hello"))
	if _, errOut, code := e.swarg(t, srv, "go", "--yes"); code != 0 {
		t.Fatalf("exit %d, %q", code, errOut)
	}
	if _, err := os.Stat(e.sdir(t)); !os.IsNotExist(err) {
		t.Errorf("session dir exists with session.save false: %v", err)
	}
}

func TestMain_TraceOffWritesNoTrace(t *testing.T) {
	e := newEnv(t)
	srv := llmtest.New(t, say("a"), say("b"))
	if _, errOut, code := e.swarg(t, srv, "go", "--yes", "--trace-off"); code != 0 {
		t.Fatalf("exit %d, %q", code, errOut)
	}
	if len(files(t, e.sdir(t), "*.jsonl")) != 1 {
		t.Error("the session must still be saved")
	}
	if tr := files(t, filepath.Join(e.sdir(t), "traces"), "*"); len(tr) != 0 {
		t.Errorf("traces = %v, want none", tr)
	}
	_, errOut, code := e.swarg(t, nil, "", "trace")
	if code != 1 || !strings.Contains(errOut, "no trace") {
		t.Errorf("trace of a session without one: exit %d, %q", code, errOut)
	}
}

func TestMain_EmptyRunLeavesNoFile(t *testing.T) {
	e := newEnv(t)
	srv := llmtest.New(t)
	out, _, code := e.swarg(t, srv, "", "--yes")
	if code != 0 || !strings.Contains(out, "usage") {
		t.Fatalf("exit %d, %q", code, out)
	}
	if fs := files(t, e.sdir(t), "*"); len(fs) != 0 {
		for _, f := range fs {
			if !strings.HasSuffix(f, "traces") {
				t.Errorf("leftover %s", f)
			}
		}
	}
	if tr := files(t, filepath.Join(e.sdir(t), "traces"), "*"); len(tr) != 0 {
		t.Errorf("leftover traces %v", tr)
	}
}

func TestMain_PruneKeepsConfiguredCount(t *testing.T) {
	e := newEnv(t)
	cfgDir, _ := config.Dir()
	os.MkdirAll(cfgDir, 0o700)
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), []byte(`{"session":{"keep":2}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	sd := e.sdir(t)
	for i, id := range []string{"aaa001", "aaa002", "aaa003", "aaa004"} {
		seed(t, sd, id, "q", e.fixedNow().Add(time.Duration(i)*time.Minute))
	}
	// pruning spares files modified in the last hour: age them all
	for _, p := range files(t, sd, "*.jsonl") {
		old := e.fixedNow()
		os.Chtimes(p, old, old)
	}
	srv := llmtest.New(t, say("ok"))
	if _, errOut, code := e.swarg(t, srv, "go", "--yes"); code != 0 {
		t.Fatalf("exit %d, %q", code, errOut)
	}
	// 4 old, keep 2 -> the 2 oldest go; the new session joins the 2 newest old ones
	if n := len(files(t, sd, "*.jsonl")); n != 3 {
		t.Errorf("session files = %d, want 3 (2 kept + the new one)", n)
	}
}

func TestMain_SessionsListsProject(t *testing.T) {
	e := newEnv(t)
	srv := llmtest.New(t, say("hello"))
	if _, errOut, code := e.swarg(t, srv, "my first question", "--yes"); code != 0 {
		t.Fatalf("exit %d, %q", code, errOut)
	}
	out, errOut, code := e.swarg(t, nil, "", "--sessions")
	if code != 0 {
		t.Fatalf("exit %d, %q", code, errOut)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 1 {
		t.Fatalf("--sessions output %q, want one line", out)
	}
	f := strings.Fields(lines[0])
	// id, date, time, model, messages, title...
	if len(f) < 7 || len(f[0]) != 6 || f[3] != "m" || f[4] != "2" || strings.Join(f[5:], " ") != "my first question" {
		t.Errorf("line %q does not match `id  started  model  messages  title`", lines[0])
	}

	empty := newEnv(t)
	out, _, code = empty.swarg(t, nil, "", "--sessions")
	if code != 0 || !strings.Contains(out, "no saved sessions") {
		t.Errorf("empty project: exit %d, %q", code, out)
	}
}

func TestTraceCommand_PrintsTimeline(t *testing.T) {
	e := newEnv(t)
	srv := llmtest.New(t, say("hello"))
	if _, errOut, code := e.swarg(t, srv, "go", "--yes"); code != 0 {
		t.Fatalf("exit %d, %q", code, errOut)
	}
	out, errOut, code := e.swarg(t, nil, "", "trace")
	if code != 0 || !strings.Contains(out, "request") {
		t.Fatalf("trace: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	infos, _ := session.List(e.sdir(t))
	out2, _, code := e.swarg(t, nil, "", "trace", infos[0].ID[:3])
	if code != 0 || out2 != out {
		t.Errorf("trace by prefix: exit %d, differs from the newest-session output", code)
	}
	if _, errOut, code = e.swarg(t, nil, "", "trace", "zzzzzz"); code != 1 || errOut == "" {
		t.Errorf("unknown id: exit %d, %q", code, errOut)
	}
	if _, _, code = e.swarg(t, nil, "", "trace", "a", "b"); code != 2 {
		t.Errorf("usage error: exit %d, want 2", code)
	}
}

func TestMain_PickWithoutTerminalListsAndExits2(t *testing.T) {
	e := newEnv(t)
	seed(t, e.sdir(t), "abc111", "old question", e.fixedNow())
	srv := llmtest.New(t, say("ok"))
	for _, flag := range []string{"-r", "--resume"} {
		out, errOut, code := e.swarg(t, srv, "x", "--yes", flag)
		if code != 2 || !strings.Contains(out, "abc111") || !strings.Contains(errOut, "swarg -r <id>") {
			t.Errorf("%s: exit %d, stdout %q, stderr %q", flag, code, out, errOut)
		}
	}
	if n := len(srv.Requests()); n != 0 {
		t.Errorf("pick must not reach the model, got %d requests", n)
	}
	empty := newEnv(t)
	out, _, code := empty.swarg(t, nil, "", "-r")
	if code != 0 || !strings.Contains(out, "no saved sessions in this folder") {
		t.Errorf("empty: exit %d, %q", code, out)
	}
	// -r ID and -c still work through the short aliases
	if _, errOut, code := e.swarg(t, srv, "next", "--yes", "-r", "abc1"); code != 0 {
		t.Errorf("-r abc1: exit %d, %q", code, errOut)
	}
}

// The plain answer and the saved hint share one terminal (2>&1): the hint must start its own line.
func TestPlain_SavedHintOnItsOwnLine(t *testing.T) {
	e := newEnv(t)
	srv := llmtest.New(t, say("the answer"))
	cmd := exec.Command(os.Args[0], "--yes", "--base-url", srv.URL, "--model", "m")
	cmd.Dir = e.work
	cmd.Stdin = strings.NewReader("go")
	cmd.Env = append(os.Environ(), helperEnv+"=1", "HOME="+e.home, "XDG_CONFIG_HOME="+e.home, "GORACE=atexit_sleep_ms=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if !strings.Contains(string(out), "the answer\nsession saved: swarg --continue\n") {
		t.Errorf("combined output %q: the hint is not on its own line", out)
	}
}

func TestPlain_SavedHintVariants(t *testing.T) {
	tool := llmtest.Reply{ToolCalls: []chat.ToolCall{{ID: "c1", Name: "shell", Args: `{"command":"echo hi"}`}}}
	cases := map[string][]llmtest.Reply{
		"plain":       {say("the answer")},
		"trailing nl": {say("the answer\n")},
		"split":       {{Chunks: []string{"the ", "answer", "."}}},
		"after tool":  {tool, say("done.")},
		"spaces":      {say("done.  ")},
	}
	for name, replies := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			srv := llmtest.New(t, replies...)
			cmd := exec.Command(os.Args[0], "--yes", "--base-url", srv.URL, "--model", "m")
			cmd.Dir = e.work
			cmd.Stdin = strings.NewReader("go")
			cmd.Env = append(os.Environ(), helperEnv+"=1", "HOME="+e.home, "XDG_CONFIG_HOME="+e.home, "GORACE=atexit_sleep_ms=0")
			out, _ := cmd.CombinedOutput()
			if !strings.Contains(string(out), "\nsession saved: swarg --continue\n") {
				t.Errorf("%q", out)
			}
		})
	}
}

// R-30: a resumed Session continues with its last chosen model, over SWARG_MODEL; --model wins
// and is recorded.
func TestMain_ContinueKeepsTheLastChosenModel(t *testing.T) {
	e := newEnv(t)
	srv := llmtest.New(t, say("one"), say("two"), say("three"))
	var lastErr string
	run := func(stdin string, args ...string) {
		t.Helper()
		args = append(args, "--yes", "--base-url", srv.URL)
		_, errOut, code := e.swarg(t, nil, stdin, args...)
		if code != 0 {
			t.Fatalf("%v: exit %d, %q", args, code, errOut)
		}
		lastErr = errOut
	}
	run("go", "--model", "first")
	fs := files(t, e.sdir(t), "*.jsonl")
	if len(fs) != 1 {
		t.Fatalf("session files %v", fs)
	}
	f, err := os.OpenFile(fs[0], os.O_APPEND|os.O_WRONLY, 0o600) // a choice made in the app
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"model":"chosen"}` + "\n")
	f.Close()

	t.Setenv("SWARG_MODEL", "from-env")
	run("again", "--continue")
	if !strings.Contains(lastErr, "continuing with the session's model chosen") {
		t.Errorf("no hint that the session's own model is used: %q", lastErr)
	}
	run("more", "--continue", "--model", "flagged")
	if strings.Contains(lastErr, "continuing with") {
		t.Errorf("a hint although --model was given: %q", lastErr)
	}

	var got []string
	for _, r := range srv.Requests() {
		got = append(got, r["model"].(string))
	}
	if want := []string{"first", "chosen", "flagged"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("request models %q, want %q", got, want)
	}
	l, err := session.Load(fs[0])
	if err != nil {
		t.Fatal(err)
	}
	if l.Model() != "flagged" {
		t.Errorf("the file's last model %q, want the --model choice recorded", l.Model())
	}
}

// ---------------------------------------------------------------------------
// Sources at start (R-101, R-104)
// ---------------------------------------------------------------------------

// A resumed session continues on its last Source (recorded by a /server switch) with that
// Source's model; the hint names it.
func TestMain_ContinueRestoresTheSessionsProvider(t *testing.T) {
	e := newEnv(t)
	first := llmtest.New(t, say("one"))
	second := llmtest.New(t, say("two"))
	cfgPath := filepath.Join(e.home, "config.json")
	body := `{"providers":{"second":{"type":"openai","base_url":"` + second.URL + `","model":"m2","context_limit":1000,"max_tokens":100}}}`
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, errOut, code := e.swarg(t, nil, "go", "--yes", "--config", cfgPath, "--base-url", first.URL, "--model", "m1"); code != 0 {
		t.Fatalf("first run: exit %d, %q", code, errOut)
	}
	fs := files(t, e.sdir(t), "*.jsonl")
	if len(fs) != 1 {
		t.Fatalf("session files %v", fs)
	}
	f, err := os.OpenFile(fs[0], os.O_APPEND|os.O_WRONLY, 0o600) // a /server switch made in the app
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"provider":"second"}` + "\n" + `{"model":"m2"}` + "\n") // SetSource writes both lines
	f.Close()
	_, errOut, code := e.swarg(t, nil, "again", "--yes", "--config", cfgPath, "--continue")
	if code != 0 || !strings.Contains(errOut, "continuing on the session's source second") {
		t.Fatalf("exit %d, %q", code, errOut)
	}
	if reqs := second.Requests(); len(reqs) != 1 || reqs[0]["model"] != "m2" {
		t.Errorf("the second source got %v, want one request on m2", reqs)
	}
	if len(first.Requests()) != 1 {
		t.Errorf("the first source got %d requests after the switch, want none more", len(first.Requests())-1)
	}
	l, err := session.Load(fs[0])
	if err != nil {
		t.Fatal(err)
	}
	if l.Provider() != "second" || l.Header.Provider != "local" {
		t.Errorf("file provider %q header %q", l.Provider(), l.Header.Provider)
	}
}

// The active Source without its key is a start error that names the variable (R-104).
func TestMain_ActiveProviderWithoutKeyIsAStartError(t *testing.T) {
	e := newEnv(t)
	cfgPath := filepath.Join(e.home, "config.json")
	if err := os.WriteFile(cfgPath, []byte(`{"provider":"openrouter"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	os.Unsetenv("OPENROUTER_API_KEY")
	_, errOut, code := e.swarg(t, nil, "go", "--yes", "--config", cfgPath)
	if code != 2 || !strings.Contains(errOut, `provider "openrouter": no key: set OPENROUTER_API_KEY`) {
		t.Fatalf("exit %d, %q", code, errOut)
	}
}
