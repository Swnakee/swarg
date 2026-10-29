package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"swarg/internal/agent"
	"swarg/internal/chat"
	"swarg/internal/llm/llmtest"
	"swarg/internal/llm/openai"
	"swarg/internal/tools"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// plainDeps wires the real openai client and agent to a fake server, without tools.
func plainDeps(t *testing.T, replies ...llmtest.Reply) (Deps, *llmtest.Server) {
	t.Helper()
	return plainRig(t, nil, false, replies...)
}

// plainRig wires the real openai client, agent and approver to a fake server.
// tr may be nil (no tools); yes is the plain-mode approval.
func plainRig(t *testing.T, tr agent.ToolRunner, yes bool, replies ...llmtest.Reply) (Deps, *llmtest.Server) {
	t.Helper()
	srv := llmtest.New(t, replies...)
	events := make(chan agent.Event, 256)
	approver := NewApprover(yes)
	a := agent.New(agent.Deps{
		Provider:     openai.New(srv.URL, "", nil),
		SystemPrompt: func() string { return "system" },
		Model:        "m",
		MaxTokens:    100,
		ContextLimit: 1000,
		OnEvent:      func(e agent.Event) { events <- e },
		Tools:        tr,
		Approve:      approver.Approve,
	})
	return Deps{Agent: a, Events: events, Approver: approver, Yes: yes}, srv
}

// testTools is a test-local copy of cmd/swarg's adapter over a real tools.Set.
type testTools struct {
	set *tools.Set
	env tools.Env
}

// jobTools is testTools with BackgroundJobs: the hooks reach the tools, and shell_output
// first waits until its job is done (a channel, not a sleep).
type jobTools struct {
	testTools
	jobs *tools.Jobs
}

func (r testTools) Specs() []chat.ToolSpec { return r.set.Specs() }

func (r testTools) Kind(name string) (bool, bool, bool) { return r.set.Kind(name) }

var _ agent.ToolRunner = testTools{}

func (r testTools) Detachable(name string) bool { return name == "shell" }

func (r testTools) Label(c chat.ToolCall) string {
	return r.set.Label(c.Name, json.RawMessage(c.Args))
}

func (r testTools) Run(ctx context.Context, c chat.ToolCall, h agent.ToolHooks) (string, string, error) {
	env := r.env
	env.Progress, env.Detach = h.Progress, h.Detach
	res, err := r.set.Run(ctx, env, c.Name, json.RawMessage(c.Args))
	return res.Output, res.Brief, err
}

func (r jobTools) Run(ctx context.Context, c chat.ToolCall, h agent.ToolHooks) (string, string, error) {
	if c.Name == "shell_output" {
		var a struct{ ID string }
		if json.Unmarshal([]byte(c.Args), &a) == nil {
			<-r.jobs.Done(a.ID)
		}
	}
	return r.testTools.Run(ctx, c, h)
}

// newTestTools returns the tools of a real Set rooted at a fresh temp dir.
func newTestTools(t *testing.T) (agent.ToolRunner, string) {
	t.Helper()
	tr, dir, _ := newJobTools(t)
	return tr, dir
}

// newJobTools is newTestTools plus a Jobs set that is killed when the test ends.
func newJobTools(t *testing.T) (agent.ToolRunner, string, *tools.Jobs) {
	t.Helper()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	set, err := tools.NewSet(tools.All(), nil)
	if err != nil {
		t.Fatal(err)
	}
	jobs := tools.NewJobs()
	t.Cleanup(jobs.KillAll)
	tt := testTools{set: set, env: tools.Env{Root: root, Dir: dir, OS: runtime.GOOS, Jobs: jobs}}
	return jobTools{testTools: tt, jobs: jobs}, dir, jobs
}

