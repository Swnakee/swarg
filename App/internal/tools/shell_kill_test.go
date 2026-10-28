package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func runKill(env Env, id string) (Result, error) {
	args, _ := json.Marshal(map[string]string{"id": id})
	return ShellKill{}.Run(context.Background(), env, args)
}

func TestShellKill_KillsAndReports(t *testing.T) {
	env := testEnv(t)
	_, id := start(t, env.Jobs, "block")
	res, err := runKill(env, id)
	if err != nil || res.Output != "killed bg1" || res.Brief != "killed bg1" {
		t.Fatalf("got %+v / %v", res, err)
	}
	waitDone(t, env.Jobs, id)
	if env.Jobs.List()[0].Running {
		t.Error("job still running")
	}
	out, _ := runOutput(env, id)
	if !strings.HasSuffix(out.Output, "[killed]") || out.Brief != "killed" {
		t.Errorf("status after kill: %q / %q", out.Output, out.Brief)
	}
}

func TestShellKill_Boundaries(t *testing.T) {
	env := testEnv(t)
	_, id := start(t, env.Jobs, "block")
	if _, err := runKill(env, "bg9"); err == nil {
		t.Error("unknown id: want error")
	}
	if _, err := runKill(env, ""); err == nil {
		t.Error("empty id: want error")
	}
	runKill(env, id)
	res, err := runKill(env, id) // double kill is a no-op, not an error
	if err != nil || !strings.Contains(res.Output, "already exited") {
		t.Errorf("double kill: %+v / %v", res, err)
	}
	if _, err := (ShellKill{}).Run(context.Background(), Env{}, json.RawMessage(`{"id":"bg1"}`)); err == nil {
		t.Error("Jobs == nil: want error")
	}
}

func TestShellKill_Flags(t *testing.T) {
	s := ShellKill{}
	if s.Spec().Name != "shell_kill" || s.ReadOnly() || !s.Confirm() {
		t.Error("bad flags")
	}
	if got := s.Summary(json.RawMessage(`{"id":"bg2"}`)); got != "bg2" {
		t.Errorf("summary %q", got)
	}
}
