package main

import (
	"os"
	"os/signal"
	"syscall"

	"swarg/internal/tools"
	"swarg/internal/ui"
)

// ---------------------------------------------------------------------------
// Background jobs adapter
// ---------------------------------------------------------------------------

// jobsView adapts *tools.Jobs to ui.JobsAPI.
type jobsView struct{ j *tools.Jobs }

var _ ui.JobsAPI = jobsView{}

func (v jobsView) List() []ui.JobRow { return jobRows(v.j.List()) }

// jobRows maps job snapshots to the rows the UI shows.
func jobRows(infos []tools.JobInfo) []ui.JobRow {
	rows := make([]ui.JobRow, 0, len(infos))
	for _, in := range infos {
		rows = append(rows, ui.JobRow{ID: in.ID, Command: in.Command, Running: in.Running, ExitCode: in.ExitCode})
	}
	return rows
}

// ---------------------------------------------------------------------------
// Signals
// ---------------------------------------------------------------------------

// killOnSignal kills the background jobs, closes the session and trace files (cl) and exits
// when the terminal closes (SIGHUP) or the process is asked to terminate (SIGTERM). Ctrl+C
// is handled by the UI or runUI.
func killOnSignal(jobs *tools.Jobs, cl *cleanups) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGHUP, syscall.SIGTERM)
	go func() {
		<-ch
		jobs.KillAll()
		cl.Run()
		os.Exit(1)
	}()
}
