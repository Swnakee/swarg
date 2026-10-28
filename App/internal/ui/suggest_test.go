package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"swarg/internal/agent"
	"swarg/internal/chat"
	"swarg/internal/ui/mdstream"
)

// ---------------------------------------------------------------------------
// Command suggestions and the command highlight (T3 ui item 6, R-35)
// ---------------------------------------------------------------------------

// typeIn types text into the input key by key.
func (r *rig) typeIn(s string) {
	for _, ch := range s {
		r.send(ptype(string(ch)))
	}
}

// names lists the command names of specs.
func names(s []cmdSpec) []string {
	out := make([]string, len(s))
	for i, c := range s {
		out[i] = c.name
	}
	return out
}

func TestSuggest_PrefixAndOrder(t *testing.T) {
	if got := names(suggestions("/")); len(got) != len(commands) || got[0] != commands[0].name {
		t.Errorf("/ suggests %v", got)
	}
	if got := names(suggestions("/re")); strings.Join(got, " ") != "/resume /rename" {
		t.Errorf("/re suggests %v, want /resume /rename in table order", got)
	}
	if got := names(suggestions(" /re")); strings.Join(got, " ") != "/resume /rename" {
		t.Errorf("a leading blank must not hide the suggestions (ParseCommand trims): %v", got)
	}
	for _, line := range []string{"", "hello", "//", "//re", "/resume ", "/re\nx", "/zzz"} {
		if got := suggestions(line); len(got) != 0 {
			t.Errorf("%q suggests %v, want none", line, names(got))
		}
	}
	if !isCommandLine("/rename api work") || !isCommandLine("/re") || isCommandLine("/zz x") || isCommandLine("hello") || isCommandLine("//x") {
		t.Error("isCommandLine wrong")
	}
}

func TestSuggest_RowsAboveTheInputAndKeys(t *testing.T) {
	r := newRig(t)
	r.typeIn("/re")
	rows := r.rows()
	yRes, yRen := rowOf(r, "/resume "), rowOf(r, "/rename <name>")
	if yRes < 0 || yRen != yRes+1 || !strings.Contains(rows[yRes], "the same as /sessions") {
		t.Fatalf("suggestion rows missing or out of order:\n%s", r.view())
	}
	boxTop := -1
	for y, row := range rows {
		if strings.HasPrefix(row, "╭") {
			boxTop = y
		}
	}
	if boxTop != yRen+1 {
		t.Errorf("the rows are not right above the input box (box at %d, last row %d)", boxTop, yRen)
	}
	if r.m.suggSel != 0 {
		t.Errorf("first selected, got %d", r.m.suggSel)
	}
	r.send(key(tea.KeyDown, 0))
	if _, sel := r.m.current(); sel != 1 {
		t.Errorf("Down: selected %d", sel)
	}
	r.send(key(tea.KeyDown, 0)) // wraps
	if _, sel := r.m.current(); sel != 0 {
		t.Errorf("Down wraps: selected %d", sel)
	}
	r.send(key(tea.KeyUp, 0))
	r.send(key(tea.KeyTab, 0))
	if r.m.ta.Value() != "/rename " {
		t.Errorf("Tab completed to %q, want \"/rename \"", r.m.ta.Value())
	}
	if r.m.suggesting() || rowOf(r, "/resume ") >= 0 {
		t.Error("suggestions still shown after the completion with a space")
	}
}

func TestSuggest_TabWithoutArgumentsAddsNoSpace(t *testing.T) {
	r := newRig(t)
	r.typeIn("/he")
	r.send(key(tea.KeyTab, 0))
	if r.m.ta.Value() != "/help" {
		t.Errorf("Tab completed to %q", r.m.ta.Value())
	}
	r.send(key(tea.KeyEnter, 0))
	r.wantLine(result{}, "/resume ") // /help ran
}

func TestSuggest_ClickAndHover(t *testing.T) {
	r := newRig(t)
	r.typeIn("/re")
	y := rowOf(r, "/rename <name>")
	r.hoverTo(4, y)
	if got := underlined(r.rawRows()); len(got) != 1 || got[0] != y {
		t.Errorf("hover: underlined %v, want [%d]", got, y)
	}
	r.click(4, y)
	if r.m.ta.Value() != "/rename " {
		t.Errorf("click completed to %q", r.m.ta.Value())
	}
}

