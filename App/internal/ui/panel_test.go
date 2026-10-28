package ui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	"swarg/internal/agent"
)

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

// modelAgent is the fake agent plus SetModel (the optional modelSetter part).
type modelAgent struct {
	*fakeAgent
	set []string
	err error
}

func (a *modelAgent) SetModel(name string) error {
	if a.err != nil {
		return a.err
	}
	a.set = append(a.set, name)
	return nil
}

type fakeModels struct {
	ids   []string
	err   error
	calls int
}

func (f *fakeModels) List(context.Context) ([]string, error) { f.calls++; return f.ids, f.err }

// withModels gives the rig a model-setting agent and a model list.
func (r *rig) withModels(ids ...string) (*modelAgent, *fakeModels) {
	ma, fm := &modelAgent{fakeAgent: r.ag}, &fakeModels{ids: ids}
	r.m.d.Agent, r.m.d.Models = ma, fm
	return ma, fm
}

// panelRow is the screen row of the Bottom panel (the last row).
func (r *rig) panelRow() int { return r.m.rowsHeight() - 1 }

// panelText is the Bottom panel row, colours stripped.
func (r *rig) panelText() string { rows := r.rows(); return rows[len(rows)-1] }

// panelX is the first cell of part on the panel row.
func (r *rig) panelX(part string) int {
	r.t.Helper()
	i := strings.Index(r.panelText(), part)
	if i < 0 {
		r.t.Fatalf("panel %q lacks %q", r.panelText(), part)
	}
	return len([]rune(r.panelText()[:i]))
}

// ---------------------------------------------------------------------------
// Items (R-31)
// ---------------------------------------------------------------------------

func TestPanel_Items(t *testing.T) {
	r := newRig(t)
	if got := r.panelText(); got != "qwen │ ~0% · ~0/16k │ – tok/s │ ○ ask" {
		t.Errorf("idle panel %q", got)
	}
	r.m.d.Approver.SetAuto(true)
	r.events(agent.Event{Kind: agent.EvMeter, Used: 5600, Limit: 16000},
		agent.Event{Kind: agent.EvSpeed, Speed: 23.4},
		agent.Event{Kind: agent.EvModel, Text: "big-model"})
	r.m.bgRunning = 2
	r.m.copyNote = "copied 3 lines"
	if got := r.panelText(); got != "big-model │ 35% · 5.6k/16k │ 23 tok/s │ ◉ auto │ ⧗ 2 bg │ copied 3 lines" {
		t.Errorf("busy panel %q", got)
	}
}

func TestPanel_SlowSpeedHasOneDecimal(t *testing.T) {
	r := newRig(t)
	r.events(agent.Event{Kind: agent.EvSpeed, Speed: 7.34})
	if !strings.Contains(r.panelText(), "│ 7.3 tok/s │") {
		t.Errorf("panel %q", r.panelText())
	}
}

func TestPanel_NothingExtra(t *testing.T) {
	r := newRig(t)
	r.events(started("q"), agent.Event{Kind: agent.EvQueue, Queued: 2},
		agent.Event{Kind: agent.EvMeter, Used: 10, Limit: 16000, CachePct: 50, HasCache: true}, compactStarted())
	got := r.panelText()
	for _, extra := range []string{"swarg", "cache", "queued", "compacting"} {
		if strings.Contains(got, extra) {
			t.Errorf("panel %q shows %q", got, extra)
		}
	}
}

// ---------------------------------------------------------------------------
// Clicks
// ---------------------------------------------------------------------------

func TestPanel_ClickModelFetchesTheList(t *testing.T) {
	r := newRig(t)
	_, fm := r.withModels("a", "qwen", "c")
	r.click(r.panelX("qwen")+1, r.panelRow())
	if fm.calls != 1 {
		t.Fatalf("the model list was fetched %d times, want 1", fm.calls)
	}
	r.send(modelsMsg{ids: fm.ids})
	for _, want := range []string{"1   a", "2 ◉ qwen", "3   c", "/model N"} {
		r.wantLine(result{}, want)
	}
}

func TestPanel_ClickJobsListsThem(t *testing.T) {
	r := newRig(t)
	r.m.bgRunning = 1
	r.click(r.panelX("⧗ 1 bg"), r.panelRow())
	r.wantLine(result{}, noJobsText) // the rig has no job source: the list says so
}

