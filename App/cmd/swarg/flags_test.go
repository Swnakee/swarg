package main

import (
	"io"
	"strings"
	"testing"
	"time"

	"swarg/internal/agent"
	"swarg/internal/chat"
	"swarg/internal/config"
	"swarg/internal/prompt"
	"swarg/internal/session"
	"swarg/internal/ui"
)

// ---------------------------------------------------------------------------
// Flags
// ---------------------------------------------------------------------------

func TestFlags_ContinueResumeNoSaveTraceOff(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    opts
		wantErr string
	}{
		{"none", nil, opts{}, ""},
		{"continue", []string{"--continue"}, opts{cont: true}, ""},
		{"resume id", []string{"--resume", "ab12"}, opts{resume: "ab12"}, ""},
		{"resume equals", []string{"--resume=ab12"}, opts{resume: "ab12"}, ""},
		{"resume short id", []string{"-r", "ab12"}, opts{resume: "ab12"}, ""},
		{"resume single dash long", []string{"-resume", "ab12"}, opts{resume: "ab12"}, ""},
		{"resume short equals", []string{"-r=ab12"}, opts{resume: "ab12"}, ""},
		{"pick long", []string{"--resume"}, opts{pick: true}, ""},
		{"pick short", []string{"-r"}, opts{pick: true}, ""},
		{"pick followed by flag", []string{"--resume", "--yes"}, opts{pick: true, yes: true}, ""},
		{"pick short followed by no-save", []string{"-r", "--no-save"}, opts{pick: true, noSave: true}, ""},
		{"pick after other flags", []string{"--yes", "-r"}, opts{pick: true, yes: true}, ""},
		{"model value looks like pick", []string{"--model", "-r"}, opts{model: "-r"}, ""},
		{"pick and id", []string{"-r", "-r", "ab"}, opts{}, "twice"},
		{"continue short", []string{"-c"}, opts{cont: true}, ""},
		{"continue short and pick", []string{"-c", "-r"}, opts{}, "cannot be used together"},
		{"pick and continue", []string{"-r", "--continue"}, opts{}, "cannot be used together"},
		{"short continue and short resume", []string{"-c", "-r", "ab"}, opts{}, "cannot be used together"},
		{"resume empty id", []string{"--resume="}, opts{}, "needs a session id"},
		{"continue and resume", []string{"--continue", "--resume", "ab"}, opts{}, "cannot be used together"},
		{"sessions", []string{"--sessions"}, opts{sessions: true}, ""},
		{"no-save", []string{"--no-save"}, opts{noSave: true}, ""},
		{"trace-off", []string{"--trace-off"}, opts{traceOff: true}, ""},
		{"combo", []string{"--continue", "--no-save", "--trace-off", "--yes", "--model", "m"},
			opts{cont: true, noSave: true, traceOff: true, yes: true, model: "m"}, ""},
		{"resume with no-save", []string{"--resume", "ab", "--no-save"}, opts{resume: "ab", noSave: true}, ""},
		{"stray argument", []string{"hello"}, opts{}, "unexpected argument"},
		{"unknown flag", []string{"--nope"}, opts{}, "not defined"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseArgs(tt.args, io.Discard)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("got %+v, %v; want %+v", got, err, tt.want)
			}
		})
	}
}

func TestFlags_TraceSubcommand(t *testing.T) {
	tests := []struct {
		args    []string
		want    opts
		wantErr bool
	}{
		{[]string{"trace"}, opts{trace: true}, false},
		{[]string{"trace", "ab12"}, opts{trace: true, traceID: "ab12"}, false},
		{[]string{"trace", "ab12", "extra"}, opts{}, true},
	}
	for _, tt := range tests {
		got, err := parseArgs(tt.args, io.Discard)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("%v: got %+v, %v; want %+v, err %v", tt.args, got, err, tt.want, tt.wantErr)
		}
	}
}

func TestFlags_HelpIsNotAnError(t *testing.T) {
	if _, err := parseArgs([]string{"-h"}, io.Discard); err != errHelp {
		t.Fatalf("err = %v, want errHelp", err)
	}
}

// ---------------------------------------------------------------------------
// Picking a session
// ---------------------------------------------------------------------------

