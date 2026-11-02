package menu

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func pkey(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code} }
func ptype(s string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: []rune(s)[0], Text: s}
}
func space() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "} }

// drive sends the messages and, like Bubble Tea, draws after each one (clicks hit the spots
// of the last draw).
func drive(p *Menu, msgs ...tea.Msg) (quit bool) {
	for _, m := range msgs {
		_, cmd := p.Update(m)
		if cmd != nil {
			if _, ok := cmd().(tea.QuitMsg); ok {
				quit = true
			}
		}
		p.View()
	}
	return quit
}

// ---------------------------------------------------------------------------
// Engine
// ---------------------------------------------------------------------------

// R-99
func TestMenu_RightLabelClickAndHotkeys(t *testing.T) {
	var got []string
	mn := New(Options{Title: "T", Rows: []Row{{ID: "a", Text: "  row a", Right: "off"}, {ID: "b", Text: "  row b", Right: "on"}},
		OnRight: func(id string) { got = append(got, "right:"+id) },
		Keys: map[string]func(string){
			"space": func(id string) { got = append(got, "space:"+id) },
			"r":     func(id string) { got = append(got, "r:"+id) },
		}})
	drive(mn, tea.WindowSizeMsg{Width: 40, Height: 6})
	drive(mn, tea.MouseClickMsg{X: 38, Y: 2, Button: tea.MouseLeft}) // the label of row b
	drive(mn, space(), ptype("r"))
	if strings.Join(got, " ") != "right:b space:b r:b" {
		t.Fatalf("got %v", got)
	}
	drive(mn, ptype("/"), ptype("r")) // while filtering, r types
	if mn.Filter() != "r" || len(got) != 3 {
		t.Fatalf("filter %q got %v", mn.Filter(), got)
	}
}

// R-99
func TestMenu_DimRowsSkippedAndFooterTexts(t *testing.T) {
	mn := New(Options{Title: "T"})
	drive(mn, tea.WindowSizeMsg{Width: 40, Height: 5})
	if v := mn.View().Content; !strings.Contains(v, "nothing here") {
		t.Fatalf("empty: %q", v)
	}
	mn.SetRows([]Row{{ID: "d", Text: "dim", Dim: true}, {ID: "a", Text: "alpha"}})
	if drive(mn, pkey(tea.KeyEnter)); mn.Chosen() != "a" {
		t.Fatalf("chosen %q", mn.Chosen())
	}
	drive(mn, ptype("z"))
	if v := mn.View().Content; !strings.Contains(v, "nothing matches") {
		t.Fatalf("no match: %q", v)
	}
	mn.Confirm("sure?", "[y] go", func() { mn.SetNote("went") })
	drive(mn, tea.KeyPressMsg{Code: tea.KeyBackspace}) // any other key cancels
	if mn.Confirming() || mn.Note() != "" {
		t.Fatal("cancel")
	}
	mn.Confirm("sure?", "[y] go", func() { mn.SetNote("went") })
	drive(mn, ptype("y"))
	if mn.Note() != "went" {
		t.Fatalf("note %q", mn.Note())
	}
}
