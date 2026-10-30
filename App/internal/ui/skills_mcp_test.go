package ui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

type fakeSkills struct {
	rows    []SkillRow
	toggled []string // "name:scope" per switch
	saveErr error    // the switch applies, the write fails (ui.ErrNotSaved)
}

func (f *fakeSkills) List() []SkillRow { return f.rows }
func (f *fakeSkills) SetEnabled(name string, on bool, scope string) error {
	for i := range f.rows {
		if f.rows[i].Name == name {
			f.rows[i].Enabled = on
			f.toggled = append(f.toggled, name+":"+scope)
			if f.saveErr != nil {
				return &NotSavedError{Err: f.saveErr}
			}
			return nil
		}
	}
	return errors.New("unknown skill " + name)
}

// fakeScopes offers global and project with project as the default.
type fakeScopes struct{}

func (fakeScopes) Scopes() []string { return []string{"project", "global"} }
func (fakeScopes) Default() string  { return "project" }

type fakeMCP struct {
	servers    []MCPServerRow
	restarted  []string
	approved   []string
	toggled    []string // "name:scope"
	restartErr error
}

func (f *fakeMCP) Approve(server string) error {
	for i := range f.servers {
		if f.servers[i].Name == server {
			f.servers[i].State, f.approved = "running", append(f.approved, server)
			return nil
		}
	}
	return errors.New("unknown mcp server " + server)
}

func (f *fakeMCP) Servers() []MCPServerRow { return f.servers }
func (f *fakeMCP) Tools(server string) ([]MCPToolRow, string, error) {
	if server != "demo" {
		return nil, "", errors.New("unknown mcp server " + server)
	}
	return []MCPToolRow{{Name: "lookup", Description: "find", ReadOnly: true, Offered: true}, {Name: "save", Description: "write", Offered: false}}, "warn: slow\n", nil
}
func (f *fakeMCP) SetEnabled(server string, on bool, scope string) error {
	for i := range f.servers {
		if f.servers[i].Name == server {
			f.servers[i].Enabled = on
			f.toggled = append(f.toggled, server+":"+scope)
			return nil
		}
	}
	return errors.New("unknown mcp server " + server)
}
func (f *fakeMCP) Restart(server string) error {
	f.restarted = append(f.restarted, server)
	return f.restartErr
}

// skillAgent is a fake agent that also takes /skill.
type skillAgent struct {
	fakeAgent
	skills [][2]string
	err    error
}

func (a *skillAgent) SubmitSkill(name, text string) error {
	a.skills = append(a.skills, [2]string{name, text})
	return a.err
}

// ---------------------------------------------------------------------------
// /skill, /skills
// ---------------------------------------------------------------------------

func TestSkills_ListToggleAndNoSkills(t *testing.T) {
	r := newRig(t)
	r.m.submit("/skills")
	if !strings.Contains(r.convText(), "no skills loaded") {
		t.Fatalf("no skills: %q", r.convText())
	}
	fs := &fakeSkills{rows: []SkillRow{{Name: "deploy", Description: "ship it", Source: "project", Enabled: true}, {Name: "lint", Description: "lint", Source: "global"}}}
	r.m.d.Skills = fs
	r.m.submit("/skills")
	if v := r.view(); r.m.pick == nil || !strings.Contains(v, "◉ deploy") || !strings.Contains(v, "○ lint") || !strings.Contains(v, "project") {
		t.Fatalf("menu: %q", v)
	}
	r.send(key(tea.KeyEscape, 0))
	r.m.submit("/skills on lint")
	if fs.toggled[0] != "lint:" || !fs.rows[1].Enabled || !strings.Contains(r.convText(), "skill lint: on (this run)") {
		t.Fatalf("toggle: %+v %q", fs, r.convText())
	}
	r.m.d.Scopes = fakeScopes{}
	r.m.submit("/skills off lint")
	r.m.submit("/skills off lint global")
	if fs.toggled[1] != "lint:project" || fs.toggled[2] != "lint:global" || !strings.Contains(r.convText(), "skill lint: off (saved to global)") {
		t.Fatalf("scopes: %v %q", fs.toggled, r.convText())
	}
	r.m.submit("/skills off nope")
	if !strings.Contains(r.convText(), "unknown skill nope") {
		t.Fatalf("unknown: %q", r.convText())
	}
}

