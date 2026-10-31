package main

import (
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"swarg/internal/chat"
	"swarg/internal/session"
)

// ---------------------------------------------------------------------------
// saving as ui.SessionsAPI: Open switches the SessionFile being written (R-33), Delete
// spares the current Session (R-34), Rename goes to the current file (R-32).
// ---------------------------------------------------------------------------

// savedSession writes a finished SessionFile with the given user messages and returns its path.
func savedSession(t *testing.T, dir, id string, msgs ...string) string {
	t.Helper()
	w, err := session.Create(dir, session.Header{Cwd: "/p", Model: "m", ID: id, Started: time.Now().Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	for i, m := range msgs {
		if _, err := w.Message(chat.Message{Role: chat.RoleUser, Content: m, Seq: i + 1}); err != nil {
			t.Fatal(err)
		}
	}
	w.Close()
	return w.Path()
}

// newSaving starts saving a fresh Session in dir, with the Trace on.
func newSaving(t *testing.T, dir string) (*saving, *cleanups) {
	t.Helper()
	var cl cleanups
	sv, err := openSaving(dir, session.Info{}, session.Header{Cwd: "/p", Model: "m"}, true, true, 0, &cl, io.Discard)
	if err != nil || !sv.on {
		t.Fatalf("openSaving: %v, on %v", err, sv.on)
	}
	t.Cleanup(cl.Run)
	return sv, &cl
}

func entries(t *testing.T, path string) int {
	t.Helper()
	l, err := session.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return len(l.Entries)
}

func exists(path string) bool { _, err := os.Stat(path); return err == nil }

func TestSaving_OpenSwitchesTheFileAndDropsTheEmptyNewOne(t *testing.T) {
	dir := t.TempDir()
	pathA := savedSession(t, dir, "aaaaaa", "hello")
	sv, cl := newSaving(t, dir)
	fresh := sv.w.Path()

	o, err := sv.Open("aaa")
	if err != nil || len(o.History) != 1 || o.History[0].Content != "hello" || o.Title != "hello" || o.MaxSeq != 1 || o.Model != "m" {
		t.Fatalf("Open = %+v, %v", o, err)
	}
	if sv.Current() != "aaaaaa" {
		t.Errorf("Current = %q, want aaaaaa", sv.Current())
	}
	if exists(fresh) || exists(session.TracePath(fresh)) {
		t.Error("the empty new file or its trace was not removed on the switch")
	}
	if err := sv.store.Message(chat.Message{Role: chat.RoleUser, Content: "more", Seq: 9}); err != nil {
		t.Fatal(err)
	}
	if n := entries(t, pathA); n != 2 {
		t.Errorf("the opened file has %d entries, want 2 (the new message went elsewhere)", n)
	}
	sv.trace.Record("probe", map[string]int{"x": 1})
	if !exists(session.TracePath(pathA)) {
		t.Error("the Trace did not follow the switch")
	}
	if !sv.saved() {
		t.Error("saved() must be true for an opened Session with messages")
	}
	cl.Run()
	if n := entries(t, pathA); n != 2 || !exists(pathA) {
		t.Errorf("after close: %d entries, exists %v", n, exists(pathA))
	}
}

func TestSaving_OpenKeepsANewFileThatHasMessages(t *testing.T) {
	dir := t.TempDir()
	savedSession(t, dir, "aaaaaa", "hello")
	sv, _ := newSaving(t, dir)
	fresh := sv.w.Path()
	if err := sv.store.Message(chat.Message{Role: chat.RoleUser, Content: "said", Seq: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := sv.Open("aaaaaa"); err != nil {
		t.Fatal(err)
	}
	if !exists(fresh) || entries(t, fresh) != 1 {
		t.Error("a new file with a message must stay after the switch")
	}
}

func TestSaving_RenameGoesToTheCurrentFile(t *testing.T) {
	dir := t.TempDir()
	pathA := savedSession(t, dir, "aaaaaa", "hello")
	sv, _ := newSaving(t, dir)
	fresh := sv.w.Path()
	sv.store.Message(chat.Message{Role: chat.RoleUser, Content: "said", Seq: 1})
	if err := sv.Rename("one"); err != nil {
		t.Fatal(err)
	}
	if _, err := sv.Open("aaaaaa"); err != nil {
		t.Fatal(err)
	}
	if err := sv.Rename("two"); err != nil {
		t.Fatal(err)
	}
	lf, _ := session.Load(fresh)
	la, _ := session.Load(pathA)
	if lf.Name != "one" || la.Name != "two" {
		t.Errorf("names: new file %q (want one), opened file %q (want two)", lf.Name, la.Name)
	}
	if err := sv.Rename("   "); err == nil {
		t.Error("a blank name must be refused")
	}
}

func TestSaving_RenameKeepsAnEmptyNewFile(t *testing.T) {
	dir := t.TempDir()
	sv, cl := newSaving(t, dir)
	path := sv.w.Path()
	if err := sv.Rename("kept"); err != nil {
		t.Fatal(err)
	}
	cl.Run()
	l, err := session.Load(path)
	if err != nil || l.Name != "kept" {
		t.Errorf("a renamed empty session was lost: %v, Name %q", err, l.Name)
	}
}

func TestSaving_AfterCloseNothingIsWritten(t *testing.T) {
	dir := t.TempDir()
	sv, cl := newSaving(t, dir)
	sv.store.Message(chat.Message{Role: chat.RoleUser, Content: "said", Seq: 1})
	cl.Run()
	if sv.Current() != "" {
		t.Errorf("Current after close = %q", sv.Current())
	}
	if err := sv.Rename("late"); err == nil {
		t.Error("Rename after close must fail")
	}
	if err := sv.store.Message(chat.Message{Role: chat.RoleUser, Content: "late", Seq: 2}); err != nil {
		t.Errorf("a Store call after close must be a no-op, got %v", err)
	}
	if !sv.saved() {
		t.Error("saved() must still report the message written before the close")
	}
}

func TestSaving_OpenWithTraceOff(t *testing.T) {
	dir := t.TempDir()
	pathA := savedSession(t, dir, "aaaaaa", "hello")
	var cl cleanups
	sv, err := openSaving(dir, session.Info{}, session.Header{Cwd: "/p", Model: "m"}, true, false, 0, &cl, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cl.Run)
	if _, err := sv.Open("aaaaaa"); err != nil {
		t.Fatal(err)
	}
	if sv.trace != nil || sv.tw != nil || exists(session.TracePath(pathA)) {
		t.Error("a trace appeared although the Trace is off")
	}
}

// /rename during a Turn: Rename and Current race the agent's Store calls on one mutex.
func TestSaving_RenameConcurrentWithStore(t *testing.T) {
	dir := t.TempDir()
	sv, _ := newSaving(t, dir)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 50 {
			sv.store.Message(chat.Message{Role: chat.RoleUser, Content: "m", Seq: i + 1})
		}
	}()
	for i := range 50 {
		if err := sv.Rename(fmt.Sprint("n", i)); err != nil {
			t.Fatal(err)
		}
		sv.Current()
	}
	<-done
	l, err := session.Load(sv.w.Path())
	if err != nil || len(l.Entries) != 50 || l.Name != "n49" {
		t.Errorf("load %v, %d entries, Name %q", err, len(l.Entries), l.Name)
	}
}

func TestSaving_DeleteSparesTheCurrentSession(t *testing.T) {
	dir := t.TempDir()
	pathA := savedSession(t, dir, "aaaaaa", "hello")
	sv, _ := newSaving(t, dir)
	if err := sv.Delete(sv.Current()); err == nil {
		t.Error("deleting the current session must fail")
	}
	if exists(sv.w.Path()) == false {
		t.Error("the current file vanished")
	}
	if err := sv.Delete("aaaaaa"); err != nil || exists(pathA) {
		t.Errorf("delete of another session: %v, exists %v", err, exists(pathA))
	}
}

func TestSaving_OpenCurrentOrUnknownKeepsTheFile(t *testing.T) {
	dir := t.TempDir()
	savedSession(t, dir, "aaaaaa", "hello")
	sv, _ := newSaving(t, dir)
	fresh := sv.w.Path()
	sv.store.Message(chat.Message{Role: chat.RoleUser, Content: "said", Seq: 1})
	if o, err := sv.Open(sv.Current()); err != nil || len(o.History) != 1 || sv.w.Path() != fresh {
		t.Errorf("reopening the current session: %+v, %v, path %q", o, err, sv.w.Path())
	}
	if _, err := sv.Open("zzz"); err == nil || sv.w.Path() != fresh {
		t.Errorf("unknown id: %v, path %q", err, sv.w.Path())
	}
}

// A response's cost goes to the SessionFile of the moment; opening or resuming that session brings
// the spend back (R-111).
func TestSaving_SpendIsRecordedAndComesBackOnOpenAndResume(t *testing.T) {
	dir := t.TempDir()
	sv, cl := newSaving(t, dir)
	st := swStore{sv}
	if err := st.Message(chat.Message{Role: chat.RoleUser, Content: "hi", Seq: 1}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []float64{0.25, 0.5} {
		if err := st.Spend("a/m", c); err != nil {
			t.Fatal(err)
		}
	}
	path, id := sv.w.Path(), sv.Current()
	cl.Run()
	other, _ := newSaving(t, dir)
	o, err := other.Open(id)
	if err != nil || o.Spend["a/m"] != 0.75 {
		t.Fatalf("Open = %+v, %v; want spend 0.75", o.Spend, err)
	}
	var cl2 cleanups
	back, err := openSaving(dir, session.Info{Path: path}, session.Header{}, true, false, 0, &cl2, io.Discard)
	if err != nil || back.spend["a/m"] != 0.75 {
		t.Fatalf("resume spend %v, %v; want 0.75", back.spend, err)
	}
	cl2.Run()
}
