package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// ---------------------------------------------------------------------------
// Model choice lines (R-30, I-9)
// ---------------------------------------------------------------------------

func TestWriter_ModelLine(t *testing.T) {
	w := newWriter(t)
	if err := w.Model("qwen-2b"); err != nil {
		t.Fatal(err)
	}
	lines := readLines(t, w.Path())
	if got := lines[len(lines)-1]; got != `{"model":"qwen-2b"}` {
		t.Errorf("line %s", got)
	}
}

func TestWriter_BlankModelIsRefused(t *testing.T) {
	w := newWriter(t)
	if err := w.Model(""); err == nil {
		t.Fatal("a blank model was accepted")
	}
	if n := len(readLines(t, w.Path())); n != 1 {
		t.Errorf("file has %d lines, want only the header", n)
	}
}

func TestLoad_ModelLastWins(t *testing.T) {
	w := newWriter(t) // header model "m"
	msg(t, w, user("a"))
	w.Model("x")
	msg(t, w, user("b"))
	w.Model("y")
	l := loadOK(t, w.Path())
	if l.Model() != "y" || l.Header.Model != "m" {
		t.Errorf("model %q header %q", l.Model(), l.Header.Model)
	}
	if !reflect.DeepEqual(texts(l.Entries), []string{"a", "b"}) {
		t.Errorf("a model line changed the history: %q", texts(l.Entries))
	}
}

func TestLoad_ModelDefaultsToHeader(t *testing.T) {
	w := newWriter(t)
	msg(t, w, user("a"))
	if l := loadOK(t, w.Path()); l.Model() != "m" {
		t.Errorf("model %q, want the header's", l.Model())
	}
}

func TestLoad_OldFileWithoutModelLines(t *testing.T) {
	p := filepath.Join(t.TempDir(), "old.jsonl")
	body := `{"v":1,"id":"abc123","cwd":"/p","model":"old-model","started":"2026-10-27T10:00:00Z"}` + "\n" +
		`{"msg":{"seq":1,"role":"user","content":"hi","turn_start":true}}` + "\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	l := loadOK(t, p)
	if l.Model() != "old-model" || len(l.Entries) != 1 {
		t.Errorf("model %q entries %d", l.Model(), len(l.Entries))
	}
}

// An older swarg reading a new file: the model line decodes to a record with no payload it
// knows, which it skips. Simulated with the record type minus the Model field.
func TestLoad_ModelLineIsANoOpForOldReaders(t *testing.T) {
	var old struct {
		V     int        `json:"v,omitempty"`
		Msg   *msgRecord `json:"msg,omitempty"`
		Clear bool       `json:"clear,omitempty"`
	}
	if err := json.Unmarshal([]byte(`{"model":"x"}`), &old); err != nil || old.V != 0 || old.Msg != nil || old.Clear {
		t.Errorf("old reader sees %+v, %v", old, err)
	}
}

func TestStore_Model(t *testing.T) {
	w := newWriter(t)
	if err := NewStore(w).Model("z"); err != nil {
		t.Fatal(err)
	}
	if l := loadOK(t, w.Path()); l.Model() != "z" {
		t.Errorf("model %q", l.Model())
	}
}
