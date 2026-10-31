package session

import "testing"

// ---------------------------------------------------------------------------
// The prompt profile record (R-118)
// ---------------------------------------------------------------------------

func TestProfile_RecordedAndReplayed(t *testing.T) {
	w := newWriter(t)
	msg(t, w, user("a"))
	if got := loadOK(t, w.Path()).Profile(); got != "" {
		t.Errorf("a file without a prompt line has profile %q, want empty (the config decides)", got)
	}
	if err := w.Profile("standard"); err != nil {
		t.Fatal(err)
	}
	if err := NewStore(w).Profile("full"); err != nil {
		t.Fatal(err)
	}
	l := loadOK(t, w.Path())
	if l.Profile() != "full" {
		t.Errorf("profile = %q, the last line (full) must win", l.Profile())
	}
	if len(l.Entries) != 1 {
		t.Errorf("a profile line changed the history: %d entries", len(l.Entries))
	}
	lines := readLines(t, w.Path())
	if got := lines[len(lines)-1]; got != `{"prompt":"full"}` {
		t.Errorf("line %s", got)
	}
}

func TestProfile_BlankIsRefused(t *testing.T) {
	w := newWriter(t)
	before := len(readLines(t, w.Path()))
	if err := w.Profile(""); err == nil {
		t.Fatal("a blank profile was accepted")
	}
	if len(readLines(t, w.Path())) != before {
		t.Error("a refused profile wrote a line")
	}
}
