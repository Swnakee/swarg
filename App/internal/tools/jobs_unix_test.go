//go:build !windows

package tools

import (
	"fmt"
	"strings"
	"testing"
)

func TestJobs_KillTerminatesProcessGroup(t *testing.T) {
	j := newJobs(t)
	p, id := start(t, j, "grandchild")
	p.waitBytes(t, 5)
	out, _ := mustRead(t, j, id)
	var pid int
	if _, err := fmt.Sscanf(strings.TrimSpace(out), "pid %d", &pid); err != nil || pid <= 0 {
		t.Fatalf("no grandchild pid in %q", out)
	}
	if err := j.Kill(id); err != nil {
		t.Fatal(err)
	}
	// The grandchild holds our stdout pipe: the job is Done only when it is gone too.
	waitDone(t, j, id)
}

func TestJobs_KilledJobHasExitCodeMinusOne(t *testing.T) {
	j := newJobs(t)
	_, id := start(t, j, "block")
	if err := j.Kill(id); err != nil {
		t.Fatal(err)
	}
	waitDone(t, j, id)
	if info := j.List()[0]; info.Running || info.ExitCode != -1 {
		t.Errorf("killed job: %+v", info)
	}
}
