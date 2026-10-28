package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"

	"swarg/internal/agent"
	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Messages and model
// ---------------------------------------------------------------------------

const (
	tickEvery  = 120 * time.Millisecond
	maxInput   = 6
	defaultCol = 80
)

// eventsMsg is a batch of agent events coalesced by readEvents.
type eventsMsg []agent.Event

// tickMsg drives the spinner and the elapsed time while a turn runs.
type tickMsg time.Time

// model is the Bubble Tea model. It is mutated in place (pointer receiver).
type model struct {
	d      Deps
	ctx    context.Context
	clock  func() time.Time
	tick   func() tea.Cmd // spinner timer; a field so tests need no real timer
	ta     textarea.Model
	cv     *conv // the conversation list: everything on screen above the input
	cur    int   // list index of the streaming assistant item, -1 when none
	width  int
	height int // screen rows; 0 until the first WindowSizeMsg

	busy, stopping, quitting, ticking bool
	gotText                           bool
	turn                              strings.Builder
	started, now                      time.Time
	frame                             int
	progDone, progTotal               int
	used, limit                       int
	est                               bool
	cachePct                          int
	bd                                agent.Breakdown // the last EvMeter breakdown, for /context
	hasBd                             bool
	hasCache                          bool
	compacting                        bool      // a Compaction runs (the status line says so)
	escCount                          int       // Esc presses in this Turn
	calls                             []callRec // tool calls of the session (capped), by number
	runN                              []int     // numbers of the calls that are running
	tasks                             []int     // list indexes of the running Task blocks, outermost first
	outBuf                            map[int][]byte
	nextN, step                       int     // last call number, last step number
	ask                               *askMsg // pending confirmation
	askSince                          time.Time
	sc                                scrollState
	sel                               selection // the highlighted text (content coordinates)
	pr                                press     // the left button, while held
	hv                                hover     // the pointer and the clickable spans of the last draw
	pick                              *picker   // the in-app session picker while it is open
	suggSel                           int       // the selected command suggestion
	typed                             string    // the input at the last update (a change resets suggSel)
	copyNote                          string    // "copied N lines", shown in the status line
	bgRunning                         int       // running background jobs
	bgTicking                         bool
	bgTick                            func() tea.Cmd // job refresh timer; a field so tests need no timer
	waiting                           []string       // Waiting Messages: sent, not taken by the agent yet (oldest first)
	modelName                         string         // the Model choice shown in the Bottom panel
	modelList                         []string       // the last model list shown by /model (for /model N)
	speed                             float64        // the last measured Generation speed (EvSpeed)
	hasSpeed                          bool
	liveN                             int       // text chunks of the Step streaming now (live speed)
	liveFrom                          time.Time // when its first chunk came

	side    []tea.Cmd            // non-blocking commands for this Update
	profile colorprofile.Profile // the terminal's colour profile, Unknown until reported
}

func newModel(ctx context.Context, d Deps, clock func() time.Time) *model {
	ta := textarea.New()
	ta.Prompt, ta.ShowLineNumbers = "", false
	ta.DynamicHeight, ta.MinHeight, ta.MaxHeight, ta.MaxContentHeight = true, 1, maxInput, 1000
	st := ta.Styles()
	st.Cursor.Blink = false
	st.Focused.CursorLine = lipgloss.NewStyle()
	ta.SetStyles(st)
	ta.SetVirtualCursor(false) // the real cursor: View places it in the input box
	ta.Focus()
	if d.Approver == nil {
		d.Approver = NewApprover(d.Yes)
	}
	if d.AutoApprove {
		d.Approver.SetAuto(true)
	}
	m := &model{d: d, ctx: ctx, clock: clock, tick: spinTick, bgTick: bgTick, ta: ta, cv: newConv(), cur: -1,
		outBuf: map[int][]byte{}, sc: scrollState{follow: true}, limit: d.Limit, est: true, modelName: d.Model}
	m.resize(defaultCol, 0)
	m.rebuild(d.History)
	return m
}

// resize applies a new terminal size: the list re-wraps, the scroll anchor (an item) stays.
func (m *model) resize(w, h int) {
	m.width, m.height = w, h
	m.ta.SetWidth(max(w-2, 1))
	m.ta.MaxHeight = maxInput
	if h > 0 {
		m.ta.MaxHeight = min(max(h-4, 1), maxInput) // keeps one list row at the smallest sizes
	}
	m.ta.SetHeight(m.ta.Height())
	m.cv.setWidth(w)
}

// ---------------------------------------------------------------------------
// Run and the event reader
// ---------------------------------------------------------------------------

// Run starts the full-screen terminal UI and blocks until the user quits or ctx ends.
func Run(ctx context.Context, d Deps) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	m := newModel(ctx, d, time.Now)
	p := tea.NewProgram(m, tea.WithContext(ctx))
	m.d.Approver.Bind(p.Send)
	defer m.d.Approver.Bind(nil)
	d.Agent.Start(ctx)
	_, err := p.Run()
	if ctx.Err() != nil && errors.Is(err, tea.ErrProgramKilled) {
		return nil
	}
	return err
}

