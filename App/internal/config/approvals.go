package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// ---------------------------------------------------------------------------
// Approvals of project MCP servers (R-97, I-34) and one-time notices (R-104): stored in the user
// config dir, never in the project
// ---------------------------------------------------------------------------

const approvalsFile = "approvals.json"

// Approvals remembers which project MCP servers the user approved: project dir -> server name ->
// hash of the server entry (a changed entry asks again).
type Approvals struct {
	path string
	m    map[string]map[string]string
}

// LoadApprovals reads <dir>/approvals.json; a missing file is an empty store.
func LoadApprovals(dir string) (*Approvals, error) {
	a := &Approvals{path: filepath.Join(dir, approvalsFile), m: map[string]map[string]string{}}
	data, err := os.ReadFile(a.path)
	if errors.Is(err, fs.ErrNotExist) {
		return a, nil
	}
	if err != nil {
		return nil, fmt.Errorf("approvals: %w", err)
	}
	if err := json.Unmarshal(data, &a.m); err != nil {
		return nil, fmt.Errorf("approvals: parse %s: %w", a.path, err)
	}
	return a, nil
}

// Approved reports whether server s of the project was approved as it is configured now.
func (a *Approvals) Approved(project, server string, s MCPServer) bool {
	return a.m[project][server] == hashServer(s)
}

// Approve records the approval and saves the store.
func (a *Approvals) Approve(project, server string, s MCPServer) error {
	return a.put(project, server, hashServer(s))
}

// Noticed reports whether the one-time notice key was shown for the project.
func (a *Approvals) Noticed(project, key string) bool { return a.m[project]["notice:"+key] == "1" }

// Notice records that the notice was shown and saves the store.
func (a *Approvals) Notice(project, key string) error { return a.put(project, "notice:"+key, "1") }

// put stores one value and saves the file.
func (a *Approvals) put(project, key, value string) error {
	if a.m[project] == nil {
		a.m[project] = map[string]string{}
	}
	a.m[project][key] = value
	out, err := json.MarshalIndent(a.m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(a.path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(a.path, append(out, '\n'), 0o600)
}

// hashServer fingerprints a server entry (command, args, env names, url, headers names...).
func hashServer(s MCPServer) string {
	b, _ := json.Marshal(s)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:16])
}