func TestSuggest_EnterSubmitsAsTyped(t *testing.T) {
	r := newRig(t)
	r.typeIn("/re")
	r.send(key(tea.KeyEnter, 0))
	r.wantLine(result{}, "unknown command /re (see /help)")
	if len(r.ag.submitted) != 0 || r.m.ta.Value() != "" {
		t.Errorf("submitted %v, input %q", r.ag.submitted, r.m.ta.Value())
	}
}

func TestSuggest_AllShownAndRoomGivesWay(t *testing.T) {
	r := newRig(t)
	r.typeIn("/")
	n := 0
	for _, row := range r.rows() {
		if strings.HasPrefix(row, "  /") {
			n++
		}
	}
	if n != len(commands) {
		t.Errorf("%d suggestion rows, want all %d", n, len(commands))
	}
	for _, h := range []int{8, 6, 5} {
		r.send(tea.WindowSizeMsg{Width: 60, Height: h})
		wantFrame(t, r, 60, h)
	}
	r.send(tea.WindowSizeMsg{Width: 60, Height: 6}) // 3 box rows + status + 1 list row: one extra row at most
	if n := len(r.m.bottom().extra); n > 1 {
		t.Errorf("%d extra rows at height 6", n)
	}
}

// F9 of the code review: a row that does not fit is neither selected nor completed.
func TestSuggest_OnlyVisibleRowsSelectable(t *testing.T) {
	r := newRig(t)
	r.send(tea.WindowSizeMsg{Width: 80, Height: 8}) // 3 rows of room above the box
	r.typeIn("/")
	shown, _ := r.m.current()
	if len(shown) != 3 || len(r.m.bottom().extra) != 3 {
		t.Fatalf("%d selectable, %d shown", len(shown), len(r.m.bottom().extra))
	}
	for range 5 {
		r.send(key(tea.KeyDown, 0))
	}
	r.send(key(tea.KeyTab, 0))
	if v := r.m.ta.Value(); v != shown[2].name {
		t.Errorf("Tab completed %q, want the third visible row %q", v, shown[2].name)
	}
}

func TestSuggest_HiddenWhileAsking(t *testing.T) {
	r := newRig(t)
	r.typeIn("/re")
	r.send(askMsg{call: chat.ToolCall{Name: "shell", Args: `{"command":"ls"}`}, reply: make(chan agent.Approval, 1)})
	if r.m.suggesting() || rowOf(r, "/resume ") >= 0 {
		t.Error("suggestions shown beside a confirmation prompt")
	}
	r.m.denyAsk("")
}

// accentSGR is the escape sequence lipgloss emits for the Accent foreground.
func accentSGR() string {
	s := lipgloss.NewStyle().Foreground(lipgloss.Color(mdstream.Colors.Accent)).Render("x")
	return strings.SplitN(s, "x", 2)[0]
}

func TestInput_CommandHighlighted(t *testing.T) {
	r := newRig(t)
	boxRow := func() string { // the first row inside the input box, with its escape sequences
		for _, row := range r.rawRows() {
			if strings.HasPrefix(ansi.Strip(row), "│") {
				return row
			}
		}
		return ""
	}
	r.typeIn("hello")
	if strings.Contains(boxRow(), accentSGR()) {
		t.Errorf("plain text highlighted: %q", boxRow())
	}
	r.send(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}) // clears the input
	r.typeIn("/re")
	if !strings.Contains(boxRow(), accentSGR()) {
		t.Errorf("a command prefix is not highlighted: %q", boxRow())
	}
	r.typeIn("name api work")
	if !strings.Contains(boxRow(), accentSGR()) {
		t.Errorf("a command with arguments is not highlighted: %q", boxRow())
	}
	r.send(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	r.typeIn("/zzz")
	if strings.Contains(boxRow(), accentSGR()) {
		t.Errorf("an unknown command is highlighted: %q", boxRow())
	}
}
