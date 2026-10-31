package agents

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// New agent file
// ---------------------------------------------------------------------------

const fileTemplate = `---
name: %s
description: TODO: say in one line when the main agent should use this agent.
tools: readonly
max_steps: 15
---
You are a %s agent. Describe what to do and what to return.
`

// NewFile writes dir/<name>.md from the template and returns its path. An invalid name or an
// existing file is refused; dir is created when missing.
func NewFile(dir, name string) (string, error) {
	if !ValidName(name) {
		return "", fmt.Errorf("invalid agent name %q: use %s", name, namePattern)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, name+".md")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return "", fmt.Errorf("%s already exists", path)
		}
		return "", err
	}
	_, werr := fmt.Fprintf(f, fileTemplate, name, name)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return "", werr
	}
	return path, nil
}

// ---------------------------------------------------------------------------
// Copy of a built-in type (R-129)
// ---------------------------------------------------------------------------

// ExportBuiltin writes the full definition of the built-in type name (front matter and body)
// into dir/<name>.md so it can be edited; the file then replaces the built-in. An existing file is
// never overwritten: its path comes back with created false. A name that is no built-in is an error.
func ExportBuiltin(dir, name string) (path string, created bool, err error) {
	var t *Type
	for _, b := range builtins() {
		if b.Name == name {
			t = &b
		}
	}
	if t == nil {
		return "", false, fmt.Errorf("%q is not a built-in agent type", name)
	}
	path = filepath.Join(dir, name+".md")
	if _, err := os.Stat(path); err == nil {
		return path, false, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", false, err
	}
	var b strings.Builder
	b.WriteString("---\nname: " + name + "\ndescription: " + t.Description + "\n")
	if len(t.Tools) > 0 {
		b.WriteString("tools: " + strings.Join(t.Tools, ", ") + "\n")
	}
	for _, kv := range [][2]string{{"server", t.Server}, {"model", t.Model}, {"profile", t.Profile}} {
		if kv[1] != "" {
			b.WriteString(kv[0] + ": " + kv[1] + "\n")
		}
	}
	if t.MaxSteps > 0 {
		b.WriteString("max_steps: " + strconv.Itoa(t.MaxSteps) + "\n")
	}
	b.WriteString("---\n" + t.Body + "\n")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return path, false, nil
		}
		return "", false, err
	}
	_, werr := f.WriteString(b.String())
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return "", false, werr
	}
	return path, true, nil
}
