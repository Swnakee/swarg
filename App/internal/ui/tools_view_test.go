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

func toolStart(id, name, label string) agent.Event {
	return agent.Event{Kind: agent.EvToolStart, Call: chat.ToolCall{ID: id, Name: name, Args: `{"path":"x"}`}, Label: label}
}

func toolEnd(id, name, label, brief, output string, err error) agent.Event {
	return agent.Event{Kind: agent.EvToolEnd, Call: chat.ToolCall{ID: id, Name: name, Args: `{"path":"x"}`},
		Label: label, Brief: brief, Output: output, Err: err}
}

func TestToolLine_CollapsedLines(t *testing.T) {
	head, tail := ToolLine(3, "read_file", "README.md", "42 lines", nil)
	if head != "● read_file(README.md)  #3" || tail != "  └ 42 lines" {
		t.Errorf("got %q %q", head, tail)
	}
	_, tail = ToolLine(1, "shell", "ls", "ignored", errors.New("boom\nsecond"))
	if tail != "  └ ✘ error: boom" {
		t.Errorf("error tail = %q", tail)
	}
	head, tail = ToolLine(2, "x", "a\x1b[31m\nb", "ok\x1b]0;t\x07", nil)
	if strings.ContainsAny(head+tail, "\x1b\x07") || !strings.HasSuffix(head, "#2") {
		t.Errorf("not sanitized: %q %q", head, tail)
	}
}

func TestModel_ToolSpinnerAndRows(t *testing.T) {
	r := newRig(t)
	r.events(started("q"), toolStart("a", "read_file", "README.md"))
	if v := r.view(); !strings.Contains(v, "Running read_file(README.md)…") || !strings.Contains(v, "● read_file(README.md)  #1 ◐") {
		t.Errorf("single tool spinner:\n%s", v)
	}
	r.events(toolStart("b", "list_dir", "."), toolStart("c", "grep", "x"))
	if v := r.view(); !strings.Contains(v, "Running 3 tools…") {
		t.Errorf("batch spinner:\n%s", v)
	}
	r.events(toolEnd("a", "read_file", "README.md", "42 lines", "out", nil),
		toolEnd("b", "list_dir", ".", "", "", errors.New("no such dir")),
		toolEnd("c", "grep", "x", "0 matches", "", nil), ended(chat.FinishStop, nil))
	for _, want := range []string{"● read_file(README.md)  #1", "  └ 42 lines", "● list_dir(.)  #2",
		"  └ ✘ error: no such dir", "● grep(x)  #3", "  └ 0 matches"} {
		r.wantLine(result{}, want)
	}
	// numbering continues across Turns
	r.events(started("again"), toolStart("d", "grep", "y"), toolEnd("d", "grep", "y", "1 match", "", nil), ended(chat.FinishStop, nil))
	r.wantLine(result{}, "#4")
}

func TestModel_BlankLineIsARow(t *testing.T) {
	r := newRig(t)
	r.events(started("q"), delta("a\n\nb\n"))
	rows := plainRows(r.m.cv.rows(0, r.m.cv.total()))
	if len(rows) != 4 || strings.TrimSpace(rows[1]) != "a" || rows[2] != "" || strings.TrimSpace(rows[3]) != "b" { // rows[0] is the user echo
		t.Errorf("rows = %q, want echo, a, empty, b", rows)
	}
}

func TestModel_UserTextEventAddsAUserItem(t *testing.T) {
	r := newRig(t)
	r.events(started("q"), agent.Event{Kind: agent.EvUserText, Text: "also this\x1b[2J"})
	r.wantLine(result{}, "▸ also this[2J")
	if it := r.m.cv.at(r.m.cv.count() - 1); it.kind != itemUser {
		t.Errorf("last item kind %v", it.kind)
	}
}

func TestModel_CtrlOAndToolCommand(t *testing.T) {
	r := newRig(t)
	r.send(key('o', tea.ModCtrl))
	r.wantLine(result{}, "no tool calls yet")
	r.events(started("q"),
		toolStart("a", "read_file", "one"), toolEnd("a", "read_file", "one", "1 line", "first output", nil),
		toolStart("b", "grep", "two"), toolEnd("b", "grep", "two", "1 match", "second output", nil))
	r.send(key('o', tea.ModCtrl)) // toggles the LAST block only
	if got := r.convText(); !strings.Contains(got, "second output") || strings.Contains(got, "first output") {
		t.Errorf("ctrl+o must open the last block only:\n%s", got)
	}
	r.send(key('o', tea.ModCtrl))
	if got := r.convText(); strings.Contains(got, "second output") {
		t.Errorf("ctrl+o again must close it:\n%s", got)
	}
	r.m.ta.SetValue("/tool 1")
	r.send(key(tea.KeyEnter, 0))
	if got := r.convText(); !strings.Contains(got, "first output") {
		t.Errorf("/tool 1 must open block 1:\n%s", got)
	}
	r.m.ta.SetValue("/tool 9")
	r.send(key(tea.KeyEnter, 0))
	r.wantLine(result{}, "no tool call #9")
	for _, bad := range []string{"/tool", "/tool x", "/tool 0", "/tool 1 2"} {
		r.m.ta.SetValue(bad)
		r.send(key(tea.KeyEnter, 0))
		r.wantLine(result{}, "usage: /tool N")
	}
	r.m.ta.SetValue("/clear")
	r.send(key(tea.KeyEnter, 0))
	r.m.ta.SetValue("/tool 1")
	r.send(key(tea.KeyEnter, 0))
	r.wantLine(result{}, "no tool call #1")
	r.send(key('o', tea.ModCtrl))
	if got := r.convText(); strings.Count(got, "no tool calls yet") != 1 {
		t.Errorf("ctrl+o after /clear:\n%s", got)
	}
}

func TestModel_CallHistoryCapped(t *testing.T) {
	r := newRig(t)
	r.events(started("q"))
	for i := 0; i < maxCalls+5; i++ {
		id := fmt.Sprint("c", i)
		r.events(toolStart(id, "grep", "x"), toolEnd(id, "grep", "x", "ok", "", nil))
	}
	if maxCalls != 2000 || len(r.m.calls) != maxCalls || r.m.calls[0].n != 6 {
		t.Errorf("calls %d, first #%d", len(r.m.calls), r.m.calls[0].n)
	}
	if got := r.m.cv.count(); got != maxCalls+5+1 { // the list keeps every block (+ the user echo)
		t.Errorf("list has %d items", got)
	}
	r.m.ta.SetValue("/tool 3")
	r.send(key(tea.KeyEnter, 0))
	r.wantLine(result{}, "no tool call #3")
}
