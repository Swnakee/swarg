package ui

import (
	"errors"
	"strings"
	"swarg/internal/ui/command"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// ---------------------------------------------------------------------------
// /agents
// ---------------------------------------------------------------------------

type fakeAgents struct {
	rows    []AgentRow
	toggled []string // "name:on|off:scope" per switch
	saveErr error
	newErr  error
	made    []string // "name:scope" per New
	edited  []string // name per Edit
	editNew bool
}

func (f *fakeAgents) Edit(name string) (string, bool, error) {
	f.edited = append(f.edited, name)
	if f.newErr != nil {
		return "", false, f.newErr
	}
	return "/tmp/x/agents/" + name + ".md", f.editNew, nil
}

func (f *fakeAgents) List() []AgentRow { return f.rows }
func (f *fakeAgents) SetEnabled(name string, on bool, scope string) error {
	for i := range f.rows {
		if f.rows[i].Name == name {
			f.rows[i].Enabled = on
			f.toggled = append(f.toggled, name+":"+onOff(on)+":"+scope)
			if f.saveErr != nil {
				return &NotSavedError{Err: f.saveErr}
			}
			return nil
		}
	}
	return errors.New("unknown agent " + name)
}
func (f *fakeAgents) New(name, scope string) (string, error) {
	f.made = append(f.made, name+":"+scope)
	if f.newErr != nil {
		return "", f.newErr
	}
	return "/tmp/x/agents/" + name + ".md", nil
}

func newAgentsRig(t *testing.T, fa *fakeAgents) *rig {
	r := newRig(t)
	r.m.d.Agents, r.m.d.Scopes = fa, fakeScopes{}
	return r
}

func TestParseAgents_Table(t *testing.T) {
	cases := []struct {
		line    string
		want    command.Command
		errPart string
	}{
		{line: "/agents", want: command.Command{Kind: command.KindAgents}},
		{line: "/agents off a", want: command.Command{Kind: command.KindAgents, Name: "a"}},
		{line: "/agents on a global", want: command.Command{Kind: command.KindAgents, Name: "a", On: true, Scope: "global"}},
		{line: "/agents new a", want: command.Command{Kind: command.KindAgents, Name: "a", Text: "new"}},
		{line: "/agents new a project", want: command.Command{Kind: command.KindAgents, Name: "a", Text: "new", Scope: "project"}},
		{line: "/agents edit a", want: command.Command{Kind: command.KindAgents, Name: "a", Text: "edit"}},
		{line: "/agents edit", errPart: "usage: /agents"},
		{line: "/agents on", errPart: "usage: /agents"},
		{line: "/agents a", errPart: "usage: /agents"},
		{line: "/agents off a here", errPart: "usage: /agents"},
		{line: "/agents new", errPart: "usage: /agents"},
	}
	for _, c := range cases {
		got, err := command.Parse(c.line)
		if c.errPart != "" {
			if err == nil || !strings.Contains(err.Error(), c.errPart) {
				t.Errorf("%q: err %v, want %q", c.line, err, c.errPart)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("%q: got %+v, %v; want %+v", c.line, got, err, c.want)
		}
	}
}

func TestAgentsMenu_ShowsRowsAndMarks(t *testing.T) {
	r := newAgentsRig(t, &fakeAgents{rows: []AgentRow{
		{Name: "explore", Description: "read only", Source: "builtin", Enabled: true},
		{Name: "mine", Description: "my type", Source: "project", Path: "/tmp/x/agents/mine.md"}}})
	r.cmd("/agents")
	v := r.view()
	for _, w := range []string{"Agents", "◉ explore", "○ mine", "builtin", "project"} {
		if !strings.Contains(v, w) {
			t.Fatalf("missing %q in %q", w, v)
		}
	}
}

func TestAgentsMenu_SpaceTogglesAndPersistsToScope(t *testing.T) {
	fa := &fakeAgents{rows: []AgentRow{{Name: "explore", Source: "builtin", Enabled: true}}}
	r := newAgentsRig(t, fa)
	r.cmd("/agents")
	r.send(space())
	if len(fa.toggled) != 1 || fa.toggled[0] != "explore:off:global" || !strings.Contains(r.view(), "agent explore: off (saved to global)") {
		t.Fatalf("toggled %v view %q", fa.toggled, r.view())
	}
}

func TestAgentsMenu_PersistErrorKeepsRowsTrue(t *testing.T) {
	fa := &fakeAgents{rows: []AgentRow{{Name: "explore", Enabled: true}}, saveErr: errors.New("open x: permission denied")}
	r := newAgentsRig(t, fa)
	r.cmd("/agents")
	r.send(space())
	if v := r.view(); !strings.Contains(v, "○ explore") || !strings.Contains(v, "agent explore: off (this run only; not saved: open x: permission denied)") {
		t.Fatalf("view: %q", v)
	}
}

func TestAgentsMenu_EnterShowsPathAndStaysOpen(t *testing.T) {
	r := newAgentsRig(t, &fakeAgents{rows: []AgentRow{{Name: "mine", Source: "project", Path: "/tmp/x/agents/mine.md", Enabled: true}}})
	r.cmd("/agents")
	r.send(key(tea.KeyEnter, 0))
	if r.m.pick == nil || !strings.Contains(r.view(), "mine: /tmp/x/agents/mine.md") {
		t.Fatalf("open %v view %q", r.m.pick != nil, r.view())
	}
}

func TestAgentsMenu_EnterOnBuiltinSaysBuiltIn(t *testing.T) {
	r := newAgentsRig(t, &fakeAgents{rows: []AgentRow{{Name: "explore", Source: "builtin", Enabled: true}}})
	r.cmd("/agents")
	r.send(key(tea.KeyEnter, 0))
	if !strings.Contains(r.view(), "explore: built-in") {
		t.Fatalf("view %q", r.view())
	}
}

func TestAgentsMenu_EmptyListSaysSo(t *testing.T) {
	r := newAgentsRig(t, &fakeAgents{})
	r.cmd("/agents")
	if !strings.Contains(r.view(), "no agent types") {
		t.Fatalf("view %q", r.view())
	}
}

func TestAgentsCmd_OffWithScopeCallsOnce(t *testing.T) {
	fa := &fakeAgents{rows: []AgentRow{{Name: "x", Enabled: true}}}
	r := newAgentsRig(t, fa)
	r.cmd("/agents off x project")
	if len(fa.toggled) != 1 || fa.toggled[0] != "x:off:project" || !strings.Contains(r.view(), "agent x: off (saved to project) (from the next request)") {
		t.Fatalf("toggled %v view %q", fa.toggled, r.view())
	}
}

func TestAgentsCmd_NewReportsPathOrError(t *testing.T) {
	fa := &fakeAgents{}
	r := newAgentsRig(t, fa)
	r.cmd("/agents new a global")
	if len(fa.made) != 1 || fa.made[0] != "a:global" || !strings.Contains(r.view(), "agent type file created: /tmp/x/agents/a.md") {
		t.Fatalf("made %v view %q", fa.made, r.view())
	}
	fa.newErr = errors.New("already exists")
	r.cmd("/agents new b")
	if !strings.Contains(r.view(), "agents new: already exists") {
		t.Fatalf("view %q", r.view())
	}
}

func TestAgentsCmd_NilAgentsSaysSo(t *testing.T) {
	r := newRig(t)
	r.cmd("/agents")
	if !strings.Contains(r.view(), "no sub-agent types available") {
		t.Fatalf("view %q", r.view())
	}
}

// S-723..S-729 helpers: the scope default is global even though fakeScopes defaults to project.

func TestAgentsCmd_OffDefaultsToGlobalNotProject(t *testing.T) {
	fa := &fakeAgents{rows: []AgentRow{{Name: "x", Enabled: true}}}
	r := newAgentsRig(t, fa)
	r.cmd("/agents off x")
	if len(fa.toggled) != 1 || fa.toggled[0] != "x:off:global" || !strings.Contains(r.view(), "(saved to global)") {
		t.Fatalf("toggled %v view %q", fa.toggled, r.view())
	}
}

func TestAgentsCmd_ExplicitProjectScopeStillWorks(t *testing.T) {
	fa := &fakeAgents{rows: []AgentRow{{Name: "x", Enabled: true}}}
	r := newAgentsRig(t, fa)
	r.cmd("/agents off x project")
	r.cmd("/agents new y project")
	if fa.toggled[0] != "x:off:project" || fa.made[0] != "y:project" {
		t.Fatalf("toggled %v made %v", fa.toggled, fa.made)
	}
}

func TestAgentsCmd_NewDefaultsToGlobal(t *testing.T) {
	fa := &fakeAgents{}
	r := newAgentsRig(t, fa)
	r.cmd("/agents new y")
	if len(fa.made) != 1 || fa.made[0] != "y:global" {
		t.Fatalf("made %v", fa.made)
	}
}

func TestAgentsMenu_TabReachesProject(t *testing.T) {
	fa := &fakeAgents{rows: []AgentRow{{Name: "x", Enabled: true}}}
	r := newAgentsRig(t, fa)
	r.cmd("/agents")
	r.send(key(tea.KeyTab, 0))
	r.send(space())
	if len(fa.toggled) != 1 || fa.toggled[0] != "x:off:project" {
		t.Fatalf("toggled %v", fa.toggled)
	}
}

func TestAgentsMenu_ShowsBuiltinAndFilePaths(t *testing.T) {
	r := newAgentsRig(t, &fakeAgents{rows: []AgentRow{
		{Name: "explore", Source: "builtin", Enabled: true},
		{Name: "mine", Source: "global", Path: "/cfg/agents/mine.md", Enabled: true}}})
	r.cmd("/agents")
	v := r.view()
	if !strings.Contains(v, "(built-in)") || !strings.Contains(v, "/cfg/agents/mine.md") {
		t.Fatalf("view %q", v)
	}
}

func TestAgentsMenu_EKeyEditsAndReportsCopy(t *testing.T) {
	fa := &fakeAgents{rows: []AgentRow{{Name: "explore", Source: "builtin", Enabled: true}}, editNew: true}
	r := newAgentsRig(t, fa)
	r.cmd("/agents")
	r.send(tea.KeyPressMsg{Code: 'e', Text: "e"})
	if len(fa.edited) != 1 || fa.edited[0] != "explore" || !strings.Contains(r.view(), "built-in copied to /tmp/x/agents/explore.md") {
		t.Fatalf("edited %v view %q", fa.edited, r.view())
	}
}

func TestAgentsCmd_EditShowsPathOrError(t *testing.T) {
	fa := &fakeAgents{}
	r := newAgentsRig(t, fa)
	r.cmd("/agents edit mine")
	if !strings.Contains(r.view(), "edit /tmp/x/agents/mine.md") {
		t.Fatalf("view %q", r.view())
	}
	fa.newErr = errors.New("not a built-in")
	r.cmd("/agents edit zzz")
	if !strings.Contains(r.view(), "agents edit: not a built-in") {
		t.Fatalf("view %q", r.view())
	}
}