// toolCallReply asks for one call; the next reply is the final text.
func toolCallReply(name, args string) llmtest.Reply {
	return llmtest.Reply{ToolCalls: []chat.ToolCall{{ID: "c1", Name: name, Args: args}}}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestRunPlain_StreamsAnswer(t *testing.T) {
	d, srv := plainDeps(t, llmtest.Reply{Chunks: []string{"Привет", ", мир", "\n"}})
	in := strings.NewReader("Первая строка\nвторая строка 🙂\n")
	var out bytes.Buffer

	if err := RunPlain(context.Background(), d, in, &out); err != nil {
		t.Fatalf("RunPlain: %v", err)
	}
	if got, want := out.String(), "Привет, мир\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
	reqs := srv.Requests()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d, want 1", len(reqs))
	}
	msgs, _ := reqs[0]["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages = %d, want system + one user", len(msgs))
	}
	user, _ := msgs[1].(map[string]any)
	if got, want := user["content"], "Первая строка\nвторая строка 🙂"; got != want {
		t.Errorf("user content = %q, want %q", got, want)
	}
}

func TestRunPlain_Truncated(t *testing.T) {
	d, _ := plainDeps(t, llmtest.Reply{Chunks: []string{"partial"}, Finish: "length"})
	var out bytes.Buffer

	if err := RunPlain(context.Background(), d, strings.NewReader("q"), &out); err != nil {
		t.Fatalf("RunPlain: %v", err)
	}
	if got, want := out.String(), "partial\n[answer truncated: token limit]\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestRunPlain_ServerError(t *testing.T) {
	d, _ := plainDeps(t, llmtest.Reply{Status: 500})
	var out bytes.Buffer

	err := RunPlain(context.Background(), d, strings.NewReader("q"), &out)
	if err == nil {
		t.Fatal("RunPlain returned nil, want the turn error")
	}
	if !strings.Contains(out.String(), "[error: ") {
		t.Errorf("output = %q, want an [error: ...] notice", out.String())
	}
}

func TestRunPlain_EmptyStdin(t *testing.T) {
	d, srv := plainDeps(t)
	var out bytes.Buffer

	if err := RunPlain(context.Background(), d, strings.NewReader(" \n\t\n"), &out); err != nil {
		t.Fatalf("RunPlain: %v", err)
	}
	if got, want := out.String(), "usage: echo \"question\" | swarg\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
	if n := len(srv.Requests()); n != 0 {
		t.Errorf("requests = %d, want 0", n)
	}
}

func TestRunPlain_ExitsWhenIdle(t *testing.T) {
	d, _ := plainDeps(t, llmtest.Reply{Chunks: []string{"ok"}, Usage: &chat.Usage{PromptTokens: 3, CompletionTokens: 1}})
	var out bytes.Buffer

	if err := RunPlain(context.Background(), d, strings.NewReader("q"), &out); err != nil {
		t.Fatalf("RunPlain: %v", err)
	}
	if d.Agent.Busy() {
		t.Error("agent still busy after RunPlain returned")
	}
}

func TestRunPlain_YesWriteFileCreatesFileAndPrintsToolLines(t *testing.T) {
	tr, dir := newTestTools(t)
	d, srv := plainRig(t, tr, true,
		toolCallReply("write_file", `{"path":"a.txt","content":"hi\n"}`),
		llmtest.Reply{Chunks: []string{"готово\n"}})
	var out bytes.Buffer

	if err := RunPlain(context.Background(), d, strings.NewReader("запиши"), &out); err != nil {
		t.Fatalf("RunPlain: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "a.txt")); err != nil || string(data) != "hi\n" {
		t.Fatalf("a.txt = %q, %v; want the written content", data, err)
	}
	for _, want := range []string{"● write_file(", "└", "готово\n"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output = %q, want it to contain %q", out.String(), want)
		}
	}
	if n := len(srv.Requests()); n != 2 {
		t.Errorf("requests = %d, want 2", n)
	}
}

func TestRunPlain_WithoutYesDeniesWrite(t *testing.T) {
	tr, dir := newTestTools(t)
	d, srv := plainRig(t, tr, false,
		toolCallReply("write_file", `{"path":"a.txt","content":"hi\n"}`),
		llmtest.Reply{Chunks: []string{"ok"}})
	var out bytes.Buffer

	if err := RunPlain(context.Background(), d, strings.NewReader("запиши"), &out); err != nil {
		t.Fatalf("RunPlain: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a.txt")); !os.IsNotExist(err) {
		t.Errorf("a.txt exists or stat failed (%v); want it not created", err)
	}
	reqs := srv.Requests()
	if len(reqs) != 2 {
		t.Fatalf("requests = %d, want 2", len(reqs))
	}
	body, _ := json.Marshal(reqs[1])
	if !strings.Contains(string(body), "not approved: run with --yes to allow") {
		t.Errorf("second request does not carry the approval hint: %s", body)
	}
	if !strings.Contains(out.String(), "● write_file(") {
		t.Errorf("output = %q, want the tool line", out.String())
	}
}

