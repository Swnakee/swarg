package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"swarg/internal/agent"
	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

type fakeConfig struct {
	settings []Setting
	sets     []string // "scope key=value"
	err      error
}

func (f *fakeConfig) Settings() []Setting { return f.settings }
func (f *fakeConfig) Set(scope, key string, v any) error {
	if f.err != nil {
		return f.err
	}
	f.sets = append(f.sets, fmt.Sprintf("%s %s=%v", scope, key, v))
	for i := range f.settings {
		if f.settings[i].Key == key {
			f.settings[i].Value, f.settings[i].Scope = v, scope
		}
	}
	return nil
}

// menuRow is the screen row showing text while a menu is open.
func menuRowAt(t *testing.T, r *rig, text string) int {
	t.Helper()
	for i, row := range r.rows() {
		if strings.Contains(row, text) {
			return i
		}
	}
	t.Fatalf("no menu row with %q:\n%s", text, r.view())
	return -1
}

func space() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "} }

// ---------------------------------------------------------------------------
// Engine
// ---------------------------------------------------------------------------

func TestMenu_RightLabelClickAndHotkeys(t *testing.T) {
	var got []string
	mn := newMenu("T", []menuRow{{ID: "a", Text: "  row a", Right: "off"}, {ID: "b", Text: "  row b", Right: "on"}})
	mn.onRight = func(id string) { got = append(got, "right:"+id) }
	mn.keys["space"] = func(id string) { got = append(got, "space:"+id) }
	mn.keys["r"] = func(id string) { got = append(got, "r:"+id) }
	drive(mn, tea.WindowSizeMsg{Width: 40, Height: 6})
	drive(mn, tea.MouseClickMsg{X: 38, Y: 2, Button: tea.MouseLeft}) // the label of row b
	drive(mn, space(), ptype("r"))
	if strings.Join(got, " ") != "right:b space:b r:b" {
		t.Fatalf("got %v", got)
	}
	drive(mn, ptype("/"), ptype("r")) // while filtering, r types
	if mn.filter != "r" || len(got) != 3 {
		t.Fatalf("filter %q got %v", mn.filter, got)
	}
}

func TestMenu_ScopeTabAndSubmenuEsc(t *testing.T) {
	r := newRig(t)
	var scopes []string
	mn := newMenu("T", []menuRow{{ID: "a", Text: "a"}})
	mn.onScope = func(s string) { scopes = append(scopes, s) }
	r.m.d.Scopes = fakeScopes{}
	r.m.openMenu(mn, nil)
	if !strings.Contains(r.view(), "Tab scope: project") {
		t.Fatalf("footer: %q", r.view())
	}
	r.send(key(tea.KeyTab, 0))
	r.send(key(tea.KeyTab, 0))
	if strings.Join(scopes, " ") != "global project" || mn.scope != "project" {
		t.Fatalf("scopes %v now %q", scopes, mn.scope)
	}
	sub := newMenu("Sub", []menuRow{{ID: "x", Text: "x"}})
	r.m.openMenu(sub, mn)
	if r.m.pick != sub {
		t.Fatal("sub not open")
	}
	r.send(key(tea.KeyEscape, 0))
	if r.m.pick != mn {
		t.Fatal("Esc did not return to the parent")
	}
	r.send(key(tea.KeyEscape, 0))
	if r.m.pick != nil {
		t.Fatal("Esc did not close the parent")
	}
}

func TestMenu_DimRowsSkippedAndFooterTexts(t *testing.T) {
	mn := newMenu("T", nil)
	drive(mn, tea.WindowSizeMsg{Width: 40, Height: 5})
	if v := mn.View().Content; !strings.Contains(v, "nothing here") {
		t.Fatalf("empty: %q", v)
	}
	mn.setItems([]menuRow{{ID: "d", Text: "dim", Dim: true}, {ID: "a", Text: "alpha"}})
	if drive(mn, pkey(tea.KeyEnter)); mn.chosen != "a" {
		t.Fatalf("chosen %q", mn.chosen)
	}
	drive(mn, ptype("z"))
	if v := mn.View().Content; !strings.Contains(v, "nothing matches") {
		t.Fatalf("no match: %q", v)
	}
	mn.confirmWith("sure?", "[y] go", func() { mn.note = "went" })
	drive(mn, tea.KeyPressMsg{Code: tea.KeyBackspace}) // any other key cancels
	if mn.confirm || mn.note != "" {
		t.Fatal("cancel")
	}
	mn.confirmWith("sure?", "[y] go", func() { mn.note = "went" })
	drive(mn, ptype("y"))
	if mn.note != "went" {
		t.Fatalf("note %q", mn.note)
	}
}

