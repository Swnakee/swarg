package session

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// ---------------------------------------------------------------------------
// Provider lines (R-101): the Source a Session runs on
// ---------------------------------------------------------------------------

func TestWriter_ProviderLineAndLoadedProvider(t *testing.T) {
	w := newWriter(t) // the header has no provider: "" = the configured one
	msg(t, w, user("a"))
	if l := loadOK(t, w.Path()); l.Provider() != "" {
		t.Errorf("provider %q before any line, want \"\"", l.Provider())
	}
	if err := w.Provider("openrouter"); err != nil {
		t.Fatal(err)
	}
	msg(t, w, user("b"))
	if err := w.Provider("local"); err != nil {
		t.Fatal(err)
	}
	lines := readLines(t, w.Path())
	if got := lines[len(lines)-1]; got != `{"provider":"local"}` {
		t.Errorf("line %s", got)
	}
	l := loadOK(t, w.Path())
	if l.Provider() != "local" {
		t.Errorf("provider %q, want the last line", l.Provider())
	}
	if !reflect.DeepEqual(texts(l.Entries), []string{"a", "b"}) {
		t.Errorf("a provider line changed the history: %q", texts(l.Entries))
	}
	if err := w.Provider(""); err == nil {
		t.Error("a blank provider was accepted")
	}
}

func TestLoad_ProviderDefaultsToHeader(t *testing.T) {
	dir := t.TempDir()
	w, err := Create(dir, Header{Cwd: "/p", Model: "m", Provider: "ollama"})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	msg(t, w, user("a"))
	if l := loadOK(t, w.Path()); l.Provider() != "ollama" || l.Header.Provider != "ollama" {
		t.Errorf("provider %q header %q", l.Provider(), l.Header.Provider)
	}
}

// A header without the field (files written before T11) loads with "" = the configured provider.
func TestLoad_OldFileWithoutProvider(t *testing.T) {
	p := filepath.Join(t.TempDir(), "old.jsonl")
	body := `{"v":1,"id":"abc123","cwd":"/p","model":"m","started":"2026-01-01T00:00:00Z"}` + "\n" +
		`{"msg":{"seq":1,"role":"user","content":"a"}}` + "\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if l := loadOK(t, p); l.Provider() != "" || len(l.Entries) != 1 {
		t.Errorf("provider %q entries %d", l.Provider(), len(l.Entries))
	}
}

func TestStore_Provider(t *testing.T) {
	w := newWriter(t)
	if err := NewStore(w).Provider("z"); err != nil {
		t.Fatal(err)
	}
	if l := loadOK(t, w.Path()); l.Provider() != "z" {
		t.Errorf("provider %q", l.Provider())
	}
}
