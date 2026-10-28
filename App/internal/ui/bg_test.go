package ui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

type fakeJobs struct{ rows []JobRow }

func (f *fakeJobs) List() []JobRow { return f.rows }

func ctrlB() tea.KeyPressMsg { return key('b', tea.ModCtrl) }

// ---------------------------------------------------------------------------
// Ctrl+B
// ---------------------------------------------------------------------------

func TestCtrlB_CallsBackgroundOnce(t *testing.T) {
	r := newRig(t)
	r.ag.bgResult = true
	r.events(started("q"), shellStart("a", "sleep 20"))
	r.m.ta.SetValue("ab")
	r.send(ctrlB())
	if r.ag.backgrounds != 1 || r.ag.interrupts != 0 {
		t.Errorf("backgrounds %d, interrupts %d", r.ag.backgrounds, r.ag.interrupts)
	}
	if r.m.ta.Value() != "ab" {
		t.Errorf("input changed: %q", r.m.ta.Value())
	}
}

func TestCtrlB_NothingRunningFallsThrough(t *testing.T) {
	r := newRig(t)
	r.m.ta.SetValue("abc")
	if _, handled := r.m.handleKey(ctrlB()); handled {
		t.Error("ctrl+b with nothing running must reach the textarea")
	}
	r.send(ctrlB())
	r.send(tea.KeyPressMsg{Code: 'X', Text: "X"})
	if got := r.m.ta.Value(); got != "abXc" {
		t.Errorf("input %q, want the cursor moved left by ctrl+b: abXc", got)
	}
}

func TestCtrlB_RunningButNotMovable(t *testing.T) {
	r := newRig(t) // bgResult false: e.g. a read-only tool
	r.events(started("q"), toolStart("a", "read_file", "x"))
	res := r.send(ctrlB())
	r.wantLine(res, "no running command can be moved")
	if r.ag.backgrounds != 1 || r.ag.interrupts != 0 {
		t.Errorf("backgrounds %d, interrupts %d", r.ag.backgrounds, r.ag.interrupts)
	}
}

func TestCtrlB_DoesNotInterrupt(t *testing.T) {
	r := newRig(t)
	r.events(started("q"))
	r.send(ctrlB())
	if r.ag.interrupts != 0 || r.ag.aborts != 0 || r.m.stopping {
		t.Errorf("interrupts %d aborts %d stopping %v", r.ag.interrupts, r.ag.aborts, r.m.stopping)
	}
}

func TestCtrlB_IgnoredDuringAsk(t *testing.T) {
	r := newRig(t)
	r.events(started("q"))
	r.ask(context.Background(), "shell", `{"command":"ls"}`)
	r.m.ta.SetValue("ab")
	r.send(ctrlB())
	if r.ag.backgrounds != 0 || r.m.ask == nil || r.m.ta.Value() != "ab" {
		t.Errorf("backgrounds %d, ask %v, input %q", r.ag.backgrounds, r.m.ask, r.m.ta.Value())
	}
}

// ---------------------------------------------------------------------------
// /bg and the status count
// ---------------------------------------------------------------------------

func TestBg_CommandListsJobs(t *testing.T) {
	r := newRig(t)
	r.m.ta.SetValue("/bg")
	r.wantLine(r.send(key(tea.KeyEnter, 0)), "no background jobs") // Jobs == nil
	r.m.d.Jobs = &fakeJobs{}
	r.m.ta.SetValue("/bg")
	r.wantLine(r.send(key(tea.KeyEnter, 0)), "no background jobs")
	r.m.d.Jobs = &fakeJobs{rows: []JobRow{
		{ID: "bg1", Command: "sleep 99\nsecond line", Running: true},
		{ID: "bg2", Command: "make", ExitCode: 3},
	}}
	r.m.ta.SetValue("/bg")
	res := r.send(key(tea.KeyEnter, 0))
	r.wantLine(res, "bg1  running  sleep 99")
	r.wantLine(res, "bg2  exit 3  make")
	if strings.Contains(r.convText(), "second line") {
		t.Error("only the first line of a command is listed")
	}
	r.m.ta.SetValue("/bg x")
	r.wantLine(r.send(key(tea.KeyEnter, 0)), "usage: /bg")
	if len(r.ag.submitted) != 0 {
		t.Error("/bg must never reach the model")
	}
}