func TestPanel_ClickElsewhereDoesNothing(t *testing.T) {
	r := newRig(t)
	_, fm := r.withModels("a")
	n := r.m.cv.count()
	for _, part := range []string{"tok/s", "ask", "│"} {
		r.click(r.panelX(part), r.panelRow())
	}
	r.click(r.panelX("qwen"), r.panelRow()-2) // the input box row
	if fm.calls != 0 || r.m.cv.count() != n {
		t.Errorf("list calls %d, items %d -> %d", fm.calls, n, r.m.cv.count())
	}
}

func TestPanel_DragFromTheModelIsNoClick(t *testing.T) {
	r := newRig(t)
	_, fm := r.withModels("a")
	x := r.panelX("qwen")
	r.press(x, r.panelRow())
	r.release(x+2, r.panelRow())
	if fm.calls != 0 {
		t.Errorf("a release on another cell opened the list")
	}
}

func TestPanel_NarrowScreenCutsSafely(t *testing.T) {
	r := newRig(t)
	r.size(20, 8)
	r.events(agent.Event{Kind: agent.EvModel, Text: "a-very-long-model-name-indeed"})
	_, fm := r.withModels("x")
	wantFrame(t, r, 20, 8)
	for x := 0; x < 40; x++ {
		r.click(x, r.panelRow())
	}
	if fm.calls != 19 && fm.calls != 20 { // only the visible cells of the model item react
		t.Errorf("list fetched %d times for 40 clicks over a 20-cell row", fm.calls)
	}
}

// ---------------------------------------------------------------------------
// /model (R-30)
// ---------------------------------------------------------------------------

func TestModelCmd_SetsDirectly(t *testing.T) {
	r := newRig(t)
	ma, _ := r.withModels()
	r.enter("/model big")
	if len(ma.set) != 1 || ma.set[0] != "big" {
		t.Fatalf("SetModel calls %q", ma.set)
	}
	r.wantLine(result{}, "model: big (used from the next request)")
	if !strings.HasPrefix(r.panelText(), "big │") {
		t.Errorf("panel %q", r.panelText())
	}
}

func TestModelCmd_NumberFromTheList(t *testing.T) {
	r := newRig(t)
	ma, fm := r.withModels("a", "b")
	r.enter("/model")
	r.send(modelsMsg{ids: fm.ids})
	r.enter("/model 2")
	if len(ma.set) != 1 || ma.set[0] != "b" {
		t.Errorf("SetModel calls %q", ma.set)
	}
}

func TestModelCmd_Failures(t *testing.T) {
	t.Run("no model list", func(t *testing.T) {
		r := newRig(t)
		r.enter("/model")
		r.wantLine(result{}, "the model list is not available; use /model <id>")
	})
	t.Run("list error", func(t *testing.T) {
		r := newRig(t)
		r.withModels()
		r.send(modelsMsg{err: errors.New("cannot reach the model server")})
		r.wantLine(result{}, "model list: cannot reach the model server")
	})
	t.Run("empty list", func(t *testing.T) {
		r := newRig(t)
		r.withModels()
		r.send(modelsMsg{})
		r.wantLine(result{}, "the server lists no models")
	})
	t.Run("agent refuses", func(t *testing.T) {
		r := newRig(t)
		ma, _ := r.withModels()
		ma.err = errors.New("agent: blank model id")
		r.enter("/model x")
		r.wantLine(result{}, "model: agent: blank model id")
		if !strings.HasPrefix(r.panelText(), "qwen │") {
			t.Errorf("panel changed after a refusal: %q", r.panelText())
		}
	})
	t.Run("agent cannot", func(t *testing.T) {
		r := newRig(t)
		r.enter("/model x")
		r.wantLine(result{}, "this agent cannot change the model")
	})
	t.Run("two arguments", func(t *testing.T) {
		r := newRig(t)
		r.enter("/model a b")
		r.wantLine(result{}, "usage: /model")
	})
}

// ---------------------------------------------------------------------------
// Live speed on the Progress line (R-29)
// ---------------------------------------------------------------------------

