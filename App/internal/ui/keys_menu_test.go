package ui

import (
	"errors"
	"strings"
	"swarg/internal/ui/command"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// ---------------------------------------------------------------------------
// Fake key store and helpers. Placeholders only: no real key anywhere.
// ---------------------------------------------------------------------------

const testSecret = "sk-test-placeholder-value-WXYZ" // 30 chars: masks as sk-tes…WXYZ

type fakeKeys struct {
	rows     []KeyRow
	file     string
	warnings []string
	exists   bool
	inits    int
}

func (f *fakeKeys) Keys() []KeyRow     { return f.rows }
func (f *fakeKeys) File() string       { return f.file }
func (f *fakeKeys) Warnings() []string { return f.warnings }
func (f *fakeKeys) Init() (string, error) {
	f.inits++
	if f.exists {
		return "", errors.New("the keys file already exists; edit it")
	}
	f.exists = true
	return f.file, nil
}

func sampleKeys() *fakeKeys {
	return &fakeKeys{file: "/cfg/swarg/keys.env", rows: []KeyRow{
		{Name: "MCP_TOKEN", State: "missing", Users: []string{"mcp web env TOK"}},
		{Name: "OPENROUTER_API_KEY", State: "set (env, wins)", Masked: "sk-or-…a1b2", Users: []string{"provider openrouter"}},
	}}
}

func keysRig(t *testing.T) (*rig, *fakeKeys) {
	r := newRig(t)
	fk := sampleKeys()
	r.m.d.Keys = fk
	return r, fk
}

// ---------------------------------------------------------------------------
// Commands
// ---------------------------------------------------------------------------

func TestParseCommand_Keys(t *testing.T) {
	tests := []struct {
		line    string
		want    command.Command
		errPart string
	}{
		{line: "/keys", want: command.Command{Kind: command.KindKeys}},
		{line: "/keys init", want: command.Command{Kind: command.KindKeys, Text: "init"}},
		{line: "/keys set OPENROUTER_API_KEY", errPart: "usage: /keys"}, // typing a key is gone
		{line: "/keys delete MCP_TOKEN", errPart: "usage: /keys"},
		{line: "/keys x", errPart: "usage: /keys"},
		{line: "/keys init extra", errPart: "usage: /keys"},
		{line: "/keys set OPENROUTER_API_KEY " + testSecret, errPart: "usage: /keys"}, // an inline value is refused
		{line: "/keys " + testSecret, errPart: "usage: /keys"},                        // the "word" is the key
	}
	for _, tc := range tests {
		got, err := command.Parse(tc.line)
		switch {
		case tc.errPart != "":
			if err == nil || !strings.Contains(err.Error(), tc.errPart) {
				t.Errorf("%q: err %v, want %q", tc.line, err, tc.errPart)
			} else if strings.Contains(err.Error(), "sk-test") {
				t.Errorf("%q: the error echoes the line: %v", tc.line, err)
			}
		case err != nil || got != tc.want:
			t.Errorf("%q: %+v %v, want %+v", tc.line, got, err, tc.want)
		}
	}
}

// I-38
func TestKeysCommand_InlineValueIsRefusedAndNeverShown(t *testing.T) {
	r, fk := keysRig(t)
	r.cmd("/keys set OPENROUTER_API_KEY " + testSecret)
	if r.m.pick != nil || fk.inits != 0 {
		t.Fatal("an inline value must open nothing")
	}
	if got := r.convText(); !strings.Contains(got, "usage: /keys") || strings.Contains(r.view()+got, "sk-test") {
		t.Fatalf("refusal: %q", got)
	}
}

func TestKeysCommand_NoStore(t *testing.T) {
	r := newRig(t)
	r.cmd("/keys")
	if !strings.Contains(r.convText(), "key store is not available") {
		t.Fatalf("notice: %q", r.convText())
	}
}

func TestKeysCommand_InitCreatesTheFileOnceAndOpensTheMenu(t *testing.T) {
	r, fk := keysRig(t)
	r.cmd("/keys init")
	if fk.inits != 1 || !strings.Contains(r.convText(), "created /cfg/swarg/keys.env") {
		t.Fatalf("init: %d %q", fk.inits, r.convText())
	}
	if r.m.pick == nil {
		t.Fatal("the menu must open after init")
	}
	r.send(key(tea.KeyEscape, 0))
	r.cmd("/keys init") // the file exists now: refused, nothing overwritten
	if got := r.convText(); !strings.Contains(got, "already exists") {
		t.Fatalf("second init: %q", got)
	}
}

// ---------------------------------------------------------------------------
// The menu: view only
// ---------------------------------------------------------------------------

func TestKeysMenu_RowsStatesMaskAndFooter(t *testing.T) {
	r, _ := keysRig(t)
	r.cmd("/keys")
	v := r.view()
	for _, want := range []string{
		"MCP_TOKEN", "missing", "mcp web env TOK",
		"OPENROUTER_API_KEY", "set (env, wins)", "sk-or-…a1b2", "provider openrouter",
		"/cfg/swarg/keys.env", "r reload",
	} {
		if !strings.Contains(v, want) {
			t.Errorf("menu lacks %q:\n%s", want, v)
		}
	}
}

func TestKeysMenu_ReloadRereadsTheFile(t *testing.T) {
	r, fk := keysRig(t)
	r.cmd("/keys")
	fk.rows[0].State, fk.rows[0].Masked = "set (keys.env)", "set"
	r.send(tea.KeyPressMsg{Code: 'r', Text: "r"})
	v := r.view()
	if !strings.Contains(v, "set (keys.env)") || !strings.Contains(v, "reloaded") {
		t.Fatalf("after r:\n%s", v)
	}
}

func TestKeysMenu_WarningsAreShownOnOpenAndReload(t *testing.T) {
	r, fk := keysRig(t)
	fk.warnings = []string{"keys.env is not private and was ignored: chmod 600 /cfg/swarg/keys.env"}
	r.cmd("/keys")
	if !strings.Contains(r.view(), "chmod 600") {
		t.Fatalf("no warning on open:\n%s", r.view())
	}
	r.send(key(tea.KeyDown, 0))
	r.send(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if !strings.Contains(r.view(), "chmod 600") {
		t.Fatalf("no warning after r:\n%s", r.view())
	}
}

func TestKeysMenu_IKeyCreatesTheTemplate(t *testing.T) {
	r, fk := keysRig(t)
	r.cmd("/keys")
	r.send(tea.KeyPressMsg{Code: 'i', Text: "i"})
	if fk.inits != 1 || !strings.Contains(r.view(), "created /cfg/swarg/keys.env") {
		t.Fatalf("i: %d\n%s", fk.inits, r.view())
	}
	r.send(tea.KeyPressMsg{Code: 'i', Text: "i"})
	if !strings.Contains(r.view(), "already exists") {
		t.Fatalf("second i:\n%s", r.view())
	}
}

// There is no way to enter a key in the menu: Enter, d, n and Right change nothing and open no
// field (R-108).
// I-38
func TestKeysMenu_HasNoTextEntry(t *testing.T) {
	r, fk := keysRig(t)
	r.cmd("/keys")
	for _, k := range []tea.KeyPressMsg{key(tea.KeyEnter, 0), {Code: 'd', Text: "d"}, {Code: 'n', Text: "n"}, key(tea.KeyRight, 0)} {
		r.send(k)
		if r.m.pick == nil {
			t.Fatalf("the menu closed on %v", k)
		}
	}
	if fk.inits != 0 {
		t.Error("nothing may be created by these keys")
	}
	if got := r.view(); strings.Contains(got, "•") || strings.Contains(got, "›") {
		t.Errorf("a field opened:\n%s", got)
	}
}

func TestConfigMenu_KeysRowOpensKeysAndEscReturns(t *testing.T) {
	r, _ := keysRig(t)
	r.m.d.Config, r.m.d.Scopes = &fakeConfig{settings: []Setting{{Key: "tools.max_steps", Label: "max steps", Kind: "int", Value: 50, Scope: "global"}}}, fakeScopes{}
	r.cmd("/config")
	if !strings.Contains(r.view(), "API keys") {
		t.Fatalf("no Keys row:\n%s", r.view())
	}
	r.send(key(tea.KeyDown, 0))
	r.send(key(tea.KeyEnter, 0))
	if !strings.Contains(r.view(), "OPENROUTER_API_KEY") {
		t.Fatalf("keys menu not open:\n%s", r.view())
	}
	r.send(key(tea.KeyEscape, 0))
	if !strings.Contains(r.view(), "Settings") {
		t.Fatalf("Esc must return to /config:\n%s", r.view())
	}
	r.m.d.Keys = nil
	r.send(key(tea.KeyEscape, 0))
	r.cmd("/config")
	if strings.Contains(r.view(), "API keys") {
		t.Fatal("the row needs a key store")
	}
}

// ---------------------------------------------------------------------------
// K5: a key-like message is held back once
// ---------------------------------------------------------------------------

// R-109
func TestLooksLikeKey(t *testing.T) {
	for text, want := range map[string]bool{
		"hello":                                  false,
		"sk-short":                               false,
		"use sk-or-v1-0123456789abcdef0123":      true,
		"key " + testSecret + " here":            true,
		"ghp_0123456789abcdef0123456789abcdef01": true,
		"ask-this-question-is-long-enough":       false,
	} {
		if got := looksLikeKey(text); got != want {
			t.Errorf("looksLikeKey(%q) = %v", text, got)
		}
	}
}

// R-109, I-38
func TestSubmit_KeyLikeTextIsHeldBackOnceThenSent(t *testing.T) {
	r, _ := keysRig(t)
	text := "my key is " + testSecret
	r.cmd(text)
	if len(r.ag.submitted) != 0 || r.m.ta.Value() != text {
		t.Fatalf("held back: submitted %v input %q", r.ag.submitted, r.m.ta.Value())
	}
	if got := r.convText(); !strings.Contains(got, "looks like an API key") || strings.Contains(got, "sk-test") {
		t.Fatalf("notice: %q", got)
	}
	r.send(key(tea.KeyEnter, 0)) // the same text again goes through
	if len(r.ag.submitted) != 1 || r.ag.submitted[0] != text {
		t.Fatalf("second Enter: %v", r.ag.submitted)
	}
	r.cmd(text) // and the warning is armed again for the next one
	if len(r.ag.submitted) != 1 {
		t.Fatal("the warning must re-arm")
	}
	r.m.ta.SetValue(text + " edited")
	r.send(key(tea.KeyEnter, 0))
	if len(r.ag.submitted) != 1 {
		t.Fatal("an edited text warns again")
	}
}
