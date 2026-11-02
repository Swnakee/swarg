package ui

import (
	"errors"
	"strings"
	"swarg/internal/ui/command"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// ---------------------------------------------------------------------------
// /prompt (R-117)
// ---------------------------------------------------------------------------

type fakePrompt struct {
	cur    string
	chosen []string // "name scope"
	err    error
}

func (f *fakePrompt) Profiles() []PromptRow {
	sizes := map[string]int{"low": 100, "medium": 2400, "high": 5600}
	var rows []PromptRow
	for _, n := range []string{"low", "medium", "high"} {
		rows = append(rows, PromptRow{Name: n, Tokens: sizes[n], Active: n == f.cur})
	}
	return rows
}

func (f *fakePrompt) Choose(name, scope string) error {
	f.chosen = append(f.chosen, name+" "+scope)
	if name == "bogus" {
		return errors.New("unknown prompt profile \"bogus\", known: low, medium, high")
	}
	f.cur = map[string]string{"compact": "low", "standard": "medium", "full": "high"}[name] // like main: old names read as new
	if f.cur == "" {
		f.cur = name
	}
	return f.err
}

func TestParsePrompt(t *testing.T) {
	tests := []struct {
		line    string
		want    command.Command
		wantErr bool
	}{
		{"/prompt", command.Command{Kind: command.KindPrompt}, false},
		{"/prompt high", command.Command{Kind: command.KindPrompt, Name: "high"}, false},
		{"/prompt high global", command.Command{Kind: command.KindPrompt, Name: "high", Scope: "global"}, false},
		{"/prompt high project", command.Command{Kind: command.KindPrompt, Name: "high", Scope: "project"}, false},
		{"/prompt high maybe", command.Command{}, true},
		{"/prompt a b c", command.Command{}, true},
	}
	for _, tt := range tests {
		got, err := command.Parse(tt.line)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("command.Parse(%q) = %+v, %v", tt.line, got, err)
		}
	}
}

func TestPromptCmd_ChoosesForThisSession(t *testing.T) {
	r := newRig(t)
	fp := &fakePrompt{cur: "low"}
	r.m.d.Prompt = fp
	r.cmd("/prompt medium")
	if len(fp.chosen) != 1 || fp.chosen[0] != "medium " {
		t.Fatalf("chosen %v, want medium with no scope", fp.chosen)
	}
	if !strings.Contains(r.convText(), "prompt profile: medium (this session, from the next request)") {
		t.Errorf("notice missing:\n%s", r.convText())
	}
}

func TestPromptCmd_ScopeSavesAndFailureIsShown(t *testing.T) {
	r := newRig(t)
	fp := &fakePrompt{cur: "low"}
	r.m.d.Prompt = fp
	r.cmd("/prompt high global")
	if fp.chosen[0] != "high global" || !strings.Contains(r.convText(), "saved to global") {
		t.Errorf("chosen %v\n%s", fp.chosen, r.convText())
	}
	fp.err = &NotSavedError{Err: errors.New("disk high")}
	r.cmd("/prompt medium project")
	if !strings.Contains(r.convText(), "this run only; not saved: disk high") {
		t.Errorf("a failed save is not explained:\n%s", r.convText())
	}
	r.cmd("/prompt bogus")
	if !strings.Contains(r.convText(), "prompt: unknown prompt profile") {
		t.Errorf("a refused profile is not shown:\n%s", r.convText())
	}
}

func TestPromptCmd_NoAPI(t *testing.T) {
	r := newRig(t)
	r.cmd("/prompt")
	if !strings.Contains(r.convText(), "not available") {
		t.Errorf("missing notice:\n%s", r.convText())
	}
}

func TestPromptMenu_MarksProfileEnterAndSave(t *testing.T) {
	r := newRig(t)
	fp := &fakePrompt{cur: "medium"}
	r.m.d.Prompt, r.m.d.Scopes = fp, fakeScopes{}
	r.cmd("/prompt")
	if r.m.pick == nil {
		t.Fatal("no menu")
	}
	v := r.view()
	for _, want := range []string{"~100 tokens", "~2400 tokens", "~5600 tokens"} {
		if !strings.Contains(v, want) {
			t.Errorf("menu lacks %q:\n%s", want, v)
		}
	}
	if !strings.Contains(r.rows()[menuRowAt(t, r, "medium")], glyphAuto) {
		t.Error("the profile in use is not marked")
	}
	r.send(key(tea.KeyEnd, 0)) // the cursor to the last row, high
	r.send(key(tea.KeyEnter, 0))
	if fp.chosen[len(fp.chosen)-1] != "high " || r.m.pick == nil {
		t.Fatalf("Enter: chosen %v, menu open %v", fp.chosen, r.m.pick != nil)
	}
	if !strings.Contains(r.rows()[menuRowAt(t, r, "high")], glyphAuto) {
		t.Error("the mark did not move to high")
	}
	r.send(ptype("s"))
	if got := fp.chosen[len(fp.chosen)-1]; got != "high project" {
		t.Errorf("s saved %q, want the default scope", got)
	}
}

func TestConfigMenu_PromptProfileRowSteps(t *testing.T) {
	r := newRig(t)
	fp := &fakePrompt{cur: "low"}
	fc := &fakeConfig{settings: []Setting{
		{Key: "prompt.profile", Label: "prompt profile", Kind: "string", Value: "low", Scope: "global", Live: "profile"},
	}}
	r.m.d.Prompt, r.m.d.Config, r.m.d.Scopes = fp, fc, fakeScopes{}
	r.cmd("/config")
	r.send(key(tea.KeyEnter, 0))
	if got := strings.Join(fc.sets, ","); got != "project prompt.profile=medium" {
		t.Fatalf("config sets %q", got)
	}
	if len(fp.chosen) != 1 || fp.chosen[0] != "medium " {
		t.Errorf("not applied live: %v", fp.chosen)
	}
	if strings.Contains(r.view(), "next start") {
		t.Errorf("the row must apply at once:\n%s", r.view())
	}
}

func TestNextProfile(t *testing.T) {
	rows := (&fakePrompt{}).Profiles()
	for cur, want := range map[string]string{"low": "medium", "medium": "high", "high": "low", "odd": "low"} {
		if got := nextProfile(rows, cur); got != want {
			t.Errorf("nextProfile(%q) = %q, want %q", cur, got, want)
		}
	}
	if got := nextProfile(nil, "x"); got != "x" {
		t.Errorf("empty list changes the value: %q", got)
	}
}

func TestPrompt_OldNameTypedIsShownAsTheNewOne(t *testing.T) { // R-134
	r := newRig(t)
	fp := &fakePrompt{cur: "low"}
	r.m.d.Prompt, r.m.d.Scopes = fp, fakeScopes{}
	r.cmd("/prompt full")
	if v := r.view(); !strings.Contains(v, "prompt profile: high") || strings.Contains(v, "prompt profile: full") {
		t.Errorf("the notice must name the new profile:\n%s", v)
	}
}