func TestProgress_LiveSpeed(t *testing.T) {
	r := newRig(t)
	r.events(started("q"))
	for i := 0; i < 21; i++ { // 21 chunks, 20 intervals over 2 s
		r.events(delta("w "))
		r.now = r.now.Add(100 * time.Millisecond)
	}
	r.send(tickMsg(r.now.Add(-100 * time.Millisecond)))
	if !strings.Contains(r.view(), "~10 tok/s") {
		t.Errorf("no live speed:\n%s", r.view())
	}
	r.events(agent.Event{Kind: agent.EvSpeed, Speed: 12})
	if strings.Contains(r.view(), "~10 tok/s") || !strings.Contains(r.panelText(), "12 tok/s") {
		t.Errorf("after EvSpeed the live value stays or the panel lacks the measured one:\n%s", r.view())
	}
}

func TestProgress_NoLiveSpeedTooEarly(t *testing.T) {
	r := newRig(t)
	r.events(started("q"), delta("a"), delta("b"))
	r.send(tickMsg(r.now.Add(100 * time.Millisecond)))
	if strings.Contains(r.view(), "~") && strings.Contains(r.view(), "tok/s ·") {
		t.Errorf("a live speed after 0.1 s:\n%s", r.view())
	}
}

// ---------------------------------------------------------------------------
// Readability of the new content in every colour profile
// ---------------------------------------------------------------------------

func TestReadable_WaitingProgressAndPanelInEveryProfile(t *testing.T) {
	for _, p := range profiles {
		t.Run(p.String(), func(t *testing.T) {
			r := newRig(t)
			r.send(tea.ColorProfileMsg{Profile: p})
			r.m.d.Approver.SetAuto(true)
			r.m.bgRunning = 1
			r.events(started("q"), delta("answer"), agent.Event{Kind: agent.EvSpeed, Speed: 20})
			r.enter("сообщение в ожидании")
			view := downsample(r.m.View().Content, p)
			if bad := unreadable(view, p); len(bad) > 0 {
				t.Errorf("unreadable cells:\n  %s", strings.Join(bad, "\n  "))
			}
			if p != colorprofile.ASCII && !strings.Contains(r.view(), waitingMark) {
				t.Errorf("view lacks the waiting mark")
			}
		})
	}
}

func TestPanel_HoverUnderlinesTheClickableItems(t *testing.T) {
	r := newRig(t)
	r.m.bgRunning = 1
	y := r.panelRow()
	for _, part := range []string{"qwen", "⧗ 1 bg"} {
		r.hoverTo(r.panelX(part), y)
		if got := underlined(r.rawRows()); len(got) != 1 || got[0] != y {
			t.Errorf("pointer on %q: underlined rows %v, want [%d]", part, got, y)
		}
	}
	r.hoverTo(r.panelX("tok/s"), y)
	if got := underlined(r.rawRows()); len(got) != 0 {
		t.Errorf("pointer on the speed: underlined rows %v, want none", got)
	}
}

func TestPanel_SpeedLiveWhileStreaming(t *testing.T) {
	r := newRig(t)
	r.events(agent.Event{Kind: agent.EvSpeed, Speed: 30}, started("q"))
	for i := 0; i < 11; i++ { // 10 intervals over 1 s
		r.events(delta("w "))
		r.now = r.now.Add(100 * time.Millisecond)
	}
	r.send(tickMsg(r.now.Add(-100 * time.Millisecond)))
	if !strings.Contains(r.panelText(), "│ ~10 tok/s │") {
		t.Errorf("panel %q, want the live estimate while text streams", r.panelText())
	}
	r.events(agent.Event{Kind: agent.EvSpeed, Speed: 9.5})
	if !strings.Contains(r.panelText(), "│ 9.5 tok/s │") {
		t.Errorf("panel %q, want the measured value after the stream", r.panelText())
	}
}

// A number that is not in the last shown list is no model id: it must never be recorded.
func TestModelCmd_NumberOutsideTheListIsRefused(t *testing.T) {
	r := newRig(t)
	ma, fm := r.withModels("a", "b")
	r.enter("/model 2") // no list shown yet
	r.send(modelsMsg{ids: fm.ids})
	r.enter("/model 7")
	if len(ma.set) != 0 {
		t.Fatalf("SetModel calls %q, want none", ma.set)
	}
	r.wantLine(result{}, "no model 7 in the list")
}