func TestStatus_BackgroundCount(t *testing.T) {
	r := newRig(t)
	jobs := &fakeJobs{rows: []JobRow{{ID: "bg1", Running: true}, {ID: "bg2", Running: true}, {ID: "bg3", ExitCode: 0}}}
	r.m.d.Jobs = jobs
	if strings.Contains(r.view(), "bg") {
		t.Error("count shown before any refresh")
	}
	r.events(started("q"), toolStart("a", "shell", "x"), toolEnd("a", "shell", "x", "background bg2", "", nil))
	if v := r.view(); !strings.Contains(v, "⧗ 2 bg") {
		t.Errorf("status lacks the count after ToolEnd:\n%s", v)
	}
	r.events(ended(chat.FinishStop, nil)) // idle now
	jobs.rows[0].Running = false
	r.send(bgTickMsg{})
	if v := r.view(); !strings.Contains(v, "⧗ 1 bg") {
		t.Errorf("the 1 s tick must refresh while idle:\n%s", v)
	}
	jobs.rows[1].Running = false
	r.send(bgTickMsg{})
	if v := r.view(); strings.Contains(v, "⧗") {
		t.Errorf("count must disappear when no job runs:\n%s", v)
	}
}

func TestStatus_BackgroundTickRunsOnlyWhileJobsRun(t *testing.T) {
	r := newRig(t)
	ticks := 0
	r.m.bgTick = func() tea.Cmd { ticks++; return nil }
	jobs := &fakeJobs{rows: []JobRow{{ID: "bg1", Running: true}}}
	r.m.d.Jobs = jobs
	r.m.ta.SetValue("/bg")
	r.send(key(tea.KeyEnter, 0))
	r.m.ta.SetValue("/bg")
	r.send(key(tea.KeyEnter, 0))
	if ticks != 1 {
		t.Errorf("ticks scheduled %d, want exactly one in flight", ticks)
	}
	r.send(bgTickMsg{})
	if ticks != 2 {
		t.Errorf("a running job must keep the tick going: %d", ticks)
	}
	jobs.rows[0].Running = false
	r.send(bgTickMsg{})
	if ticks != 2 || r.m.bgTicking {
		t.Errorf("tick must stop with the last job: ticks %d, ticking %v", ticks, r.m.bgTicking)
	}
}

// ---------------------------------------------------------------------------
// Approval text
// ---------------------------------------------------------------------------

func TestApprove_ShowsBackgroundAndKillTarget(t *testing.T) {
	r := newRig(t)
	r.m.d.Jobs = &fakeJobs{rows: []JobRow{{ID: "bg1", Command: "npm run dev\nmore", Running: true}}}
	r.events(started("q"))
	r.ask(context.Background(), "shell", `{"command":"sleep 99","background":true}`)
	if v := r.view(); !strings.Contains(v, "sleep 99 (background)") {
		t.Errorf("background not shown:\n%s", v)
	}
	r.m.denyAsk("")
	r.ask(context.Background(), "shell_kill", `{"id":"bg1"}`)
	if v := r.view(); !strings.Contains(v, "bg1: npm run dev") || strings.Contains(v, "more") {
		t.Errorf("kill target not shown:\n%s", v)
	}
	r.m.denyAsk("")
	r.ask(context.Background(), "shell_kill", `{"id":"bg9"}`)
	if v := r.view(); !strings.Contains(v, "Allow shell_kill: bg9?") {
		t.Errorf("unknown job:\n%s", v)
	}
	r.m.d.Jobs = nil
	if got := describeCall(chat.ToolCall{Name: "shell_kill", Args: `{"id":"bg1"}`}, nil); got != "bg1" {
		t.Errorf("nil Jobs: %q", got)
	}
	if got := describeCall(chat.ToolCall{Name: "shell", Args: `{"command":"ls","background":false}`}, nil); got != "ls" {
		t.Errorf("foreground shell: %q", got)
	}
}
