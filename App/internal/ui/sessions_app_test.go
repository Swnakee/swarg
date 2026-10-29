package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"

	"swarg/internal/agent"
	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Sessions inside the app: the picker in the list area, open = switch, delete, rename
// (T3 ui item 1; R-33, R-34, R-32)
// ---------------------------------------------------------------------------

// twoSessions fills the fake with a newer session "a" and an older "b", both with a history.
func twoSessions(ss *fakeSessions) {
	ss.rows = []SessionRow{
		{ID: "b", Title: "t-b", Model: "m", Time: time.Unix(100, 0), Messages: 2},
		{ID: "a", Title: "t-a", Model: "m", Time: time.Unix(200, 0), Messages: 1},
	}
	ss.hist["a"] = []chat.Message{{Role: chat.RoleUser, Content: "from a"}}
	ss.hist["b"] = []chat.Message{{Role: chat.RoleUser, Content: "from b"}, {Role: chat.RoleAssistant, Content: "b said"}}
}

// pickerRow is the screen row showing text while the picker is open (draws first).
func pickerRow(t *testing.T, r *rig, text string) int {
	t.Helper()
	y := rowOf(r, text)
	if y < 0 {
		t.Fatalf("no picker row with %q:\n%s", text, r.view())
	}
	return y
}

func TestPickerInApp_EnterOpensTheSelectedSession(t *testing.T) {
	r, ag, ss := newSessRig(t)
	twoSessions(ss)
	r.events(started("stale"), ended(chat.FinishStop, nil))
	r.cmd("/sessions")
	r.send(key(tea.KeyDown, 0)) // a (newest) -> b
	r.send(key(tea.KeyEnter, 0))
	if len(ss.loads) != 1 || ss.loads[0] != "b" || len(ag.loaded) != 1 || ag.loadOpts != 3 {
		t.Fatalf("opens %v, agent loads %d (opts %d)", ss.loads, len(ag.loaded), ag.loadOpts)
	}
	if r.m.pick != nil {
		t.Error("the picker stayed open after a successful open")
	}
	text := r.convText()
	for _, want := range []string{"from b", "b said", "resumed session t-b"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "stale") {
		t.Error("the old list was kept")
	}
}

func TestPickerInApp_ClickOnARowOpensIt(t *testing.T) {
	r, _, ss := newSessRig(t)
	twoSessions(ss)
	r.cmd("/sessions")
	y := pickerRow(t, r, "t-b")
	r.click(4, y)
	if len(ss.loads) != 1 || ss.loads[0] != "b" || r.m.pick != nil {
		t.Errorf("click: opens %v, picker open %v", ss.loads, r.m.pick != nil)
	}
}

func TestPickerInApp_DeleteByMouseNeedsAConfirmClick(t *testing.T) {
	r, _, ss := newSessRig(t)
	twoSessions(ss)
	r.cmd("/sessions")
	y := pickerRow(t, r, "t-b")
	if !strings.HasSuffix(strings.TrimRight(r.rows()[y], " "), pickDelete) {
		t.Fatalf("row has no delete button: %q", r.rows()[y])
	}
	r.click(r.m.width-1, y) // the ✕
	if !r.m.pick.confirm || len(ss.deleted) != 0 {
		t.Fatalf("✕ click: confirm %v, deleted %v", r.m.pick.confirm, ss.deleted)
	}
	foot := pickerRow(t, r, btnCancel)
	x := strings.Index(r.rows()[foot], btnCancel)
	r.click(x+1, foot)
	if r.m.pick.confirm || len(ss.deleted) != 0 {
		t.Fatalf("[n] cancel: confirm %v, deleted %v", r.m.pick.confirm, ss.deleted)
	}
	r.click(r.m.width-1, y)
	foot = pickerRow(t, r, btnDelete)
	x = strings.Index(r.rows()[foot], btnDelete)
	r.click(x+1, foot)
	if len(ss.deleted) != 1 || ss.deleted[0] != "b" {
		t.Fatalf("[y] delete: deleted %v, want [b]", ss.deleted)
	}
	if strings.Contains(r.view(), "t-b") {
		t.Error("the deleted row is still shown")
	}
	if !strings.Contains(r.view(), "session deleted") {
		t.Error("no confirmation note")
	}
}

