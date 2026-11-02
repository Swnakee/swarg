package ui

import (
	"fmt"
	"strings"
	"swarg/internal/ui/command"
	"swarg/internal/ui/menu"
)

// ---------------------------------------------------------------------------
// /prompt: the prompt profile (R-117)
// ---------------------------------------------------------------------------

// promptCmd opens the menu or chooses a profile.
func (m *model) promptCmd(c command.Command) {
	if m.d.Prompt == nil {
		m.say("prompt profiles are not available here")
		return
	}
	if c.Name == "" {
		m.openMenu(m.promptMenu(), nil)
		return
	}
	m.say(m.choosePrompt(c.Name, c.Scope))
}

// choosePrompt applies a profile and returns the notice (the error text when it failed). The
// notice names the profile in force, so an old name typed by the user is shown as the new one (R-134).
func (m *model) choosePrompt(name, scope string) string {
	cerr := m.d.Prompt.Choose(name, scope)
	for _, r := range m.d.Prompt.Profiles() {
		if r.Active {
			name = r.Name
		}
	}
	note, err := saved(scope, "prompt profile: "+name, cerr)
	switch {
	case err != nil:
		return "prompt: " + err.Error()
	case scope == "":
		return "prompt profile: " + name + " (this session, from the next request)"
	}
	return note + " (from the next request)"
}

// promptMenu lists the profiles with their size: Enter applies one to this Session, s saves it as
// the default of the scope (Tab cycles the scope).
func (m *model) promptMenu() *menu.Menu {
	var mn *menu.Menu
	o := menuOpts("Prompt profile ("+glyphAuto+" = in use)", nil)
	o.Hint = "↑/↓ move   Enter use now   s save as default   Tab scope   Esc close"
	rebuild := func() {
		rows := m.d.Prompt.Profiles()
		cells := make([][]string, len(rows))
		for i, r := range rows {
			cells[i] = []string{"  " + pickMark(r.Active, glyphAuto, " ") + " " + Clean(r.Name), fmt.Sprintf("~%d tokens", r.Tokens), Clean(r.Note)}
		}
		mn.SetRows(columnRows(cells, func(i int) menu.Row { return menu.Row{ID: rows[i].Name, Match: rows[i].Name} }))
	}
	o.OnChoose = func(id string) bool {
		mn.SetNote(strings.TrimPrefix(m.choosePrompt(id, ""), "prompt profile: "))
		rebuild()
		return false
	}
	o.Keys["s"] = func(id string) {
		mn.SetNote(strings.TrimPrefix(m.choosePrompt(id, mn.Scope()), "prompt profile: "))
		rebuild()
	}
	o.OnScope = func(string) {}
	mn = menu.New(o)
	rebuild()
	return mn
}
