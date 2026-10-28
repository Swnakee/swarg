package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"

	"swarg/internal/agent"
	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Approver
// ---------------------------------------------------------------------------

const (
	hintYes     = "not approved: run with --yes to allow"
	previewRows = 5 // lines of old/new text shown for edit_file
)

// askMsg asks the model to show a confirmation; the answer goes to reply (buffered).
type askMsg struct {
	call  chat.ToolCall
	reply chan agent.Approval
}

// askGoneMsg tells the model that the request was cancelled before it was answered.
type askGoneMsg struct{ reply chan agent.Approval }

// Approver answers the agent's confirmation requests: from the UI when bound, by the
// --yes flag when not (plain mode), and at once while auto-approve is on.
type Approver struct {
	yes bool

	mu   sync.Mutex
	send func(tea.Msg)
	auto bool
}

// NewApprover returns an unbound Approver; yes is the plain-mode answer.
func NewApprover(yes bool) *Approver { return &Approver{yes: yes} }

// Bind connects the Approver to the running program (nil unbinds).
func (a *Approver) Bind(send func(tea.Msg)) {
	a.mu.Lock()
	a.send = send
	a.mu.Unlock()
}

// SetAuto turns always-yes on or off.
func (a *Approver) SetAuto(on bool) {
	a.mu.Lock()
	a.auto = on
	a.mu.Unlock()
}

// Auto reports whether always-yes is on.
func (a *Approver) Auto() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.auto
}

// Approve is agent.Deps.Approve. It blocks until the user answers or ctx ends (deny).
func (a *Approver) Approve(ctx context.Context, call chat.ToolCall) agent.Approval {
	a.mu.Lock()
	send, auto := a.send, a.auto
	a.mu.Unlock()
	switch {
	case auto:
		return agent.Approval{Allow: true}
	case send == nil && a.yes:
		return agent.Approval{Allow: true}
	case send == nil:
		return agent.Approval{Note: hintYes}
	}
	reply := make(chan agent.Approval, 1)
	send(askMsg{call: call, reply: reply})
	select {
	case ap := <-reply:
		return ap
	case <-ctx.Done():
		send(askGoneMsg{reply})
		return agent.Approval{}
	}
}

// ---------------------------------------------------------------------------
// Prompt text
// ---------------------------------------------------------------------------

// askPrompt is the full confirmation text for a call; task is the goal of the running Task that
// asks, "" for the Session.
func askPrompt(call chat.ToolCall, jobs JobsAPI, task string) string {
	desc, name := describeCall(call, jobs), Clean(call.Name)
	if task != "" {
		name += " (task: " + task + ")"
	}
	if strings.Contains(desc, "\n") {
		return fmt.Sprintf("Allow %s:\n%s\n[y]es / [n]o / [a]lways", name, desc)
	}
	return fmt.Sprintf("Allow %s: %s? [y]es / [n]o / [a]lways", name, desc)
}

// describeCall renders the arguments of a call for a human; bad JSON is shown raw. A
// background shell says so, and shell_kill shows the command of the job it would kill.
func describeCall(call chat.ToolCall, jobs JobsAPI) string {
	var args map[string]any
	if json.Unmarshal([]byte(call.Args), &args) != nil || args == nil {
		return Clean(strings.TrimSpace(call.Args))
	}
	str := func(k string) string { s, _ := args[k].(string); return Clean(s) }
	switch call.Name {
	case "shell":
		if bg, _ := args["background"].(bool); bg {
			return str("command") + " (background)"
		}
		return str("command")
	case "shell_kill":
		if cmd := jobCommand(jobs, str("id")); cmd != "" {
			return str("id") + ": " + firstLine(Clean(cmd))
		}
		return str("id")
	case "edit_file":
		return fmt.Sprintf("%s\n%s\n%s", str("path"), preview("- ", str("old")), preview("+ ", str("new")))
	case "write_file":
		c := str("content")
		lines := strings.Count(c, "\n")
		if c != "" && !strings.HasSuffix(c, "\n") {
			lines++
		}
		return fmt.Sprintf("%s (%d lines, %d bytes)", str("path"), lines, len(c))
	}
	return Clean(strings.TrimSpace(call.Args))
}

// preview prefixes the first previewRows lines of s and notes the rest.
func preview(prefix, s string) string {
	lines := strings.Split(s, "\n")
	extra := len(lines) - previewRows
	if extra > 0 {
		lines = lines[:previewRows]
	}
	for i, l := range lines {
		lines[i] = prefix + l
	}
	if extra > 0 {
		lines = append(lines, fmt.Sprintf("%s… %d more lines", prefix, extra))
	}
	return strings.Join(lines, "\n")
}
