//go:build !windows

package tools

import (
	"os"
	"os/exec"
	"syscall"
)

// setProcessGroup puts the command in its own process group so the whole tree can be killed.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killTree kills the process group led by p.
func killTree(p *os.Process) error {
	if p == nil {
		return nil
	}
	return syscall.Kill(-p.Pid, syscall.SIGKILL)
}

// setCmdLine is only needed on Windows; elsewhere cmd gets plain arguments.
func setCmdLine(cmd *exec.Cmd, command string) {
	cmd.Args = []string{"cmd", "/c", command}
}