// readEvents blocks for one event, then drains whatever else is pending, so a burst
// of deltas becomes a single message. It returns nil when ctx ends or the channel closes.
func readEvents(ctx context.Context, ch <-chan agent.Event) tea.Cmd {
	return func() tea.Msg {
		var batch eventsMsg
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-ch:
			if !ok {
				return nil
			}
			batch = append(batch, ev)
		}
		for {
			select {
			case ev, ok := <-ch:
				if !ok {
					return batch
				}
				batch = append(batch, ev)
			default:
				return batch
			}
		}
	}
}

// spinTick schedules the next spinner frame.
func spinTick() tea.Cmd {
	return tea.Tick(tickEvery, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// ---------------------------------------------------------------------------
// Update
// ---------------------------------------------------------------------------

// Init clears the screen (a terminal whose alt screen kept the rows of an earlier program must
// not show them through the rows this UI leaves empty) and starts the event reader.
func (m *model) Init() tea.Cmd { return tea.Batch(tea.ClearScreen, readEvents(m.ctx, m.d.Events)) }

// Update handles one message. Non-blocking commands queued in m.side run beside the
// blocking reader command.
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmds := append([]tea.Cmd{m.update(msg)}, m.side...)
	m.side = nil
	m.settle()
	return m, tea.Batch(cmds...)
}

func (m *model) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if msg.Width != m.width || msg.Height != m.height {
			m.clearSel()
		}
		m.resize(msg.Width, msg.Height)
	case tea.ColorProfileMsg:
		m.profile = msg.Profile
		return nil
	case tea.MouseWheelMsg:
		if m.pick != nil {
			m.pick.wheel(msg)
			return nil
		}
		m.onWheel(msg)
		return nil
	case tea.MouseMsg:
		if m.pick != nil {
			m.pickAct(m.pick.mouse(msg))
			return nil
		}
		return m.onMouse(msg)
	case bgTickMsg:
		m.onBgTick()
		return nil
	case eventsMsg:
		cmd := m.onEvents(msg)
		if cmd != nil {
			return cmd
		}
		return readEvents(m.ctx, m.d.Events)
	case tickMsg:
		return m.onTick(time.Time(msg))
	case askMsg:
		m.denyAsk("")
		m.ask, m.askSince = &msg, m.clock()
		m.pick = nil // a confirmation takes the keys; the picker closes
		return nil
	case modelsMsg:
		m.onModels(msg)
		return nil
	case askGoneMsg:
		if m.ask != nil && m.ask.reply == msg.reply {
			m.ask = nil
		}
		return nil
	case tea.KeyPressMsg:
		if cmd, ok := m.handleKey(msg); ok {
			return cmd
		}
	}
	var cmd tea.Cmd
	m.ta, cmd = m.ta.Update(msg)
	m.styleInput()
	return cmd
}

func (m *model) onTick(t time.Time) tea.Cmd {
	if !m.working() {
		m.ticking = false
		return nil
	}
	m.now, m.frame = t, m.frame+1
	m.cv.setFrame(m.frame)
	return m.tick()
}

// ---------------------------------------------------------------------------
// Agent events
// ---------------------------------------------------------------------------

// onEvents applies a batch of events. It returns tea.Quit when a pending quit completes.
func (m *model) onEvents(evs eventsMsg) tea.Cmd {
	var quit tea.Cmd
	for _, ev := range evs {
		switch ev.Kind {
		case agent.EvTurnStarted:
			m.startTurn(ev.Text)
		case agent.EvUserText:
			m.endLive(false)
			m.takeWaiting(ev.Text)
			m.cv.addUser(ev.Text)
		case agent.EvToolStart:
			m.resetLive()
			m.toolStart(ev)
		case agent.EvToolOutput:
			m.toolOutput(ev)
		case agent.EvToolEnd:
			m.toolEnd(ev)
			m.refreshJobs()
		case agent.EvDelta:
			m.noteDelta()
			m.delta(ev.Text)
		case agent.EvProgress:
			m.progDone, m.progTotal = ev.Done, ev.Total
		case agent.EvDraining:
			m.stopping = true
		case agent.EvTurnEnded:
			if m.endTurn(ev) {
				quit = tea.Quit
			}
		case agent.EvWarning:
			m.say(ev.Text)
		case agent.EvTaskStarted:
			m.taskStart(ev)
		case agent.EvTaskEnded:
			m.taskEnd()
		case agent.EvMeter:
			if ev.Task != "" { // the panel shows the Session's meter
				break
			}
			m.used, m.limit, m.est = ev.Used, ev.Limit, ev.Estimated
			m.cachePct, m.hasCache = ev.CachePct, ev.HasCache
			m.bd, m.hasBd = ev.Breakdown, true
		case agent.EvModel:
			m.modelName = ev.Text
		case agent.EvSpeed:
			m.speed, m.hasSpeed = ev.Speed, true
			m.resetLive()
		case agent.EvQueue: // Waiting Messages show the queue
		default:
			if m.compactEvent(ev) {
				quit = tea.Quit
			}
		}
	}
	m.syncOutputs()
	return quit
}

