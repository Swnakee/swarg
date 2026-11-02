package guard

import (
	"encoding/json"
	"regexp"
)

// ---------------------------------------------------------------------------
// Repeat guard (R-24): stops a model that keeps sending the same call with the same outcome
// ---------------------------------------------------------------------------

const (
	repeatWarnAt = 2 // identical outcomes in a row that add a note to the ToolResult
	repeatStopAt = 3 // identical outcomes in a row that end the Turn

	cycleKeep   = 6 // outcomes of the Turn the cycle rule looks back over
	cycleNoteAt = 2 // full periods of a cycle that add a note
	cycleStopAt = 3 // full periods of a cycle that end the Turn

	RepeatNote    = "\n[this exact call failed 2 times in a row; do not repeat it - try something else or answer]"
	sameNote      = "\n[this exact call returned the same result 2 times in a row; do not repeat it - use the result or answer]"
	cycleNote     = "\n[these 2 calls keep repeating with the same results; do not repeat them - use the results or answer]"
	repeatWarning = "stopped: the same tool call kept failing"
	cycleWarning  = "stopped: the same tool calls kept repeating"
	sameWarning   = "stopped: the same tool call kept returning the same result"
)

// pollTools wait on something that changes by itself: the same answer again is normal.
var pollTools = map[string]bool{"shell_output": true}

// bgID matches a BackgroundJob id; the cycle rule ignores which job a call started.
var bgID = regexp.MustCompile(`bg\d+`)

// Call identifies one tool call: its name and raw JSON arguments.
type Call struct{ Name, Args string }

// Outcome is what a call produced. Denied means the user refused it.
type Outcome struct {
	Output string
	Err    error
	Denied bool
}

// cycleEntry is one outcome in the cycle window: its key (job ids erased) and whether it is the
// success of a polling Tool.
type cycleEntry struct {
	key  string
	poll bool
}

// Repeat counts, within one Turn, consecutive identical calls (same tool, same canonical
// arguments) with the same outcome (the same error text, or for a success the same output), and
// A-B-A-B cycles of two different calls over the last cycleKeep outcomes (R-44).
// The zero value is ready; it is used by one goroutine.
type Repeat struct {
	key       string // tool name + canonical args + outcome of the current streak
	n         int
	failed    bool            // the streak is of failures
	recent    []cycleEntry    // the last cycleKeep outcomes of the Turn, polls included
	cycleStop bool            // the last observe ended the Turn because of a cycle
	inStep    map[string]bool // call+outcome keys already counted in this Step (see NewStep)
}

// Observe records the outcome of one call. It returns the note to append to the ToolResult
// and whether the Turn must end. A denial, a different call or a different outcome resets the
// streak; so does a success of a polling Tool. A denial also clears the cycle window.
func (g *Repeat) Observe(c Call, o Outcome) (note string, stop bool) {
	g.cycleStop = false
	if o.Denied {
		g.key, g.n, g.recent = "", 0, nil
		return "", false
	}
	outcome := "ok\x00" + o.Output
	if o.Err != nil {
		outcome = "err\x00" + o.Err.Error()
	}
	prefix := c.Name + "\x00" + canonicalArgs(c.Args) + "\x00"
	if g.inStep != nil {
		if g.inStep[prefix+outcome] {
			return "", false
		}
		g.inStep[prefix+outcome] = true
	}
	poll := o.Err == nil && pollTools[c.Name]
	g.recent = append(g.recent, cycleEntry{prefix + bgID.ReplaceAllString(outcome, "bg#"), poll})
	if len(g.recent) > cycleKeep {
		g.recent = g.recent[1:]
	}
	if periods := g.periods(); periods >= cycleNoteAt {
		g.cycleStop = periods >= cycleStopAt
		note, stop = cycleNote, g.cycleStop
	}
	if poll {
		g.key, g.n = "", 0
		return note, stop
	}
	key := prefix + outcome
	if key != g.key {
		g.key, g.n, g.failed = key, 0, o.Err != nil
	}
	g.n++
	if g.n >= repeatWarnAt {
		note = sameNote
		if g.failed {
			note = RepeatNote
		}
	}
	return note, stop || g.n >= repeatStopAt
}

// periods counts the full periods of the two-call cycle that ends the window. It is 0 unless
// the two calls differ and at least one is not a polling success.
func (g *Repeat) periods() int {
	n := len(g.recent)
	if n < 2 {
		return 0
	}
	a, b := g.recent[n-2], g.recent[n-1]
	if a == b || (a.poll && b.poll) {
		return 0
	}
	periods := 1
	for i := n - 4; i >= 0 && g.recent[i] == a && g.recent[i+1] == b; i -= 2 {
		periods++
	}
	return periods
}

// Warning is the message for a Turn the guard ended.
func (g *Repeat) Warning() string {
	if g.cycleStop {
		return cycleWarning
	}
	if g.failed {
		return repeatWarning
	}
	return sameWarning
}

// canonicalArgs re-encodes a JSON object so that key order and spacing do not matter.
func canonicalArgs(args string) string {
	var v any
	if json.Unmarshal([]byte(args), &v) != nil {
		return args
	}
	b, err := json.Marshal(v)
	if err != nil {
		return args
	}
	return string(b)
}

// NewStep starts the calls of a new Step. Within a Step the same call with the same outcome
// counts once (a batch is one decision of the model: it saw no result in between, F12), so
// streaks and cycles grow only across Steps. Without NewStep every observe counts.
func (g *Repeat) NewStep() { g.inStep = map[string]bool{} }
