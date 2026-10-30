package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestApprovals_ApproveSaveReloadAndChangedEntry(t *testing.T) {
	dir := t.TempDir()
	a, err := LoadApprovals(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := MCPServer{Command: "python3", Args: []string{"x.py"}}
	if a.Approved("/p/one", "demo", s) {
		t.Fatal("approved before any approval")
	}
	if err := a.Approve("/p/one", "demo", s); err != nil {
		t.Fatal(err)
	}
	b, err := LoadApprovals(dir)
	if err != nil || !b.Approved("/p/one", "demo", s) {
		t.Fatalf("reload: %v %v", b, err)
	}
	if b.Approved("/p/two", "demo", s) || b.Approved("/p/one", "other", s) {
		t.Fatal("approval leaked to another project or server")
	}
	s.Args = []string{"y.py"}
	if b.Approved("/p/one", "demo", s) {
		t.Fatal("a changed entry stayed approved")
	}
	if _, err := os.Stat(filepath.Join(dir, "approvals.json")); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "approvals.json"), []byte("nope"), 0o644)
	if _, err := LoadApprovals(dir); err == nil {
		t.Fatal("a broken store must be reported")
	}
}