func (m *model) startTurn(text string) {
	m.busy, m.stopping, m.gotText, m.progDone, m.progTotal, m.escCount = true, false, false, 0, 0, 0
	m.resetLive()
	m.turn.Reset()
	m.endLive(false)
	m.compacting = false // a Turn never runs during a Compaction
	m.started, m.now = m.clock(), m.clock()
	m.takeWaiting(text)
	m.cv.addUser(text)
	m.ensureTick()
}

// working says whether the agent is busy for the UI: a Turn or a Compaction runs.
func (m *model) working() bool { return m.busy || m.compacting }

// ensureTick starts the spinner timer unless it runs.
func (m *model) ensureTick() {
	if !m.ticking {
		m.ticking = true
		m.side = append(m.side, m.tick())
	}
}

// delta appends streamed text to the live assistant item, starting one when needed. The
// raw text goes in: the list cleans whole text, so a chunk split inside a rune is safe.
func (m *model) delta(text string) {
	if text == "" {
		return
	}
	if m.cur < 0 {
		m.cur = m.place(m.cv.addAssistant())
	}
	m.turn.WriteString(Clean(text))
	m.gotText = true
	m.cv.feed(m.cur, text)
}

// endLive finishes the streaming assistant item, if any, and reports whether there was one.
// interrupted adds the "interrupted" row inside that item.
func (m *model) endLive(interrupted bool) bool {
	if m.cur < 0 {
		return false
	}
	m.cv.endAssistant(m.cur, interrupted)
	m.cur = -1
	return true
}

// endTurn finishes the answer, adds the outcome notice and reports whether to quit now.
func (m *model) endTurn(ev agent.Event) bool {
	m.abandonCalls()
	interrupted := ev.Finish == chat.FinishInterrupted && ev.Err == nil
	wasLive := m.endLive(interrupted)
	m.busy, m.stopping, m.escCount, m.ask = false, false, 0, nil
	switch {
	case ev.Err != nil:
		m.say("error: " + ev.Err.Error())
	case ev.Finish == chat.FinishLength:
		m.say("answer cut off: output or context limit reached")
	case interrupted && !wasLive:
		m.say(interruptedText)
	}
	return m.quitting
}

// say adds a dim notice to the list (after the text streamed so far).
func (m *model) say(text string) {
	m.endLive(false)
	m.cv.addNotice(Clean(text)) // never inside a Task block: a command's reply must stay visible
}

// ---------------------------------------------------------------------------
// Submit and commands
// ---------------------------------------------------------------------------

// submit runs a command or sends a message to the agent.
func (m *model) submit(line string) tea.Cmd {
	c, err := ParseCommand(line)
	if err != nil {
		m.say(err.Error())
		return nil
	}
	switch c.Kind {
	case KindExit:
		return m.requestQuit()
	case KindClear:
		m.clear()
	case KindAuto:
		on := !m.d.Approver.Auto()
		m.d.Approver.SetAuto(on)
		m.say(map[bool]string{true: "auto-approve on", false: "auto-approve off"}[on])
	case KindTool:
		m.showCall(c.N)
	case KindBg:
		m.listJobs()
	case KindCompact:
		m.compact()
	case KindSessions:
		m.openPicker()
	case KindRename:
		m.rename(c.Name)
	case KindHelp:
		m.say(helpText())
	case KindContext:
		m.showContext()
	case KindBlocks:
		m.listBlocks()
	case KindBlock:
		if err := m.d.Blocks.SetEnabled(c.Name, c.On); err != nil {
			m.say(err.Error())
		}
	case KindModel:
		return m.chooseModel(c.Name)
	case KindTask:
		m.submitTask(c.Text)
	default:
		m.addWaiting(c.Text) // shown at once, before the agent takes it (R-27)
		m.d.Agent.Submit(c.Text)
	}
	return nil
}

// clear empties the history and, only if that worked, the list.
func (m *model) clear() {
	switch err := m.d.Agent.Clear(); {
	case errors.Is(err, agent.ErrBusy):
		m.say("wait for the answer to finish (esc to stop it)")
	case err != nil:
		m.say("clear: " + err.Error())
	default:
		m.cv.reset()
		m.calls, m.runN, m.tasks, m.outBuf, m.cur, m.nextN, m.step = nil, nil, nil, map[int][]byte{}, -1, 0, 0
		m.sc = scrollState{follow: true}
		m.clearSel()
	}
}

func (m *model) listBlocks() {
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for _, bi := range m.d.Blocks.List() {
		state := "off"
		if bi.Enabled {
			state = "on"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t~%d tokens\n", bi.Name, bi.Source, state, bi.Tokens)
	}
	w.Flush()
	m.say(strings.TrimRight(b.String(), "\n"))
}
