//go:build windows

package tools

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

// setProcessGroup is a no-op: taskkill /T walks the process tree.
func setProcessGroup(cmd *exec.Cmd) {}

// killTree kills p and all its descendants.
func killTree(p *os.Process) error {
	if p == nil {
		return nil
	}
	return exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(p.Pid)).Run()
}

// setCmdLine passes the command to cmd /c verbatim, without Go's argument quoting.
func setCmdLine(cmd *exec.Cmd, command string) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: "cmd /c " + command}
}
