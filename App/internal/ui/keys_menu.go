package ui

import (
	"regexp"
	"strings"
	"swarg/internal/ui/command"
	"swarg/internal/ui/menu"
)

// ---------------------------------------------------------------------------
// /keys (R-106, R-108, R-110): the global API keys, shown in a view-only menu on the one engine
// ---------------------------------------------------------------------------

const keysRowID = "keys" // the row of /config that opens this menu

// keyLike is what a pasted API key looks like: an sk- token of 20+ characters (covers
// sk-or-v1-...) or a GitHub token (R-109).
var keyLike = regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}|\bgh[pous]_[A-Za-z0-9]{30,}`)

const keyWarnText = "this looks like an API key, so it was not sent. Keys go in the keys file (/keys shows where), never in the chat. " +
	"Press Enter again to send the text anyway, or edit it."

// keysCmd is /keys (the menu) or /keys init (the template, then the menu).
func (m *model) keysCmd(c command.Command) {
	if m.d.Keys == nil {
		m.say("the key store is not available here")
		return
	}
	if c.Text == "init" {
		m.say(m.initKeys())
	}
	m.openMenu(m.keysMenu(), nil)
}

// initKeys creates the keys file template and says what happened.
func (m *model) initKeys() string {
	path, err := m.d.Keys.Init()
	if err != nil {
		return "keys: " + Clean(err.Error())
	}
	return "created " + Clean(path) + " (private): write NAME=value lines in it, then press r in /keys"
}

// keysMenu lists every key the config uses plus the ones in the files: name, state, the masked value
// and who uses it. It is view only (R-108): r re-reads the files, i creates the template when there
// is no keys file; the footer shows the file.
func (m *model) keysMenu() *menu.Menu {
	var mn *menu.Menu
	o := menuOpts("API keys (global)", nil)
	o.Hint = Clean(m.d.Keys.File()) + "   r reload   i create file   Esc close"
	o.Empty = "no keys used by the config   i creates " + Clean(m.d.Keys.File()) + "   Esc close"
	warn := func(ok string) {
		mn.SetNote(ok)
		if w := m.d.Keys.Warnings(); len(w) > 0 {
			mn.SetNote(Clean(strings.Join(w, "; ")))
		}
	}
	rebuild := func() {
		rows := m.d.Keys.Keys()
		cells := make([][]string, len(rows))
		for i, k := range rows {
			cells[i] = []string{"  " + Clean(k.Name), Clean(k.State), Clean(k.Masked), Clean(strings.Join(k.Users, ", "))}
		}
		mn.SetRows(columnRows(cells, func(i int) menu.Row {
			return menu.Row{ID: rows[i].Name, Match: rows[i].Name + " " + strings.Join(rows[i].Users, " ")}
		}))
	}
	reload := func(string) {
		rebuild()
		warn("reloaded")
	}
	o.Keys["r"] = reload
	o.Keys["i"] = func(string) {
		mn.SetNote(m.initKeys())
		rebuild()
	}
	o.OnChoose = func(string) bool { return false }
	mn = menu.New(o)
	rebuild()
	warn("")
	return mn
}

// looksLikeKey reports whether a chat message appears to contain an API key (R-109).
func looksLikeKey(text string) bool { return keyLike.MatchString(text) }

// keyWarning holds a message that looks like an API key back once (R-109): it stays in the input, a
// notice says why, and the same text sent again goes through. /keys lines are exempt (their
// refusal is the parser's). It reports whether the message was held back.
func (m *model) keyWarning(text string) bool {
	if !looksLikeKey(text) || strings.HasPrefix(text, "/keys") {
		return false
	}
	if m.keyWarned == text {
		m.keyWarned = ""
		return false
	}
	m.keyWarned = text
	m.say(keyWarnText)
	return true
}