func TestPickerInApp_DeleteErrorKeepsTheRow(t *testing.T) {
	r, _, ss := newSessRig(t)
	twoSessions(ss)
	ss.deleteErr = errors.New("permission denied")
	r.cmd("/sessions")
	r.send(ctrlD())
	r.send(ptype("y"))
	if len(ss.deleted) != 1 || !strings.Contains(r.view(), "delete failed: permission denied") || !strings.Contains(r.view(), "t-a") {
		t.Errorf("deleted %v:\n%s", ss.deleted, r.view())
	}
}

func TestPickerInApp_CurrentSessionIsMarkedAndNeverDeleted(t *testing.T) {
	r, _, ss := newSessRig(t)
	twoSessions(ss)
	ss.current = "a"
	r.cmd("/sessions")
	ya, yb := pickerRow(t, r, "t-a"), pickerRow(t, r, "t-b")
	rows := r.rows()
	if !strings.Contains(rows[ya], pickCurrent) || strings.Contains(rows[ya], pickDelete) {
		t.Errorf("current row: %q", rows[ya])
	}
	if strings.Contains(rows[yb], pickCurrent) || !strings.HasSuffix(strings.TrimRight(rows[yb], " "), pickDelete) {
		t.Errorf("other row: %q", rows[yb])
	}
	r.send(ctrlD()) // the cursor is on a (newest)
	if r.m.pick.confirm || len(ss.deleted) != 0 || !strings.Contains(r.view(), "cannot be deleted") {
		t.Errorf("Ctrl+D on the current session: confirm %v deleted %v:\n%s", r.m.pick.confirm, ss.deleted, r.view())
	}
	r.click(r.m.width-1, ya) // no ✕ there: nothing happens
	if r.m.pick.confirm || len(ss.loads) != 0 {
		t.Error("a click at the end of the current row did something")
	}
}

func TestPickerInApp_BusyKeepsThePickerAndNotes(t *testing.T) {
	r, _, ss := newSessRig(t)
	twoSessions(ss)
	r.events(started("q"))
	r.cmd("/sessions")
	r.send(key(tea.KeyEnter, 0))
	if r.m.pick == nil || len(ss.loads) != 0 {
		t.Fatalf("busy: picker open %v, opens %v", r.m.pick != nil, ss.loads)
	}
	if !strings.Contains(r.view(), waitTurnText) {
		t.Errorf("the reason is not in the picker footer:\n%s", r.view())
	}
	r.events(ended(chat.FinishStop, nil))
	ss.loadErr = errors.New("corrupt")
	r.send(key(tea.KeyEnter, 0))
	if r.m.pick == nil || !strings.Contains(r.view(), "resume: corrupt") {
		t.Errorf("open error: picker open %v:\n%s", r.m.pick != nil, r.view())
	}
}

func TestPickerInApp_HoverOnRowsAndButtons(t *testing.T) {
	r, _, ss := newSessRig(t)
	twoSessions(ss)
	r.cmd("/sessions")
	y := pickerRow(t, r, "t-b")
	r.hoverTo(4, y)
	if got := underlined(r.rawRows()); len(got) != 1 || got[0] != y {
		t.Errorf("row hover: underlined %v, want [%d]", got, y)
	}
	r.hoverTo(r.m.width-2, y) // the "  ✕" target (three cells)
	if got := underlinedText(r.rawRows()[y]); strings.TrimSpace(got) != pickDelete {
		t.Errorf("the delete button hover underlines %q, want just the ✕", got)
	}
	r.click(r.m.width-1, y)
	foot := pickerRow(t, r, btnDelete)
	x := strings.Index(r.rows()[foot], btnDelete)
	r.hoverTo(x+2, foot)
	if got := underlined(r.rawRows()); len(got) != 1 || got[0] != foot {
		t.Errorf("button hover: underlined %v, want [%d]", got, foot)
	}
}

// underlinedText is the plain text between the first underline on and the underline off.
func underlinedText(raw string) string {
	_, after, ok := strings.Cut(raw, underlineOn)
	if !ok {
		return ""
	}
	mid, _, _ := strings.Cut(after, underlineOff)
	return ansi.Strip(mid)
}

