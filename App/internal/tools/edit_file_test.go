package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// edit_file
// ---------------------------------------------------------------------------

func TestEditFile_Flags(t *testing.T) {
	var e EditFile
	if e.ReadOnly() || !e.Confirm() {
		t.Fatal("edit_file must be mutating and need confirmation")
	}
	if spec := e.Spec(); spec.Name != "edit_file" || !strings.Contains(string(spec.Schema), `"required"`) {
		t.Fatalf("bad spec: %+v", spec)
	}
	if e.Summary(json.RawMessage(`{"path":"a.go"}`)) != "a.go" || e.Summary(json.RawMessage(`[`)) != "" {
		t.Fatal("Summary must return the path, or empty on bad JSON")
	}
}

func TestEditFile_ReplacesUniqueMatch(t *testing.T) {
	env := newTestEnv(t, nil)
	env.Root.WriteFile("a.txt", []byte("one two"), 0o644)
	res, err := EditFile{}.Run(context.Background(), env, json.RawMessage(`{"path":"a.txt","old":"two","new":"2"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := env.Root.ReadFile("a.txt"); string(got) != "one 2" {
		t.Fatalf("content = %q", got)
	}
	if res.Brief != "+1 -1 lines" {
		t.Fatalf("brief = %q", res.Brief)
	}
}

func TestEditFile_BriefCountsLines(t *testing.T) {
	env := newTestEnv(t, nil)
	env.Root.WriteFile("a.txt", []byte("x\nb\ny\n"), 0o644)
	res, err := EditFile{}.Run(context.Background(), env, json.RawMessage(`{"path":"a.txt","old":"b\n","new":"p\nq\n"}`))
	if err != nil {
		t.Fatal(err)
	}
	if res.Brief != "+2 -1 lines" {
		t.Fatalf("brief = %q, want +2 -1 lines", res.Brief)
	}
}

func TestEditFile_NotFound(t *testing.T) {
	env := newTestEnv(t, nil)
	env.Root.WriteFile("a.txt", []byte("one"), 0o644)
	_, err := EditFile{}.Run(context.Background(), env, json.RawMessage(`{"path":"a.txt","old":"zzz","new":"y"}`))
	if err == nil || !strings.Contains(err.Error(), "old text not found") {
		t.Fatalf("err = %v", err)
	}
	if got, _ := env.Root.ReadFile("a.txt"); string(got) != "one" {
		t.Fatalf("file changed: %q", got)
	}
}

func TestEditFile_TwoMatchesRefused(t *testing.T) {
	env := newTestEnv(t, nil)
	env.Root.WriteFile("a.txt", []byte("x x"), 0o644)
	_, err := EditFile{}.Run(context.Background(), env, json.RawMessage(`{"path":"a.txt","old":"x","new":"y"}`))
	if err == nil || !strings.Contains(err.Error(), "occurs 2 times; include more context to make it unique") {
		t.Fatalf("err = %v", err)
	}
	if got, _ := env.Root.ReadFile("a.txt"); string(got) != "x x" {
		t.Fatalf("file changed: %q", got)
	}
}

func TestEditFile_CountsAllMatches(t *testing.T) {
	env := newTestEnv(t, nil)
	env.Root.WriteFile("a.txt", []byte("x x x"), 0o644)
	_, err := EditFile{}.Run(context.Background(), env, json.RawMessage(`{"path":"a.txt","old":"x","new":"y"}`))
	if err == nil || !strings.Contains(err.Error(), "occurs 3 times") {
		t.Fatalf("err = %v, want 3 occurrences", err)
	}
}

func TestEditFile_CRLFStaysByteExact(t *testing.T) {
	env := newTestEnv(t, nil)
	env.Root.WriteFile("w.txt", []byte("a\r\nb\r\n"), 0o644)
	if _, err := (EditFile{}).Run(context.Background(), env, json.RawMessage(`{"path":"w.txt","old":"b","new":"c"}`)); err != nil {
		t.Fatal(err)
	}
	if got, _ := env.Root.ReadFile("w.txt"); string(got) != "a\r\nc\r\n" {
		t.Fatalf("bytes = %q, want CRLF kept", got)
	}
}

func TestEditFile_CRLFMultiLineMatchAndReplace(t *testing.T) {
	env := newTestEnv(t, nil)
	env.Root.WriteFile("w.txt", []byte("a\r\nb\r\nc\r\n"), 0o644)
	// The model sends LF; the file has CRLF.
	if _, err := (EditFile{}).Run(context.Background(), env, json.RawMessage(`{"path":"w.txt","old":"a\nb","new":"x\ny\nz"}`)); err != nil {
		t.Fatal(err)
	}
	if got, _ := env.Root.ReadFile("w.txt"); string(got) != "x\r\ny\r\nz\r\nc\r\n" {
		t.Fatalf("bytes = %q", got)
	}
}

func TestEditFile_LFFileStaysLF(t *testing.T) {
	env := newTestEnv(t, nil)
	env.Root.WriteFile("l.txt", []byte("a\nb\n"), 0o644)
	if _, err := (EditFile{}).Run(context.Background(), env, json.RawMessage(`{"path":"l.txt","old":"b","new":"c"}`)); err != nil {
		t.Fatal(err)
	}
	if got, _ := env.Root.ReadFile("l.txt"); string(got) != "a\nc\n" {
		t.Fatalf("bytes = %q", got)
	}
}

func TestEditFile_KeepsBOM(t *testing.T) {
	env := newTestEnv(t, nil)
	env.Root.WriteFile("b.txt", []byte("\xef\xbb\xbfone two"), 0o644)
	if _, err := (EditFile{}).Run(context.Background(), env, json.RawMessage(`{"path":"b.txt","old":"one","new":"1"}`)); err != nil {
		t.Fatal(err)
	}
	if got, _ := env.Root.ReadFile("b.txt"); string(got) != "\xef\xbb\xbf1 two" {
		t.Fatalf("bytes = %q, want BOM kept", got)
	}
}

func TestEditFile_EmptyOldRefused(t *testing.T) {
	env := newTestEnv(t, nil)
	env.Root.WriteFile("a.txt", []byte("one"), 0o644)
	if _, err := (EditFile{}).Run(context.Background(), env, json.RawMessage(`{"path":"a.txt","old":"","new":"x"}`)); err == nil {
		t.Fatal("empty old must be an error")
	}
	if got, _ := env.Root.ReadFile("a.txt"); string(got) != "one" {
		t.Fatalf("file changed: %q", got)
	}
}

func TestEditFile_SameOldAndNewRefused(t *testing.T) {
	env := newTestEnv(t, nil)
	env.Root.WriteFile("a.txt", []byte("one"), 0o644)
	_, err := EditFile{}.Run(context.Background(), env, json.RawMessage(`{"path":"a.txt","old":"one","new":"one"}`))
	if err == nil || !strings.Contains(err.Error(), "old and new are the same") {
		t.Fatalf("err = %v", err)
	}
}

func TestEditFile_MissingFile(t *testing.T) {
	env := newTestEnv(t, nil)
	if _, err := (EditFile{}).Run(context.Background(), env, json.RawMessage(`{"path":"none.txt","old":"a","new":"b"}`)); err == nil {
		t.Fatal("missing file must be an error")
	}
}

func TestEditFile_KeepsModeAndLeavesNoTemp(t *testing.T) {
	env := newTestEnv(t, nil)
	env.Root.WriteFile("m.sh", []byte("echo a"), 0o755)
	if _, err := (EditFile{}).Run(context.Background(), env, json.RawMessage(`{"path":"m.sh","old":"a","new":"b"}`)); err != nil {
		t.Fatal(err)
	}
	fi, err := env.Root.Stat("m.sh")
	if err != nil || fi.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %v, %v; want 0755", fi.Mode().Perm(), err)
	}
	if names := listNames(t, env, "."); len(names) != 1 {
		t.Fatalf("temp file left behind: %v", names)
	}
}

func TestEditFile_SymlinkRefused(t *testing.T) {
	env := newTestEnv(t, nil)
	env.Root.WriteFile("real.txt", []byte("a"), 0o644)
	if err := env.Root.Symlink("real.txt", "link.txt"); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	_, err := (EditFile{}).Run(context.Background(), env, json.RawMessage(`{"path":"link.txt","old":"a","new":"b"}`))
	if err == nil || !strings.Contains(err.Error(), "target is a symlink") {
		t.Fatalf("err = %v", err)
	}
}

func TestEditFile_EscapeRejected(t *testing.T) {
	env := newTestEnv(t, nil)
	_, err := EditFile{}.Run(context.Background(), env, json.RawMessage(`{"path":"../x","old":"a","new":"b"}`))
	if err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("err = %v, want outside", err)
	}
}

// R-46: a call without "path" names the argument and shows the call shape.
func TestEditFile_MissingPathShowsCallShape(t *testing.T) {
	for _, a := range []string{`{}`, `{"path":""}`, `{"content":"x","old":"a","new":"b"}`} {
		_, err := (EditFile{}).Run(context.Background(), Env{Dir: t.TempDir()}, json.RawMessage(a))
		if err == nil || !strings.Contains(err.Error(), `"path"`) || !strings.Contains(err.Error(), `edit_file`) {
			t.Errorf("%s: error %v does not name path and show the call", a, err)
		}
	}
}
