package guard

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func pcall(name, args string) Call { return Call{Name: name, Args: args} }

type result = Outcome

// run feeds n identical outcomes and returns the last note and stop.
func repeatN(g *Repeat, n int, p Call, r result) (note string, stop bool) {
	for i := 0; i < n; i++ {
		note, stop = g.Observe(p, r)
	}
	return note, stop
}

func TestRepeatGuard_SameSuccessNotesAtTwoStopsAtThree(t *testing.T) {
	p, r := pcall("shell", `{"command":"find . -type d | wc -l"}`), result{Output: "1"}
	for _, tc := range []struct {
		n          int
		note, stop bool
	}{{1, false, false}, {2, true, false}, {3, true, true}} {
		var g Repeat
		note, stop := repeatN(&g, tc.n, p, r)
		if (note != "") != tc.note || stop != tc.stop || (note != "" && note != sameNote) {
			t.Errorf("%d calls: note %q stop %v", tc.n, note, stop)
		}
		if stop && g.Warning() != sameWarning {
			t.Errorf("warning %q", g.Warning())
		}
	}
}

func TestRepeatGuard_SameFailureKeepsItsOwnNoteAndWarning(t *testing.T) {
	var g Repeat
	note, stop := repeatN(&g, 3, pcall("read_file", `{"path":"x"}`), result{Err: errors.New("file not found: x")})
	if note != RepeatNote || !stop || g.Warning() != repeatWarning {
		t.Errorf("note %q stop %v warning %q", note, stop, g.Warning())
	}
}

func TestRepeatGuard_ResetCases(t *testing.T) {
	p := pcall("read_file", `{"path":"a.txt"}`)
	for _, tc := range []struct {
		name string
		mid  func(g *Repeat)
	}{
		{"a different output (the file changed)", func(g *Repeat) { g.Observe(p, result{Output: "new"}) }},
		{"a different call", func(g *Repeat) { g.Observe(pcall("list_dir", `{}`), result{Output: "old"}) }},
		{"a denial", func(g *Repeat) { g.Observe(p, result{Denied: true, Err: errors.New("denied")}) }},
		{"a failure", func(g *Repeat) { g.Observe(p, result{Err: errors.New("boom")}) }},
	} {
		var g Repeat
		repeatN(&g, 2, p, result{Output: "old"}) // a streak at the note threshold
		tc.mid(&g)
		// after the reset one more identical call is only the first of a new streak: no note
		if note, _ := repeatN(&g, 1, p, result{Output: "old"}); note != "" {
			t.Errorf("%s: the streak did not restart, note %q", tc.name, note)
		}
	}
}

func TestRepeatGuard_ArgsOrderDoesNotMatter(t *testing.T) {
	var g Repeat
	g.Observe(pcall("grep", `{"a":1,"b":2}`), result{Output: "o"})
	g.Observe(pcall("grep", `{"b":2, "a":1}`), result{Output: "o"})
	if note, _ := g.Observe(pcall("grep", `{"a":1,"b":2}`), result{Output: "o"}); !strings.Contains(note, "same result") {
		t.Errorf("note %q", note)
	}
}

func TestRepeatGuard_PollingToolIsNotARepeat(t *testing.T) {
	var g Repeat
	if _, stop := repeatN(&g, 8, pcall("shell_output", `{"id":"bg1"}`), result{Output: "(no new output)"}); stop {
		t.Error("waiting on a background job was stopped")
	}
	if _, stop := repeatN(&g, 3, pcall("shell_output", `{}`), result{Err: errors.New("no job")}); !stop {
		t.Error("a failing shell_output was not stopped")
	}
}

// ---------------------------------------------------------------------------
// Period-2 cycles (R-44)
// ---------------------------------------------------------------------------

// cycleStep is one call of a scripted sequence and its outcome.
type cycleStep struct {
	p Call
	r result
}

var (
	startJob = cycleStep{pcall("shell", `{"command":"sh slow.sh &"}`), result{Output: "started in background as bg1; read it with shell_output"}}
	pollJob  = cycleStep{pcall("shell_output", `{"id":"bg1"}`), result{Output: "[running]"}}
)