// F1 of the code review: a footer cut off by a short screen must leave no click target behind.
func TestPickerInApp_CutFooterLeavesNoDeleteTarget(t *testing.T) {
	r, _, ss := newSessRig(t)
	twoSessions(ss)
	r.send(tea.WindowSizeMsg{Width: 80, Height: 6}) // list area of 2 rows: header + one row, no footer
	r.cmd("/sessions")
	r.send(ctrlD())
	if !r.m.pick.confirm {
		t.Fatal("no confirmation pending")
	}
	rows := r.rows()
	for y := 0; y < len(rows); y++ {
		for _, x := range []int{0, 25, 40, 79} {
			r.click(x, y)
		}
	}
	if len(ss.deleted) != 0 {
		t.Errorf("a click on a row without a visible button deleted %v", ss.deleted)
	}
	wantFrame(t, r, 80, 6)
}

// A pending forced quit (Ctrl+C twice) is not swallowed by the open picker.
func TestPickerInApp_SecondCtrlCStillQuits(t *testing.T) {
	r, _, ss := newSessRig(t)
	twoSessions(ss)
	r.cmd("/sessions")
	r.m.quitting = true
	if res := r.send(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}); !res.quit {
		t.Error("the second ctrl+c did not quit")
	}
}

func TestPickerInApp_KeysGoToThePickerNotTheInput(t *testing.T) {
	r, _, ss := newSessRig(t)
	twoSessions(ss)
	r.cmd("/sessions")
	for _, ch := range "t-b" {
		r.send(ptype(string(ch)))
	}
	if r.m.pick.filter != "t-b" || len(r.m.pick.vis) != 1 {
		t.Errorf("filter %q, %d rows", r.m.pick.filter, len(r.m.pick.vis))
	}
	if r.m.ta.Value() != "" {
		t.Errorf("typed text reached the input: %q", r.m.ta.Value())
	}
	r.send(key(tea.KeyEnter, 0))
	if len(ss.loads) != 1 || ss.loads[0] != "b" {
		t.Errorf("Enter on the filtered row: opens %v", ss.loads)
	}
}

func TestPickerInApp_ConfirmationPromptClosesIt(t *testing.T) {
	r, _, ss := newSessRig(t)
	twoSessions(ss)
	r.cmd("/sessions")
	reply := make(chan agent.Approval, 1)
	r.send(askMsg{call: chat.ToolCall{Name: "shell", Args: `{"command":"ls"}`}, reply: reply})
	if r.m.pick != nil {
		t.Error("an approval prompt left the picker open")
	}
	r.m.denyAsk("")
}

func TestPickerInApp_WheelScrollsTheList(t *testing.T) {
	r, _, ss := newSessRig(t)
	for i := 0; i < 50; i++ {
		ss.rows = append(ss.rows, SessionRow{ID: fmt.Sprint("id", i), Title: fmt.Sprint("t", i), Model: "m", Time: time.Unix(int64(1000-i), 0)})
	}
	r.cmd("/sessions")
	r.view()
	r.send(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if r.m.pick.top != wheelRows || r.m.sc != (scrollState{follow: true}) {
		t.Errorf("wheel: picker top %d, list scroll %+v", r.m.pick.top, r.m.sc)
	}
}

func TestPickerInApp_FitsTheListArea(t *testing.T) {
	r, _, ss := newSessRig(t)
	twoSessions(ss)
	r.cmd("/sessions")
	for _, h := range []int{40, 12, 6} {
		r.send(tea.WindowSizeMsg{Width: 60, Height: h})
		wantFrame(t, r, 60, h)
	}
}

// ---------------------------------------------------------------------------
// /rename
// ---------------------------------------------------------------------------

func TestRenameCommand_NamesTheCurrentSession(t *testing.T) {
	r, _, ss := newSessRig(t)
	r.cmd("/rename  api   work ")
	if len(ss.renamed) != 1 || ss.renamed[0] != "api work" {
		t.Fatalf("renamed %v", ss.renamed)
	}
	r.wantLine(result{}, "session renamed to api work")
	r.cmd("/rename")
	r.wantLine(result{}, "usage: /rename <name>")
	ss.renameErr = errors.New("the name is blank")
	r.cmd("/rename x")
	r.wantLine(result{}, "rename: the name is blank")
}
