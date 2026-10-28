package agent

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"swarg/internal/chat"
)

func pcall(name, args string) pendingCall {
	return pendingCall{call: chat.ToolCall{Name: name, Args: args}}
}

// run feeds n identical outcomes and returns the last note and stop.
func repeatN(g *repeatGuard, n int, p pendingCall, r result) (note string, stop bool) {
	for i := 0; i < n; i++ {
		note, stop = g.observe(p, r)
	}
	return note, stop
}

func TestRepeatGuard_SameSuccessNotesAtTwoStopsAtThree(t *testing.T) {
	p, r := pcall("shell", `{"command":"find . -type d | wc -l"}`), result{output: "1"}
	for _, tc := range []struct {
		n          int
		note, stop bool
	}{{1, false, false}, {2, true, false}, {3, true, true}} {
		var g repeatGuard
		note, stop := repeatN(&g, tc.n, p, r)
		if (note != "") != tc.note || stop != tc.stop || (note != "" && note != sameNote) {
			t.Errorf("%d calls: note %q stop %v", tc.n, note, stop)
		}
		if stop && g.warning() != sameWarning {
			t.Errorf("warning %q", g.warning())
		}
	}
}

func TestRepeatGuard_SameFailureKeepsItsOwnNoteAndWarning(t *testing.T) {
	var g repeatGuard
	note, stop := repeatN(&g, 3, pcall("read_file", `{"path":"x"}`), result{err: errors.New("file not found: x")})
	if note != repeatNote || !stop || g.warning() != repeatWarning {
		t.Errorf("note %q stop %v warning %q", note, stop, g.warning())
	}
}

func TestRepeatGuard_ResetCases(t *testing.T) {
	p := pcall("read_file", `{"path":"a.txt"}`)
	for _, tc := range []struct {
		name string
		mid  func(g *repeatGuard)
	}{
		{"a different output (the file changed)", func(g *repeatGuard) { g.observe(p, result{output: "new"}) }},
		{"a different call", func(g *repeatGuard) { g.observe(pcall("list_dir", `{}`), result{output: "old"}) }},
		{"a denial", func(g *repeatGuard) { g.observe(p, result{denied: true, err: errors.New("denied")}) }},
		{"a failure", func(g *repeatGuard) { g.observe(p, result{err: errors.New("boom")}) }},
	} {
		var g repeatGuard
		repeatN(&g, 2, p, result{output: "old"}) // a streak at the note threshold
		tc.mid(&g)
		// after the reset one more identical call is only the first of a new streak: no note
		if note, _ := repeatN(&g, 1, p, result{output: "old"}); note != "" {
			t.Errorf("%s: the streak did not restart, note %q", tc.name, note)
		}
	}
}

func TestRepeatGuard_ArgsOrderDoesNotMatter(t *testing.T) {
	var g repeatGuard
	g.observe(pcall("grep", `{"a":1,"b":2}`), result{output: "o"})
	g.observe(pcall("grep", `{"b":2, "a":1}`), result{output: "o"})
	if note, _ := g.observe(pcall("grep", `{"a":1,"b":2}`), result{output: "o"}); !strings.Contains(note, "same result") {
		t.Errorf("note %q", note)
	}
}

func TestRepeatGuard_PollingToolIsNotARepeat(t *testing.T) {
	var g repeatGuard
	if _, stop := repeatN(&g, 8, pcall("shell_output", `{"id":"bg1"}`), result{output: "(no new output)"}); stop {
		t.Error("waiting on a background job was stopped")
	}
	if _, stop := repeatN(&g, 3, pcall("shell_output", `{}`), result{err: errors.New("no job")}); !stop {
		t.Error("a failing shell_output was not stopped")
	}
}

// ---------------------------------------------------------------------------
// Period-2 cycles (R-44)
// ---------------------------------------------------------------------------

// cycleStep is one call of a scripted sequence and its outcome.
type cycleStep struct {
	p pendingCall
	r result
}

var (
	startJob = cycleStep{pcall("shell", `{"command":"sh slow.sh &"}`), result{output: "started in background as bg1; read it with shell_output"}}
	pollJob  = cycleStep{pcall("shell_output", `{"id":"bg1"}`), result{output: "[running]"}}
)

// feed observes the steps in order and returns the notes and the stop flag of each.
func feed(g *repeatGuard, steps ...cycleStep) (notes []string, stops []bool) {
	for _, s := range steps {
		n, st := g.observe(s.p, s.r)
		notes, stops = append(notes, n), append(stops, st)
	}
	return notes, stops
}

// alternate returns n calls: a, b, a, b, ...
func alternate(a, b cycleStep, n int) (out []cycleStep) {
	for i := 0; i < n; i++ {
		out = append(out, [2]cycleStep{a, b}[i%2])
	}
	return out
}