func TestSkill_SubmitsThroughTheAgentAndSkillNameAsCommand(t *testing.T) {
	r := newRig(t)
	ag := &skillAgent{}
	r.m.d.Agent = ag
	r.m.d.Skills = &fakeSkills{rows: []SkillRow{{Name: "deploy", Enabled: true}}}
	r.m.submit("/skill deploy ship it now")
	r.m.submit("/deploy to prod")
	r.m.submit("/deploy")
	if len(ag.skills) != 3 || ag.skills[0] != [2]string{"deploy", "ship it now"} || ag.skills[1] != [2]string{"deploy", "to prod"} || ag.skills[2] != [2]string{"deploy", ""} {
		t.Fatalf("skills %v", ag.skills)
	}
	if v := r.view(); !strings.Contains(v, "ship it now") || !strings.Contains(v, "Apply this skill.") {
		t.Fatalf("waiting lines: %q", v)
	}
	r.m.submit("/nope x") // not a Skill: the usual error
	if !strings.Contains(r.convText(), "unknown command /nope") {
		t.Fatalf("unknown: %q", r.convText())
	}
	r.m.submit("/help") // a built-in wins over a Skill of the same name
	if len(ag.skills) != 3 {
		t.Fatal("/help went to the skills")
	}
}

func TestSkill_WithoutSkillsIsANotice(t *testing.T) {
	r := newRig(t) // the plain fakeAgent has no SubmitSkill
	r.m.submit("/skill deploy")
	if !strings.Contains(r.convText(), "no skills loaded") {
		t.Fatalf("%q", r.convText())
	}
}

// ---------------------------------------------------------------------------
// /mcp
// ---------------------------------------------------------------------------

func TestMCP_ListShowSwitchRestart(t *testing.T) {
	r := newRig(t)
	r.m.submit("/mcp")
	if !strings.Contains(r.convText(), "no MCP servers configured") {
		t.Fatalf("none: %q", r.convText())
	}
	fm := &fakeMCP{servers: []MCPServerRow{{Name: "demo", State: "running", Tools: 2, Enabled: true, Source: "global", Transport: "stdio"}, {Name: "docs", State: "failed", Reason: "exit 1", Transport: "http"}}}
	r.m.d.MCP = fm
	r.m.submit("/mcp")
	if v := r.view(); r.m.pick == nil || !strings.Contains(v, "demo") || !strings.Contains(v, "2 tools") || !strings.Contains(v, "failed") || !strings.Contains(v, "exit 1") || !strings.Contains(v, "http") {
		t.Fatalf("menu: %q", v)
	}
	r.send(key(tea.KeyEscape, 0))
	r.m.submit("/mcp demo")
	if v := r.view(); r.m.pick == nil || !strings.Contains(v, "lookup  read-only") || !strings.Contains(v, "save    not offered") || !strings.Contains(v, "stderr: warn: slow") {
		t.Fatalf("show: %q", v)
	}
	r.send(key(tea.KeyEscape, 0))
	if r.m.pick != nil {
		t.Fatal("Esc did not close the server view")
	}
	r.m.submit("/mcp off demo")
	if fm.servers[0].Enabled || fm.toggled[0] != "demo:" || !strings.Contains(r.convText(), "mcp demo: off (this run)") {
		t.Fatalf("off: %+v %q", fm.servers, r.convText())
	}
	r.m.submit("/mcp approve docs")
	if fm.approved[0] != "docs" || !strings.Contains(r.convText(), "mcp docs: approved and started") {
		t.Fatalf("approve: %v %q", fm.approved, r.convText())
	}
	r.m.submit("/mcp restart docs")
	if fm.restarted[0] != "docs" || !strings.Contains(r.convText(), "mcp docs: running") {
		t.Fatalf("restart: %v %q", fm.restarted, r.convText())
	}
	fm.restartErr = errors.New("exit 2")
	r.m.submit("/mcp restart docs")
	if !strings.Contains(r.convText(), "mcp docs: exit 2") {
		t.Fatalf("restart error: %q", r.convText())
	}
	r.m.submit("/mcp maybe demo")
	if !strings.Contains(r.convText(), "usage: /mcp") {
		t.Fatalf("usage: %q", r.convText())
	}
}