// ---------------------------------------------------------------------------
// /skills, /mcp menus
// ---------------------------------------------------------------------------

func TestSkillsMenu_ToggleRunAndScope(t *testing.T) {
	r := newRig(t)
	ag := &skillAgent{}
	fs := &fakeSkills{rows: []SkillRow{{Name: "deploy", Description: "ship it", Source: "project", Enabled: true}, {Name: "lint", Description: "lint", Source: "global"}}}
	r.m.d.Agent, r.m.d.Skills, r.m.d.Scopes = ag, fs, fakeScopes{}
	r.cmd("/skills")
	y := menuRowAt(t, r, "deploy")
	r.send(space())
	if fs.toggled[0] != "deploy:project" || fs.rows[0].Enabled || !strings.Contains(r.view(), "skill deploy: off (saved to project)") {
		t.Fatalf("space: %v %q", fs.toggled, r.view())
	}
	r.send(key(tea.KeyTab, 0))
	r.click(r.m.width-1, y) // the on/off label
	if fs.toggled[1] != "deploy:global" || !fs.rows[0].Enabled {
		t.Fatalf("label click: %v", fs.toggled)
	}
	r.send(key(tea.KeyDown, 0))
	r.send(key(tea.KeyEnter, 0))
	if r.m.pick != nil || len(ag.skills) != 1 || ag.skills[0] != [2]string{"lint", ""} {
		t.Fatalf("enter: open %v skills %v", r.m.pick != nil, ag.skills)
	}
}

// A switch that applied but could not be written shows the new state and the write error.
func TestSkillsMenu_PersistErrorKeepsRowsTrue(t *testing.T) {
	r := newRig(t)
	fs := &fakeSkills{rows: []SkillRow{{Name: "deploy", Enabled: true}}, saveErr: errors.New("open x: permission denied")}
	r.m.d.Skills, r.m.d.Scopes = fs, fakeScopes{}
	r.cmd("/skills")
	r.send(space())
	if v := r.view(); !strings.Contains(v, "○ deploy") || !strings.Contains(v, "skill deploy: off (this run only; not saved: open x: permission denied)") {
		t.Fatalf("view: %q", v)
	}
}

func TestMCPMenu_ToggleRestartApproveAndServerView(t *testing.T) {
	r := newRig(t)
	fm := &fakeMCP{servers: []MCPServerRow{{Name: "demo", State: "running", Tools: 2, Enabled: true, Source: "global", Transport: "stdio"},
		{Name: "proj", State: "needs approval", Source: "project", Transport: "http"}}}
	r.m.d.MCP, r.m.d.Scopes = fm, fakeScopes{}
	r.cmd("/mcp")
	if v := r.view(); !strings.Contains(v, "needs approval") || !strings.Contains(v, "approve") {
		t.Fatalf("menu: %q", v)
	}
	r.send(space())
	if fm.toggled[0] != "demo:project" || fm.servers[0].Enabled || !strings.Contains(r.view(), "hidden") {
		t.Fatalf("space: %v %q", fm.toggled, r.view())
	}
	r.send(ptype("r"))
	if fm.restarted[0] != "demo" || !strings.Contains(r.view(), "mcp demo: running") {
		t.Fatalf("r: %v", fm.restarted)
	}
	y := menuRowAt(t, r, "proj")
	r.click(r.m.width-1, y) // the approve label
	if fm.approved[0] != "proj" || fm.servers[1].State != "running" {
		t.Fatalf("approve: %v %+v", fm.approved, fm.servers[1])
	}
	fm.restartErr = errors.New("exit 2")
	r.send(key(tea.KeyUp, 0))
	r.send(ptype("r"))
	if !strings.Contains(r.view(), "mcp demo: exit 2") {
		t.Fatalf("error note: %q", r.view())
	}
	r.send(key(tea.KeyEnter, 0)) // the server view
	if v := r.view(); !strings.Contains(v, "MCP server demo") || !strings.Contains(v, "lookup") || !strings.Contains(v, "stderr: warn: slow") {
		t.Fatalf("server view: %q", v)
	}
	r.send(key(tea.KeyEscape, 0))
	if r.m.pick == nil || !strings.Contains(r.view(), "MCP servers") {
		t.Fatal("Esc did not return to the servers")
	}
}

