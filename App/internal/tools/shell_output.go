package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"swarg/internal/chat"
)

// ShellOutput reads the new output of a background job; reading changes nothing the user cares
// about, so it is read-only.
type ShellOutput struct{ readOnlyTool }

// jobIDArgs are the arguments of shell_output and shell_kill.
type jobIDArgs struct {
	ID string `json:"id"`
}

// jobIDSchema is the schema of shell_kill: the id is required.
const jobIDSchema = `{"type":"object","properties":{"id":{"type":"string",` +
	`"description":"job id, e.g. bg1"}},"required":["id"]}`

// outputSchema is the schema of shell_output: without an id it reads the latest job.
const outputSchema = `{"type":"object","properties":{"id":{"type":"string",` +
	`"description":"job id, e.g. bg1; default: the latest job"}}}`

// Spec describes the tool to the model.
func (ShellOutput) Spec() chat.ToolSpec {
	return chat.ToolSpec{Name: "shell_output", Description: "Read new output of a background job",
		Schema: json.RawMessage(outputSchema)}
}

// Block is the system prompt text for the tool.
func (ShellOutput) Block() string {
	return "shell_output: new output of a background job since the last read, then [running] or [exit N]; waits up to 10 s.\n" +
		`Example: shell_output {"id": "bg1"}`
}

// errNoJobs is the answer to a model that polls shell_output without ever backgrounding a command.
var errNoJobs = errors.New("no background jobs: shell_output only reads commands started with shell background:true; to run a command use shell")

// Summary is the job id.
func (ShellOutput) Summary(args json.RawMessage) string {
	var a jobIDArgs
	if Decode(args, &a, "id") != nil {
		return ""
	}
	return a.ID
}

// Run returns the output produced since the previous read and the job status.
// Without an id it reads the latest job and names it, so the model learns the id.
func (t ShellOutput) Run(ctx context.Context, env Env, args json.RawMessage) (Result, error) {
	id, named, err := outputID(env, args)
	if err != nil {
		return Result{}, err
	}
	if err := knownJob(env, id); err != nil {
		return Result{}, err
	}
	waitJob(ctx, env, id)
	out, info, err := env.Jobs.Read(id)
	if err != nil {
		return Result{}, err
	}
	status, brief := "[running]", fmt.Sprintf("%d bytes, running", len(out))
	if !info.Running {
		status = fmt.Sprintf("[exit %d]", info.ExitCode)
		brief = fmt.Sprintf("exit %d", info.ExitCode)
		if info.ExitCode < 0 {
			status = "[killed]"
			brief = "killed"
		}
	}
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	if !named {
		out = id + ": " + out
	}
	return Result{Output: out + status, Brief: brief}, nil
}

// knownJob fails with the known ids when id names no job.
func knownJob(env Env, id string) error {
	all := env.Jobs.List()
	if len(all) == 0 {
		return fmt.Errorf("unknown job %q; %w", id, errNoJobs)
	}
	ids := make([]string, len(all))
	for i, j := range all {
		if j.ID == id {
			return nil
		}
		ids[i] = j.ID
	}
	return fmt.Errorf("unknown job %q; known ids: %s", id, strings.Join(ids, ", "))
}

// waitJob waits up to env.OutputWait for the job to end, so a polling model gets the final
// output in one call instead of many. The call context stops the wait.
func waitJob(ctx context.Context, env Env, id string) {
	if env.OutputWait <= 0 {
		return
	}
	timer := time.NewTimer(env.OutputWait)
	defer timer.Stop()
	select {
	case <-env.Jobs.Done(id):
	case <-timer.C:
	case <-ctx.Done():
	}
}

// outputID is the requested job id, or the latest job when the id is absent (named = false).
func outputID(env Env, args json.RawMessage) (id string, named bool, err error) {
	var a jobIDArgs
	if err := Decode(args, &a); err != nil {
		return "", false, err
	}
	if _, has := probeKeys(args)["id"]; has {
		id, err := jobID(ShellOutput{}, env, args)
		return id, true, err
	}
	if env.Jobs == nil {
		return "", false, errors.New("background commands are not available")
	}
	all := env.Jobs.List()
	if len(all) == 0 {
		return "", false, errNoJobs
	}
	return all[len(all)-1].ID, false, nil
}

// probeKeys returns the top-level keys of a JSON object (nil when it is not one).
func probeKeys(args json.RawMessage) map[string]json.RawMessage {
	var keys map[string]json.RawMessage
	_ = json.Unmarshal(args, &keys)
	return keys
}

// jobID decodes the args and checks that background jobs exist.
func jobID(t Tool, env Env, args json.RawMessage) (string, error) {
	var a jobIDArgs
	if err := DecodeFor(t, args, &a, "id"); err != nil {
		return "", err
	}
	if strings.TrimSpace(a.ID) == "" {
		return "", errors.New("id is empty")
	}
	if env.Jobs == nil {
		return "", errors.New("background commands are not available")
	}
	return a.ID, nil
}