// feed observes the steps in order and returns the notes and the stop flag of each.
func feed(g *Repeat, steps ...cycleStep) (notes []string, stops []bool) {
	for _, s := range steps {
		n, st := g.Observe(s.p, s.r)
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
	var g Repeat
	_, stops := feed(&g, alternate(startJob, pollJob, 6)...)
	for i, stop := range stops {
		if stop != (i == 5) {
			t.Errorf("call %d: stop %v", i+1, stop)
		}
	}
	if g.Warning() != cycleWarning {
		t.Errorf("warning %q", g.Warning())
	}
}

func TestRepeatGuard_Cycle2NoteAt4(t *testing.T) {
	var g Repeat
	notes, _ := feed(&g, alternate(startJob, pollJob, 5)...)
	for i, n := range notes {
		if (n == cycleNote) != (i >= 3) || (n != "" && n != cycleNote) {
			t.Errorf("call %d: note %q", i+1, n)
		}
	}
}

func TestRepeatGuard_BrokenCycle(t *testing.T) {
	other := cycleStep{startJob.p, result{Output: "started in background as bg1; the job failed"}}
	for name, seq := range map[string][]cycleStep{
		"a different outcome inside": {startJob, pollJob, startJob, pollJob, other, pollJob, startJob, pollJob},
		"a different call inside":    {startJob, pollJob, startJob, pollJob, cycleStep{pcall("list_dir", `{}`), result{Output: "x"}}, startJob, pollJob},
	} {
		var g Repeat
		notes, stops := feed(&g, seq...)
		for i := 4; i < len(seq); i++ {
			if notes[i] != "" || stops[i] {
				t.Errorf("%s: call %d: note %q stop %v", name, i+1, notes[i], stops[i])
			}
		}
	}
}

func TestRepeatGuard_IdenticalPollsNotACycle(t *testing.T) {
	var g Repeat
	notes, stops := feed(&g, alternate(pollJob, pollJob, 8)...)
	for i := range notes {
		if notes[i] != "" || stops[i] {
			t.Errorf("poll %d: note %q stop %v", i+1, notes[i], stops[i])
		}
	}
}

func TestRepeatGuard_TwoJobPollsNotACycle(t *testing.T) {
	var g Repeat
	other := cycleStep{pcall("shell_output", `{"id":"bg2"}`), result{Output: "[running]"}}
	notes, stops := feed(&g, alternate(pollJob, other, 8)...)
	for i := range notes {
		if notes[i] != "" || stops[i] {
			t.Errorf("poll %d: note %q stop %v", i+1, notes[i], stops[i])
		}
	}
}

func TestRepeatGuard_FreshJobIdsStillACycle(t *testing.T) {
	var g Repeat
	var seq []cycleStep
	for i := 1; i <= 3; i++ {
		start := startJob
		start.r.Output = strings.Replace(start.r.Output, "bg1", fmt.Sprintf("bg%d", 10+i), 1)
		seq = append(seq, start, pollJob)
	}
	_, stops := feed(&g, seq...)
	if !stops[5] {
		t.Error("restarting the job under a new id each time was not stopped")
	}
}

func TestRepeatGuard_DenialClears(t *testing.T) {
	var g Repeat
	feed(&g, alternate(startJob, pollJob, 4)...)
	g.Observe(startJob.p, result{Denied: true, Err: errors.New("denied")})
	notes, stops := feed(&g, alternate(startJob, pollJob, 2)...)
	if notes[1] != "" || stops[1] {
		t.Errorf("the cycle survived a denial: note %q stop %v", notes[1], stops[1])
	}
}

// F12: a batch is one decision. Identical calls inside one Step count once; streaks and cycles
// grow only across Steps (the model saw the result in between).
func TestRepeatGuard_SameCallsInOneStepCountOnce(t *testing.T) {
	var g Repeat
	p, r := pcall("shell", `{"command":"cat /etc/hostname"}`), result{Output: "host"}
	g.NewStep()
	for i := 0; i < 5; i++ {
		if note, stop := g.Observe(p, r); note != "" || stop {
			t.Fatalf("call %d of one Step: note %q stop %v", i+1, note, stop)
		}
	}
	g.NewStep()
	if note, stop := g.Observe(p, r); !strings.Contains(note, "same result") || stop {
		t.Fatalf("second Step: note %q stop %v", note, stop)
	}
	g.NewStep()
	if _, stop := g.Observe(p, r); !stop {
		t.Fatal("third Step: no stop")
	}
}

func TestRepeatGuard_CycleInOneStepCountsOnce(t *testing.T) {
	var g Repeat
	ping, pong := pcall("shell", `{"command":"echo ping"}`), pcall("shell", `{"command":"echo pong"}`)
	g.NewStep()
	for i := 0; i < 5; i++ {
		for _, p := range []Call{ping, pong} {
			if note, stop := g.Observe(p, result{Output: p.Args}); note != "" || stop {
				t.Fatalf("pair %d of one Step: note %q stop %v", i+1, note, stop)
			}
		}
	}
	for step := 2; step <= 3; step++ {
		g.NewStep()
		g.Observe(ping, result{Output: ping.Args})
		if _, stop := g.Observe(pong, result{Output: pong.Args}); stop != (step == 3) {
			t.Fatalf("Step %d: stop %v", step, stop)
		}
	}
}
