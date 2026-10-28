package session

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Name record (R-32, I-11)
// ---------------------------------------------------------------------------

func TestName_WriteAndLoad(t *testing.T) {
	w := newWriter(t)
	msg(t, w, user("hello"))
	if err := w.Name("api work"); err != nil {
		t.Fatal(err)
	}
	l, err := Load(w.Path())
	if err != nil {
		t.Fatal(err)
	}
	if l.Name != "api work" {
		t.Errorf("Name %q, want api work", l.Name)
	}
	if got := l.Messages(); len(got) != 1 || got[0].Content != "hello" {
		t.Errorf("the Name changed the history: %+v", got)
	}
}

func TestName_LastWinsAndSurvivesClear(t *testing.T) {
	w := newWriter(t)
	msg(t, w, user("hello"))
	for _, n := range []string{"first", "second"} {
		if err := w.Name(n); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Clear(); err != nil {
		t.Fatal(err)
	}
	l, err := Load(w.Path())
	if err != nil {
		t.Fatal(err)
	}
	if l.Name != "second" || len(l.Entries) != 0 {
		t.Errorf("Name %q with %d entries, want second with none", l.Name, len(l.Entries))
	}
}

func TestName_OneLineAndBlankRefused(t *testing.T) {
	w := newWriter(t)
	if err := w.Name("  two\nlines \t here  "); err != nil {
		t.Fatal(err)
	}
	for _, blank := range []string{"", "   ", "\n\t"} {
		if err := w.Name(blank); !errors.Is(err, ErrBlankName) {
			t.Errorf("Name(%q) = %v, want ErrBlankName", blank, err)
		}
	}
	l, err := Load(w.Path())
	if err != nil {
		t.Fatal(err)
	}
	if l.Name != "two lines here" {
		t.Errorf("Name %q, want one line", l.Name)
	}
	b, _ := os.ReadFile(w.Path())
	if n := strings.Count(string(b), `"name"`); n != 1 {
		t.Errorf("%d name records, want 1 (a blank Name writes nothing)", n)
	}
}

func TestName_AfterClose(t *testing.T) {
	w := newWriter(t)
	w.Close()
	if err := w.Name("x"); !errors.Is(err, ErrClosed) {
		t.Errorf("Name after Close = %v, want ErrClosed", err)
	}
}

// A file written before Names existed (no name record) loads as before, and a file whose
// name record sits among unknown keys loads too (forward compatibility).
func TestName_OldFileAndUnknownKeys(t *testing.T) {
	w := newWriter(t)
	msg(t, w, user("hello"))
	w.Close()
	l, err := Load(w.Path())
	if err != nil || l.Name != "" || len(l.Entries) != 1 {
		t.Fatalf("old file: %v, Name %q, %d entries", err, l.Name, len(l.Entries))
	}
	f, err := os.OpenFile(w.Path(), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"name":"later","future":{"x":1}}` + "\n" + `{"other_kind":true}` + "\n")
	f.Close()
	l2, err := Load(w.Path())
	if err != nil {
		t.Fatal(err)
	}
	if l2.Name != "later" || !reflect.DeepEqual(l.Messages(), l2.Messages()) {
		t.Errorf("Name %q, history changed: %v", l2.Name, !reflect.DeepEqual(l.Messages(), l2.Messages()))
	}
}

func TestList_TitleIsTheNameWhenSet(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "s")
	named, err := Create(dir, Header{Cwd: "/p", Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	msg(t, named, user("the first message"))
	if err := named.Name("  my   " + strings.Repeat("n", 70)); err != nil {
		t.Fatal(err)
	}
	named.Close()
	plain, err := Create(dir, Header{Cwd: "/p", Model: "m", ID: "ffffff"})
	if err != nil {
		t.Fatal(err)
	}
	msg(t, plain, user("the first message"))
	plain.Close()

	infos, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]Info{}
	for _, in := range infos {
		by[in.ID] = in
	}
	n := by[named.ID()]
	if want := "my " + strings.Repeat("n", 57); n.Title != want {
		t.Errorf("named Title %q, want the Name cut to 60 runes %q", n.Title, want)
	}
	if p := by["ffffff"]; p.Title != "the first message" {
		t.Errorf("unnamed: Title %q", p.Title)
	}
}
