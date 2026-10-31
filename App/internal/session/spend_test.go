package session

import (
	"reflect"
	"testing"
)

// ---------------------------------------------------------------------------
// Spend lines (R-111): the cost of each response, summed per model on replay
// ---------------------------------------------------------------------------

func TestWriter_SpendLinesSumPerModel(t *testing.T) {
	w := newWriter(t)
	msg(t, w, user("a"))
	for _, s := range []struct {
		m string
		c float64
	}{{"a/m", 0.25}, {"b/m", 0.5}, {"a/m", 0.5}} {
		if err := w.Spend(s.m, s.c); err != nil {
			t.Fatal(err)
		}
	}
	l := loadOK(t, w.Path())
	if want := map[string]float64{"a/m": 0.75, "b/m": 0.5}; !reflect.DeepEqual(l.Spend(), want) {
		t.Errorf("spend %v, want %v", l.Spend(), want)
	}
	if !reflect.DeepEqual(texts(l.Entries), []string{"a"}) {
		t.Errorf("a spend line changed the history: %q", texts(l.Entries))
	}
}

func TestWriter_SpendIgnoresNothingToRecord(t *testing.T) {
	w := newWriter(t)
	msg(t, w, user("a"))
	n := len(readLines(t, w.Path()))
	for _, c := range []float64{0, -1} {
		if err := w.Spend("m", c); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Spend("", 1); err != nil {
		t.Fatal(err)
	}
	if got := len(readLines(t, w.Path())); got != n {
		t.Errorf("%d lines, want %d: free or blank spends write nothing", got, n)
	}
	if loadOK(t, w.Path()).Spend() != nil {
		t.Error("a file with no spend line must give nil")
	}
}