// ---------------------------------------------------------------------------
// /model, /think, /config
// ---------------------------------------------------------------------------

func TestModelMenu_ListsAndChooses(t *testing.T) {
	r := newRig(t)
	ma, fm := r.withModels("small", "big")
	r.m.modelName = "small"
	r.cmd("/model")
	r.send(modelsMsg{rows: idRows(fm.ids...)})
	if fm.calls != 1 || r.m.pick == nil || !strings.Contains(r.view(), glyphAuto+" small") {
		t.Fatalf("menu: calls %d %q", fm.calls, r.view())
	}
	r.send(key(tea.KeyDown, 0))
	r.send(key(tea.KeyEnter, 0))
	if r.m.pick != nil || len(ma.set) != 1 || ma.set[0] != "big" || r.m.modelName != "big" {
		t.Fatalf("enter: %v %q", ma.set, r.m.modelName)
	}
	r.cmd("/model 1") // the numbered form still works from the last list
	if ma.set[len(ma.set)-1] != "small" {
		t.Fatalf("/model 1: %v", ma.set)
	}
}

func TestConfigMenu_ToggleStepAndSave(t *testing.T) {
	r := newRig(t)
	ta := &thinkAgent{fakeAgent: r.ag}
	fc := &fakeConfig{settings: []Setting{
		{Key: "tools.auto_approve", Label: "auto-approve", Kind: "bool", Value: false, Scope: "global", Live: "auto"},
		{Key: "providers.local.thinking", Label: "thinking", Kind: "bool", Value: false, Scope: "global", Live: "think"},
		{Key: "tools.max_steps", Label: "max steps", Kind: "int", Value: 50, Min: 1, Max: 52, Step: 5, Scope: "global"},
	}}
	r.m.d.Agent, r.m.d.Config, r.m.d.Scopes = ta, fc, fakeScopes{}
	r.cmd("/config")
	if r.m.pick == nil || !strings.Contains(r.view(), "auto-approve  false  global") {
		t.Fatalf("menu: %q", r.view())
	}
	r.send(key(tea.KeyEnter, 0))
	if fc.sets[0] != "project tools.auto_approve=true" || !r.m.d.Approver.Auto() || !strings.Contains(r.view(), "auto-approve = true (saved to project)") {
		t.Fatalf("toggle: %v auto %v %q", fc.sets, r.m.d.Approver.Auto(), r.view())
	}
	r.send(key(tea.KeyDown, 0))
	r.send(space())
	if len(ta.set) != 1 || !ta.set[0] || !r.m.thinking {
		t.Fatalf("thinking: %v", ta.set)
	}
	r.send(key(tea.KeyDown, 0))
	r.send(key(tea.KeyRight, 0))
	r.send(key(tea.KeyRight, 0))
	r.send(key(tea.KeyLeft, 0))
	if got := strings.Join(fc.sets[2:], ","); got != "project tools.max_steps=52,project tools.max_steps=52,project tools.max_steps=47" || !strings.Contains(r.view(), "at the next start") {
		t.Fatalf("steps: %q %q", got, r.view())
	}
	fc.err = errors.New("tools.auto_approve = true is not allowed in a project file")
	r.send(key(tea.KeyHome, 0))
	r.send(key(tea.KeyEnter, 0))
	if !strings.Contains(r.view(), "not allowed in a project file") {
		t.Fatalf("error note: %q", r.view())
	}
	r.m.d.Config = nil
	r.send(key(tea.KeyEscape, 0))
	r.cmd("/config")
	if !strings.Contains(r.convText(), "settings are not available") {
		t.Fatalf("no config: %q", r.convText())
	}
}

func TestMenus_CloseOnAskAndWorkDuringTurn(t *testing.T) {
	r := newRig(t)
	r.m.d.Skills = &fakeSkills{rows: []SkillRow{{Name: "deploy", Enabled: true}}}
	r.events(started("go"))
	r.cmd("/skills")
	if r.m.pick == nil {
		t.Fatal("a menu must open during a Turn")
	}
	r.send(askMsg{call: chat.ToolCall{Name: "shell", Args: `{"command":"ls"}`}, reply: make(chan agent.Approval, 1)})
	if r.m.pick != nil {
		t.Fatal("an approval prompt must close the menu")
	}
}
