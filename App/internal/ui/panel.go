package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"swarg/internal/agent"
)

// ---------------------------------------------------------------------------
// Bottom panel: the row under the input box (R-31)
// ---------------------------------------------------------------------------

const (
	panelSep   = " │ "
	glyphAsk   = "○"
	noSpeed    = "– tok/s"
	speedUnit  = " tok/s"
	autoOnText = glyphAuto + " auto"
	autoOff    = glyphAsk + " ask"
)

// Hover ids of the clickable panel items.
const (
	spotModel = "panel:model" // opens the model list
	spotJobs  = "panel:jobs"  // lists the background jobs
)

// panelItem is one item of the Bottom panel, its hover id ("" = not clickable) and its cells
// [x0, x1) on the row.
type panelItem struct {
	text   string
	id     string
	x0, x1 int
}

// panelItems are the items in order: model, context use, Generation speed, auto-approve,
// running background jobs (only when there are any), and the transient copy note. Cells are
// counted on the uncut row; an item cut by the width keeps only its visible cells.
func (m *model) panelItems() []panelItem {
	auto := autoOff
	if m.d.Approver.Auto() {
		auto = autoOnText
	}
	items := []panelItem{
		{text: m.modelName, id: spotModel},
		{text: agent.FormatMeter(m.used, m.limit, m.est)},
		{text: m.speedText()},
		{text: auto},
	}
	if m.bgRunning > 0 {
		items = append(items, panelItem{text: fmt.Sprintf("%s %d bg", glyphBg, m.bgRunning), id: spotJobs})
	}
	if m.copyNote != "" {
		items = append(items, panelItem{text: m.copyNote})
	}
	x := 0
	for i := range items {
		items[i].x0 = x
		x += ansi.StringWidth(items[i].text)
		items[i].x1 = min(x, m.width)
		items[i].x0 = min(items[i].x0, m.width)
		x += ansi.StringWidth(panelSep)
	}
	return items
}

// panelLine renders the Bottom panel, cut to the width.
func (m *model) panelLine() string {
	var parts []string
	for _, it := range m.panelItems() {
		parts = append(parts, it.text)
	}
	return noticeStyle.Render(ansi.Truncate(strings.Join(parts, panelSep), m.width, "…"))
}

// hotPanel registers the clickable panel items, on screen row y, as hover spots.
func (m *model) hotPanel(y int) {
	for _, it := range m.panelItems() {
		if it.id != "" {
			m.hv.add(it.id, y, it.x0, it.x1)
		}
	}
}

// panelClick runs the action of a clicked panel item (its hover id).
func (m *model) panelClick(id string) tea.Cmd {
	switch id {
	case spotModel:
		return m.chooseModel("")
	case spotJobs:
		m.listJobs()
	}
	return nil
}

// ---------------------------------------------------------------------------
// Generation speed (R-29): the last measured value and the live estimate
// ---------------------------------------------------------------------------

// liveMinSpan is how long text must stream before the live estimate is shown.
const liveMinSpan = 500 // milliseconds

// fmtSpeed renders tokens per second: one decimal below 10.
func fmtSpeed(v float64) string {
	if v < 10 {
		return fmt.Sprintf("%.1f%s", v, speedUnit)
	}
	return fmt.Sprintf("%.0f%s", v, speedUnit)
}

// speedText is the panel item: the live estimate while text streams ("~N tok/s"), else the last
// measured Generation speed, "– tok/s" before any.
func (m *model) speedText() string {
	if v, ok := m.liveSpeed(); ok {
		return "~" + fmtSpeed(v)
	}
	if !m.hasSpeed {
		return noSpeed
	}
	return fmtSpeed(m.speed)
}

// noteDelta counts a streamed text chunk for the live estimate (one chunk is about one token
// with mlx_lm.server).
func (m *model) noteDelta() {
	if m.liveN == 0 {
		m.liveFrom = m.clock()
	}
	m.liveN++
}

// resetLive ends the live estimate (the Step's stream ended or a tool started).
func (m *model) resetLive() { m.liveN = 0 }

// liveSpeed is the live estimate of the Step streaming now: chunks per second since its first
// chunk; ok is false before liveMinSpan or without a stream.
func (m *model) liveSpeed() (float64, bool) {
	if m.liveN < 2 {
		return 0, false
	}
	d := m.now.Sub(m.liveFrom)
	if d.Milliseconds() < liveMinSpan {
		return 0, false
	}
	return float64(m.liveN-1) / d.Seconds(), true
}
