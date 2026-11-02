package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeTrace puts a trace file next to a session so Delete has one to remove.
func writeTrace(t *testing.T, w *Writer) string {
	t.Helper()
	tp := TracePath(w.Path())
	if err := os.MkdirAll(filepath.Dir(tp), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tp, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return tp
}

// R-26
func TestDelete_RemovesTheSessionAndItsTrace(t *testing.T) {
	dir := t.TempDir()
	t0 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	keep := mkSession(t, dir, t0, "keep", 1)
	drop := mkSession(t, dir, t0.Add(time.Minute), "drop", 1)
	keepTrace := writeTrace(t, keep)
	dropTrace := writeTrace(t, drop)

	if err := Delete(dir, drop.ID()); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	for _, p := range []string{drop.Path(), dropTrace} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s still exists (err %v)", p, err)
		}
	}
	for _, p := range []string{keep.Path(), keepTrace} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("Delete removed the wrong file %s: %v", p, err)
		}
	}
	infos, _ := List(dir)
	if len(infos) != 1 || infos[0].ID != keep.ID() {
		t.Errorf("after Delete List = %+v, want only keep", infos)
	}
}

func TestDelete_MissingTraceIsFine(t *testing.T) {
	dir := t.TempDir()
	w := mkSession(t, dir, time.Now(), "s", 1) // no trace written
	if err := Delete(dir, w.ID()); err != nil {
		t.Fatalf("Delete without a trace: %v", err)
	}
	if _, err := os.Stat(w.Path()); !os.IsNotExist(err) {
		t.Error("session file still exists")
	}
}

func TestDelete_UnknownAndEmptyID(t *testing.T) {
	dir := t.TempDir()
	mkSession(t, dir, time.Now(), "s", 1)
	if err := Delete(dir, "deadbeef"); err == nil || !strings.Contains(err.Error(), "no session") {
		t.Errorf("unknown id: %v", err)
	}
	if err := Delete(dir, ""); err == nil {
		t.Error("empty id was accepted")
	}
	// the real session is untouched
	if infos, _ := List(dir); len(infos) != 1 {
		t.Errorf("a failed Delete changed the directory: %+v", infos)
	}
}

// R-26
func TestDelete_StaysInsideTheDir(t *testing.T) {
	dir := t.TempDir()
	mkSession(t, dir, time.Now(), "s", 1)
	outside := filepath.Join(t.TempDir(), "victim.txt")
	if err := os.WriteFile(outside, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// an id that is not a plain file id cannot name a file outside dir
	_ = Delete(dir, "../"+filepath.Base(outside))
	if _, err := os.Stat(outside); err != nil {
		t.Errorf("Delete reached outside the sessions dir: %v", err)
	}
}