func TestRepeatGuard_Cycle2Stops(t *testing.T) {
	var g repeatGuard
	_, stops := feed(&g, alternate(startJob, pollJob, 6)...)
	for i, stop := range stops {
		if stop != (i == 5) {
			t.Errorf("call %d: stop %v", i+1, stop)
		}
	}
	if g.warning() != cycleWarning {
		t.Errorf("warning %q", g.warning())
	}
}

func TestRepeatGuard_Cycle2NoteAt4(t *testing.T) {
	var g repeatGuard
	notes, _ := feed(&g, alternate(startJob, pollJob, 5)...)
	for i, n := range notes {
		if (n == cycleNote) != (i >= 3) || (n != "" && n != cycleNote) {
			t.Errorf("call %d: note %q", i+1, n)
		}
	}
}

func TestRepeatGuard_BrokenCycle(t *testing.T) {
	other := cycleStep{startJob.p, result{output: "started in background as bg1; the job failed"}}
	for name, seq := range map[string][]cycleStep{
		"a different outcome inside": {startJob, pollJob, startJob, pollJob, other, pollJob, startJob, pollJob},
		"a different call inside":    {startJob, pollJob, startJob, pollJob, cycleStep{pcall("list_dir", `{}`), result{output: "x"}}, startJob, pollJob},
	} {
		var g repeatGuard
		notes, stops := feed(&g, seq...)
		for i := 4; i < len(seq); i++ {
			if notes[i] != "" || stops[i] {
				t.Errorf("%s: call %d: note %q stop %v", name, i+1, notes[i], stops[i])
			}
		}
	}
}

func TestRepeatGuard_IdenticalPollsNotACycle(t *testing.T) {
	var g repeatGuard
	notes, stops := feed(&g, alternate(pollJob, pollJob, 8)...)
	for i := range notes {
		if notes[i] != "" || stops[i] {
			t.Errorf("poll %d: note %q stop %v", i+1, notes[i], stops[i])
		}
	}
}

func TestRepeatGuard_TwoJobPollsNotACycle(t *testing.T) {
	var g repeatGuard
	other := cycleStep{pcall("shell_output", `{"id":"bg2"}`), result{output: "[running]"}}
	notes, stops := feed(&g, alternate(pollJob, other, 8)...)
	for i := range notes {
		if notes[i] != "" || stops[i] {
			t.Errorf("poll %d: note %q stop %v", i+1, notes[i], stops[i])
		}
	}
}

func TestRepeatGuard_FreshJobIdsStillACycle(t *testing.T) {
	var g repeatGuard
	var seq []cycleStep
	for i := 1; i <= 3; i++ {
		start := startJob
		start.r.output = strings.Replace(start.r.output, "bg1", fmt.Sprintf("bg%d", 10+i), 1)
		seq = append(seq, start, pollJob)
	}
	_, stops := feed(&g, seq...)
	if !stops[5] {
		t.Error("restarting the job under a new id each time was not stopped")
	}
}

func TestRepeatGuard_DenialClears(t *testing.T) {
	var g repeatGuard
	feed(&g, alternate(startJob, pollJob, 4)...)
	g.observe(startJob.p, result{denied: true, err: errors.New("denied")})
	notes, stops := feed(&g, alternate(startJob, pollJob, 2)...)
	if notes[1] != "" || stops[1] {
		t.Errorf("the cycle survived a denial: note %q stop %v", notes[1], stops[1])
	}
}

// F12: a batch is one decision. Identical calls inside one Step count once; streaks and cycles
// grow only across Steps (the model saw the result in between).
func TestRepeatGuard_SameCallsInOneStepCountOnce(t *testing.T) {
	var g repeatGuard
	p, r := pcall("shell", `{"command":"cat /etc/hostname"}`), result{output: "host"}
	g.newStep()
	for i := 0; i < 5; i++ {
		if note, stop := g.observe(p, r); note != "" || stop {
			t.Fatalf("call %d of one Step: note %q stop %v", i+1, note, stop)
		}
	}
	g.newStep()
	if note, stop := g.observe(p, r); !strings.Contains(note, "same result") || stop {
		t.Fatalf("second Step: note %q stop %v", note, stop)
	}
	g.newStep()
	if _, stop := g.observe(p, r); !stop {
		t.Fatal("third Step: no stop")
	}
}

func TestRepeatGuard_CycleInOneStepCountsOnce(t *testing.T) {
	var g repeatGuard
	ping, pong := pcall("shell", `{"command":"echo ping"}`), pcall("shell", `{"command":"echo pong"}`)
	g.newStep()
	for i := 0; i < 5; i++ {
		for _, p := range []pendingCall{ping, pong} {
			if note, stop := g.observe(p, result{output: p.call.Args}); note != "" || stop {
				t.Fatalf("pair %d of one Step: note %q stop %v", i+1, note, stop)
			}
		}
	}
	for step := 2; step <= 3; step++ {
		g.newStep()
		g.observe(ping, result{output: ping.call.Args})
		if _, stop := g.observe(pong, result{output: pong.call.Args}); stop != (step == 3) {
			t.Fatalf("Step %d: stop %v", step, stop)
		}
	}
}