// seed writes a session with the given id and first user message into dir.
func seed(t *testing.T, dir, id, text string, started time.Time) {
	t.Helper()
	w, err := session.Create(dir, session.Header{ID: id, Model: "m", Started: started})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Message(chat.Message{Role: chat.RoleUser, Content: text}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Message(chat.Message{Role: chat.RoleAssistant, Content: "ack"}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPickSession_PrefixAmbiguousUnknown(t *testing.T) {
	dir := t.TempDir()
	base := time.Now().Add(-48 * time.Hour)
	seed(t, dir, "abc111", "first", base)
	seed(t, dir, "abc222", "second", base.Add(time.Minute))
	seed(t, dir, "fed333", "third", base.Add(2*time.Minute))
	infos, err := session.List(dir)
	if err != nil {
		t.Fatal(err)
	}

	if in, err := pickSession(infos, "", false); err != nil || in.ID != "fed333" {
		t.Errorf("newest = %q, %v; want fed333", in.ID, err)
	}
	if in, err := pickSession(infos, "fed", false); err != nil || in.ID != "fed333" {
		t.Errorf("prefix fed = %q, %v", in.ID, err)
	}
	if in, err := pickSession(infos, "abc2", false); err != nil || in.ID != "abc222" {
		t.Errorf("prefix abc2 = %q, %v", in.ID, err)
	}
	_, err = pickSession(infos, "abc", false)
	if err == nil || !strings.Contains(err.Error(), "ambiguous") || !strings.Contains(err.Error(), "abc111") || !strings.Contains(err.Error(), "abc222") {
		t.Errorf("ambiguous: err = %v, want both candidates listed", err)
	}
	_, err = pickSession(infos, "zzz", false)
	if err == nil || !strings.Contains(err.Error(), "fed333") {
		t.Errorf("unknown: err = %v, want the sessions listed", err)
	}
}

func TestPickSession_NoSessions(t *testing.T) {
	if in, err := pickSession(nil, "", true); err != nil || in.Path != "" {
		t.Errorf("none allowed: %+v, %v", in, err)
	}
	if _, err := pickSession(nil, "", false); err == nil {
		t.Error("no sessions and none not allowed: want an error")
	}
	if _, err := pickSession(nil, "ab", true); err == nil {
		t.Error("unknown id with no sessions: want an error")
	}
}

func TestSessionsView_ListAndOpenWithoutSaving(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().Add(-2 * time.Hour)
	seed(t, dir, "aaaaaa", "hello there", now)
	seed(t, dir, "bbbbbb", "later one", now.Add(time.Minute))
	v := &saving{dir: dir} // saving off: Open only loads

	rows, err := v.List()
	if err != nil || len(rows) != 2 || rows[0].ID != "bbbbbb" || rows[0].Title != "later one" || rows[0].Model != "m" || rows[0].Messages != 2 {
		t.Fatalf("List = %+v, %v", rows, err)
	}
	o, err := v.Open("aaa")
	if err != nil || len(o.History) != 2 || o.History[0].Content != "hello there" || o.Title != "hello there" || o.Model != "m" || o.MaxSeq != 2 {
		t.Fatalf("Open = %+v, %v", o, err)
	}
	if _, err := v.Open("nope"); err == nil {
		t.Error("Open of an unknown id: want an error")
	}
	if v.Current() != "" {
		t.Error("Current without saving must be empty")
	}
	if err := v.Rename("x"); err == nil {
		t.Error("Rename without saving must fail")
	}
}

// ---------------------------------------------------------------------------
// choosePick
// ---------------------------------------------------------------------------

func TestChoosePick_Paths(t *testing.T) {
	infos := []session.Info{
		{ID: "aaaaaa", Path: "/x/20260101-000000-aaaaaa.jsonl", Title: "t", Model: "m", Messages: 2},
		{Path: "/x/20250101-000000-bbbbbb.jsonl", Err: "bad"},
	}
	var gotRows []ui.PickRow
	pick := func(id string, ok bool, err error) func([]ui.PickRow, func(string) error) (string, bool, error) {
		return func(r []ui.PickRow, _ func(string) error) (string, bool, error) { gotRows = r; return id, ok, err }
	}
	noDel := func(string) error { return nil }
	var out, errOut strings.Builder
	if id, code, done := choosePick(nil, true, pick("", false, nil), noDel, &out, &errOut); done != true || code != 0 || id != "" || !strings.Contains(out.String(), "no saved sessions in this folder") {
		t.Errorf("empty: %q %d %v %q", id, code, done, out.String())
	}
	out.Reset()
	if _, code, done := choosePick(infos, false, pick("", false, nil), noDel, &out, &errOut); !done || code != 2 ||
		!strings.Contains(out.String(), "aaaaaa") || !strings.Contains(errOut.String(), "swarg -r <id>") {
		t.Errorf("no tty: %d %v %q %q", code, done, out.String(), errOut.String())
	}
	if id, code, done := choosePick(infos, true, pick("aaaaaa", true, nil), noDel, &out, &errOut); done || code != 0 || id != "aaaaaa" {
		t.Errorf("chosen: %q %d %v", id, code, done)
	}
	if len(gotRows) != 2 || gotRows[1].ID != "bbbbbb" || gotRows[1].Err != "bad" || gotRows[0].Messages != 2 {
		t.Errorf("rows = %+v", gotRows)
	}
	if _, code, done := choosePick(infos, true, pick("", false, nil), noDel, &out, &errOut); !done || code != 0 {
		t.Errorf("cancel: %d %v", code, done)
	}
	if _, code, done := choosePick(infos, true, pick("", false, io.ErrClosedPipe), noDel, &out, &errOut); !done || code != 1 {
		t.Errorf("error: %d %v", code, done)
	}
}

func TestProviderOverride_FlagsThenEnv(t *testing.T) {
	t.Setenv("SWARG_MODEL", "env-model")
	t.Setenv("SWARG_BASE_URL", "http://env:8080/v1")
	for _, tc := range []struct {
		name                string
		o                   opts
		wantModel, wantBase string
	}{
		{"both from env", opts{}, "env-model", "http://env:8080/v1"},
		{"flags win", opts{model: "flag-model", baseURL: "http://flag:1/v1"}, "flag-model", "http://flag:1/v1"},
		{"model flag, base env", opts{model: "flag-model"}, "flag-model", "http://env:8080/v1"},
	} {
		m, b := tc.o.providerOverride()
		if m != tc.wantModel || b != tc.wantBase {
			t.Errorf("%s: got (%q, %q), want (%q, %q)", tc.name, m, b, tc.wantModel, tc.wantBase)
		}
	}
}

func TestProviderOverride_NoEnvNoFlagsIsEmpty(t *testing.T) {
	t.Setenv("SWARG_MODEL", "")
	t.Setenv("SWARG_BASE_URL", "")
	if m, b := (opts{}).providerOverride(); m != "" || b != "" {
		t.Errorf("got (%q, %q), want empty (the config default stays)", m, b)
	}
}

// ---------------------------------------------------------------------------
// Model choice at start (R-30)
// ---------------------------------------------------------------------------

func TestStartModel_Precedence(t *testing.T) {
	for _, tc := range []struct{ flag, resumed, configured, want string }{
		{"", "", "cfg", "cfg"},          // a new session: config / SWARG_MODEL
		{"", "chosen", "cfg", "chosen"}, // a resumed session keeps its last choice over the environment
		{"flag", "chosen", "cfg", "flag"},
		{"flag", "", "cfg", "flag"},
	} {
		if got := startModel(tc.flag, tc.resumed, tc.configured); got != tc.want {
			t.Errorf("startModel(%q, %q, %q) = %q, want %q", tc.flag, tc.resumed, tc.configured, got, tc.want)
		}
	}
}

func TestOnModel_RewritesThePromptThenForwards(t *testing.T) {
	set, _, err := prompt.Load(t.TempDir(), t.TempDir(), prompt.Env{Model: "old"})
	if err != nil {
		t.Fatal(err)
	}
	var got []agent.Event
	cfg := config.Defaults()
	srcs, _ := testSources(t, &cfg)
	f := onModel(set, srcs, func(ev agent.Event) {
		if ev.Kind == agent.EvModel && !strings.Contains(set.Build(), "Model: new") {
			t.Error("the event was forwarded before the prompt changed")
		}
		got = append(got, ev)
	})
	f(agent.Event{Kind: agent.EvDelta, Text: "x"})
	f(agent.Event{Kind: agent.EvModel, Text: "new"})
	if len(got) != 2 || !strings.Contains(set.Build(), "Model: new") {
		t.Errorf("events %+v, prompt:\n%s", got, set.Build())
	}
	if r := srcs.recent.Models("local"); len(r) != 1 || r[0] != "new" { // R-101: the last used model of the Source
		t.Errorf("recent %v, want [new]", r)
	}
}
