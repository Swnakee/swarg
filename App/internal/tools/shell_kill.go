package tools

import (
	"context"
	"encoding/json"
	"errors"

	"swarg/internal/chat"
)

// ShellKill kills a background job.
type ShellKill struct{}

// Spec describes the tool to the model.
func (ShellKill) Spec() chat.ToolSpec {
	return chat.ToolSpec{Name: "shell_kill", Description: "Kill a background job",
		Schema: json.RawMessage(jobIDSchema)}
}

// ReadOnly is false: it stops a process.
func (ShellKill) ReadOnly() bool { return false }

// Confirm is true: the user approves every kill.
func (ShellKill) Confirm() bool { return true }

// Block is the system prompt text for the tool.
func (ShellKill) Block() string {
	return "shell_kill: stop a background job and its child processes.\n" +
		`Example: shell_kill {"id": "bg1"}`
}

// Summary is the job id.
func (ShellKill) Summary(args json.RawMessage) string { return ShellOutput{}.Summary(args) }

// Run kills the job; a job that already exited is reported, not an error.
func (t ShellKill) Run(_ context.Context, env Env, args json.RawMessage) (Result, error) {
	id, err := jobID(t, env, args)
	if err != nil {
		return Result{}, err
	}
	switch err := env.Jobs.Kill(id); {
	case errors.Is(err, ErrExited):
		return Result{Output: id + " already exited", Brief: "already exited"}, nil
	case err != nil:
		return Result{}, err
	}
	return Result{Output: "killed " + id, Brief: "killed " + id}, nil
}
