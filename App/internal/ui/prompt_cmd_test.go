package ui

import (
	"errors"
	"strings"
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
	sizes := map[string]int{"compact": 100, "standard": 2400, "full": 5600}
	var rows []PromptRow
	for _, n := range []string{"compact", "standard", "full"} {
		rows = append(rows, PromptRow{Name: n, Tokens: sizes[n], Active: n == f.cur})
	}
	return rows
}

func (f *fakePrompt) Choose(name, scope string) error {
	f.chosen = append(f.chosen, name+" "+scope)
	if name == "bogus" {
		return errors.New("unknown prompt profile \"bogus\", known: compact, standard, full")
	}
	f.cur = name
	return f.err
}

func TestParsePrompt(t *testing.T) {
	tests := []struct {
		line    string
		want    Command
		wantErr bool
	}{
		{"/prompt", Command{Kind: KindPrompt}, false},
		{"/prompt full", Command{Kind: KindPrompt, Name: "full"}, false},
		{"/prompt full global", Command{Kind: KindPrompt, Name: "full", Scope: "global"}, false},
		{"/prompt full project", Command{Kind: KindPrompt, Name: "full", Scope: "project"}, false},
		{"/prompt full maybe", Command{}, true},
		{"/prompt a b c", Command{}, true},
	}
	for _, tt := range tests {
		got, err := ParseCommand(tt.line)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("ParseCommand(%q) = %+v, %v", tt.line, got, err)
		}
	}
}

func TestPromptCmd_ChoosesForThisSession(t *testing.T) {
	r := newRig(t)
	fp := &fakePrompt{cur: "compact"}
	r.m.d.Prompt = fp
	r.cmd("/prompt standard")
	if len(fp.chosen) != 1 || fp.chosen[0] != "standard " {
		t.Fatalf("chosen %v, want standard with no scope", fp.chosen)
	}
	if !strings.Contains(r.convText(), "prompt profile: standard (this session, from the next request)") {
		t.Errorf("notice missing:\n%s", r.convText())
	}
}

func TestPromptCmd_ScopeSavesAndFailureIsShown(t *testing.T) {
	r := newRig(t)
	fp := &fakePrompt{cur: "compact"}
	r.m.d.Prompt = fp
	r.cmd("/prompt full global")
	if fp.chosen[0] != "full global" || !strings.Contains(r.convText(), "saved to global") {
		t.Errorf("chosen %v\n%s", fp.chosen, r.convText())
	}
	fp.err = &NotSavedError{Err: errors.New("disk full")}
	r.cmd("/prompt standard project")
	if !strings.Contains(r.convText(), "this run only; not saved: disk full") {
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
	fp := &fakePrompt{cur: "standard"}
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
	if !strings.Contains(r.rows()[menuRowAt(t, r, "standard")], glyphAuto) {
		t.Error("the profile in use is not marked")
	}
	r.send(key(tea.KeyEnd, 0)) // the cursor to the last row, full
	r.send(key(tea.KeyEnter, 0))
	if fp.chosen[len(fp.chosen)-1] != "full " || r.m.pick == nil {
		t.Fatalf("Enter: chosen %v, menu open %v", fp.chosen, r.m.pick != nil)
	}
	if !strings.Contains(r.rows()[menuRowAt(t, r, "full")], glyphAuto) {
		t.Error("the mark did not move to full")
	}
	r.send(ptype("s"))
	if got := fp.chosen[len(fp.chosen)-1]; got != "full project" {
		t.Errorf("s saved %q, want the default scope", got)
	}
}

func TestConfigMenu_PromptProfileRowSteps(t *testing.T) {
	r := newRig(t)
	fp := &fakePrompt{cur: "compact"}
	fc := &fakeConfig{settings: []Setting{
		{Key: "prompt.profile", Label: "prompt profile", Kind: "string", Value: "compact", Scope: "global", Live: "profile"},
	}}
	r.m.d.Prompt, r.m.d.Config, r.m.d.Scopes = fp, fc, fakeScopes{}
	r.cmd("/config")
	r.send(key(tea.KeyEnter, 0))
	if got := strings.Join(fc.sets, ","); got != "project prompt.profile=standard" {
		t.Fatalf("config sets %q", got)
	}
	if len(fp.chosen) != 1 || fp.chosen[0] != "standard " {
		t.Errorf("not applied live: %v", fp.chosen)
	}
	if strings.Contains(r.view(), "next start") {
		t.Errorf("the row must apply at once:\n%s", r.view())
	}
}

func TestNextProfile(t *testing.T) {
	rows := (&fakePrompt{}).Profiles()
	for cur, want := range map[string]string{"compact": "standard", "standard": "full", "full": "compact", "odd": "compact"} {
		if got := nextProfile(rows, cur); got != want {
			t.Errorf("nextProfile(%q) = %q, want %q", cur, got, want)
		}
	}
	if got := nextProfile(nil, "x"); got != "x" {
		t.Errorf("empty list changes the value: %q", got)
	}
}
