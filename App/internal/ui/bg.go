package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// ---------------------------------------------------------------------------
// Background jobs: status count, /bg, Ctrl+B
// ---------------------------------------------------------------------------

const (
	glyphBg    = "⧗"
	bgTickEach = time.Second
	noJobsText = "no background jobs"
	noMoveText = "no running command can be moved to the background"
)

// bgTickMsg refreshes the job count while any job runs, even when the agent is idle.
type bgTickMsg time.Time

// bgTick schedules the next job refresh.
func bgTick() tea.Cmd {
	return tea.Tick(bgTickEach, func(t time.Time) tea.Msg { return bgTickMsg(t) })
}

// jobRows returns the jobs, none when the UI has no job source.
func (m *model) jobRows() []JobRow {
	if m.d.Jobs == nil {
		return nil
	}
	return m.d.Jobs.List()
}

// refreshJobs recounts the running jobs and keeps the 1 s tick alive while there are any.
func (m *model) refreshJobs() {
	m.bgRunning = 0
	for _, j := range m.jobRows() {
		if j.Running {
			m.bgRunning++
		}
	}
	if m.bgRunning > 0 && !m.bgTicking {
		m.bgTicking = true
		m.side = append(m.side, m.bgTick())
	}
}

// onBgTick is the 1 s refresh; refreshJobs starts the next tick if jobs still run.
func (m *model) onBgTick() {
	m.bgTicking = false
	m.refreshJobs()
}

// listJobs prints the /bg listing: "bgN  running|exit N  <command>".
func (m *model) listJobs() {
	m.refreshJobs()
	rows := m.jobRows()
	if len(rows) == 0 {
		m.say(noJobsText)
		return
	}
	lines := make([]string, len(rows))
	for i, j := range rows {
		state := "running"
		if !j.Running {
			state = fmt.Sprintf("exit %d", j.ExitCode)
		}
		lines[i] = fmt.Sprintf("%s  %s  %s", j.ID, state, firstLine(Clean(j.Command)))
	}
	m.say(strings.Join(lines, "\n"))
}

// jobCommand returns the command of job id, "" when unknown.
func jobCommand(jobs JobsAPI, id string) string {
	if jobs == nil {
		return ""
	}
	for _, j := range jobs.List() {
		if j.ID == id {
			return j.Command
		}
	}
	return ""
}

// ctrlB moves the running command to the background. It reports whether the key was
// handled: with nothing running it falls through to the textarea (cursor left).
func (m *model) ctrlB() bool {
	if m.ask != nil {
		return true
	}
	if m.d.Agent.Background() {
		return true
	}
	if len(m.running()) == 0 {
		return false
	}
	m.say(noMoveText)
	return true
}
