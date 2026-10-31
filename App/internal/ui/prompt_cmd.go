package ui

import (
	"errors"
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// /prompt: the prompt profile (R-117)
// ---------------------------------------------------------------------------

var errPromptUsage = errors.New("usage: /prompt | /prompt <compact|standard|full> [global|project]")

// parsePrompt is /prompt (the menu) or /prompt <profile> [global|project]; the scope saves the
// profile as the config default, no scope applies it to this Session only.
func parsePrompt(f []string) (Command, error) {
	if len(f) == 1 {
		return Command{Kind: KindPrompt}, nil
	}
	if scope, ok := parseScope(f, 2); ok {
		return Command{Kind: KindPrompt, Name: f[1], Scope: scope}, nil
	}
	return Command{}, errPromptUsage
}

// promptCmd opens the menu or chooses a profile.
func (m *model) promptCmd(c Command) {
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

// choosePrompt applies a profile and returns the notice (the error text when it failed).
func (m *model) choosePrompt(name, scope string) string {
	note, err := saved(scope, "prompt profile: "+name, m.d.Prompt.Choose(name, scope))
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
func (m *model) promptMenu() *menu {
	mn := newMenu("Prompt profile ("+glyphAuto+" = in use)", nil)
	mn.hint = "↑/↓ move   Enter use now   s save as default   Tab scope   Esc close"
	rebuild := func() {
		rows := m.d.Prompt.Profiles()
		cells := make([][]string, len(rows))
		for i, r := range rows {
			mark := " "
			if r.Active {
				mark = glyphAuto
			}
			cells[i] = []string{"  " + mark + " " + Clean(r.Name), fmt.Sprintf("~%d tokens", r.Tokens), Clean(r.Note)}
		}
		items := make([]menuRow, len(rows))
		for i, t := range columns(cells) {
			items[i] = menuRow{ID: rows[i].Name, Text: t, Match: rows[i].Name}
		}
		mn.setItems(items)
	}
	mn.onChoose = func(id string) bool {
		mn.note = strings.TrimPrefix(m.choosePrompt(id, ""), "prompt profile: ")
		rebuild()
		return false
	}
	mn.keys["s"] = func(id string) {
		mn.note = strings.TrimPrefix(m.choosePrompt(id, mn.scope), "prompt profile: ")
		rebuild()
	}
	mn.onScope = func(string) {}
	rebuild()
	return mn
}
