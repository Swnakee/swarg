package session

import (
	"errors"
	"reflect"
	"testing"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Seq handling and the Store adapter
// ---------------------------------------------------------------------------

func TestWriter_KeepsTheSeqOfAMessageAboveEverythingWritten(t *testing.T) {
	w := newWriter(t)
	if seq := msg(t, w, withSeq(user("a"), 5)); seq != 5 {
		t.Errorf("seq %d, want the message's own 5", seq)
	}
	if seq := msg(t, w, withSeq(user("b"), 3)); seq != 6 { // stale: next free
		t.Errorf("seq %d, want 6", seq)
	}
	if seq := msg(t, w, user("c")); seq != 7 { // unassigned: next free
		t.Errorf("seq %d, want 7", seq)
	}
	rep, err := w.Repair([]chat.Message{withSeq(result("x", "t", "c"), 20), result("y", "t", "c")})
	if err != nil || !reflect.DeepEqual(rep, []int{20, 21}) {
		t.Errorf("repair seqs %v, %v", rep, err)
	}
	l := loadOK(t, w.Path())
	if l.MaxSeq() != 21 || l.Entries[0].Msg.Seq != 5 {
		t.Errorf("max seq %d, first entry %+v", l.MaxSeq(), l.Entries[0])
	}
}

func TestLoaded_MaxSeqCountsMessagesThatWereCleared(t *testing.T) {
	w := newWriter(t)
	msg(t, w, user("a"))
	msg(t, w, user("b"))
	w.Clear()
	l := loadOK(t, w.Path())
	if len(l.Entries) != 0 || l.MaxSeq() != 2 {
		t.Errorf("entries %d, max seq %d", len(l.Entries), l.MaxSeq())
	}
}

func TestStore_AdapterWritesEveryKindOfRecord(t *testing.T) {
	w := newWriter(t)
	s := NewStore(w)
	steps := []error{
		s.Message(withSeq(user("go"), 1)),
		s.Message(withSeq(calls("", chat.ToolCall{ID: "c1", Name: "shell", Args: `{"a":"BIG"}`}), 2)),
		s.Message(withSeq(chat.Message{Role: chat.RoleTool, ToolCallID: "c1", Name: "shell", Content: "out", Brief: "b", Label: "l", Failed: true}, 3)),
		s.Patch([]chat.Patch{{Seq: 3, Stub: "[s]"}, {Seq: 2, Args: map[string]string{"c1": `{"a":"x"}`}}}),
		s.Repair([]chat.Message{withSeq(result("z", "t", "cancelled by the user"), 4)}),
		s.Flush(),
	}
	for i, err := range steps {
		if err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	l := loadOK(t, w.Path())
	if len(l.Entries) != 4 || l.Entries[2].Msg.Stub != "[s]" || !l.Entries[2].Msg.Failed || l.Entries[1].Msg.ToolCalls[0].Args != `{"a":"x"}` {
		t.Errorf("entries %+v", l.Entries)
	}
	if err := s.Compact(1, 2, withSeq(assistant("sum"), 9)); err != nil {
		t.Fatal(err)
	}
	if l = loadOK(t, w.Path()); len(l.Entries) != 3 || l.Entries[0].Seq != 9 {
		t.Errorf("after Compact %+v", l.Entries)
	}
	if err := s.Clear(); err != nil {
		t.Fatal(err)
	}
	if l = loadOK(t, w.Path()); len(l.Entries) != 0 {
		t.Errorf("after Clear %+v", l.Entries)
	}
	w.Close()
	if err := s.Message(user("late")); !errors.Is(err, ErrClosed) {
		t.Errorf("after Close: %v", err)
	}
	if err := s.Flush(); !errors.Is(err, ErrClosed) {
		t.Errorf("Flush after Close: %v", err)
	}
}