func TestRunPlain_ReadOnlyCallNeedsNoYes(t *testing.T) {
	tr, dir := newTestTools(t)
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("secret-line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d, srv := plainRig(t, tr, false,
		toolCallReply("read_file", `{"path":"note.txt"}`),
		llmtest.Reply{Chunks: []string{"ok"}})
	var out bytes.Buffer

	if err := RunPlain(context.Background(), d, strings.NewReader("прочитай"), &out); err != nil {
		t.Fatalf("RunPlain: %v", err)
	}
	reqs := srv.Requests()
	if len(reqs) != 2 {
		t.Fatalf("requests = %d, want 2", len(reqs))
	}
	body, _ := json.Marshal(reqs[1])
	if !strings.Contains(string(body), "secret-line") {
		t.Errorf("second request lacks the file content: %s", body)
	}
	if strings.Contains(string(body), "not approved") {
		t.Errorf("read-only call was denied: %s", body)
	}
	if !strings.Contains(out.String(), "● read_file(") {
		t.Errorf("output = %q, want the read_file line", out.String())
	}
}

// ---------------------------------------------------------------------------
// Live output and background jobs in plain mode
// ---------------------------------------------------------------------------

func TestRunPlain_NoLiveOutput(t *testing.T) {
	tr, _ := newTestTools(t)
	cmd := "echo marker-$((40+2))"
	d, _ := plainRig(t, tr, true,
		toolCallReply("shell", `{"command":"`+cmd+`"}`),
		llmtest.Reply{Chunks: []string{"fin\n"}})
	var out bytes.Buffer

	if err := RunPlain(context.Background(), d, strings.NewReader("run"), &out); err != nil {
		t.Fatalf("RunPlain: %v", err)
	}
	if strings.Contains(out.String(), "marker-42") {
		t.Errorf("output = %q, want no live tool output in plain mode", out.String())
	}
	if !strings.Contains(out.String(), "● shell(") || !strings.HasSuffix(out.String(), "fin\n") {
		t.Errorf("output = %q, want the tool lines and the answer", out.String())
	}
}

func TestRunPlain_BackgroundStartedAndPolled(t *testing.T) {
	tr, _, jobs := newJobTools(t)
	d, srv := plainRig(t, tr, true,
		toolCallReply("shell", `{"command":"echo polled-ready","background":true}`),
		llmtest.Reply{ToolCalls: []chat.ToolCall{{ID: "c2", Name: "shell_output", Args: `{"id":"bg1"}`}}},
		llmtest.Reply{Chunks: []string{"fin\n"}})
	var out bytes.Buffer

	if err := RunPlain(context.Background(), d, strings.NewReader("go"), &out); err != nil {
		t.Fatalf("RunPlain: %v", err)
	}
	reqs := srv.Requests()
	if len(reqs) != 3 {
		t.Fatalf("requests = %d, want 3", len(reqs))
	}
	second, _ := json.Marshal(reqs[1])
	third, _ := json.Marshal(reqs[2])
	if !strings.Contains(string(second), "started in background as bg1") {
		t.Errorf("second request lacks the started note: %s", second)
	}
	if !strings.Contains(string(third), "polled-ready") || !strings.Contains(string(third), "[exit 0]") {
		t.Errorf("third request lacks the job output and status: %s", third)
	}
	if got := jobs.List(); len(got) != 1 || got[0].Running {
		t.Errorf("jobs = %+v, want one finished job", got)
	}
}

func TestRunPlain_IgnoresActivity(t *testing.T) {
	d, _ := plainDeps(t, llmtest.Reply{Reasoning: []string{"deep thought"}, Chunks: []string{"ok\n"}})
	var out bytes.Buffer
	if err := RunPlain(context.Background(), d, strings.NewReader("go\n"), &out); err != nil {
		t.Fatalf("RunPlain: %v", err)
	}
	if got := out.String(); got != "ok\n" {
		t.Errorf("output = %q, want only the answer", got)
	}
}
