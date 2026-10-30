package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// listNames returns the names of the entries directly under dir (relative to the root).
func listNames(t *testing.T, env Env, dir string) []string {
	t.Helper()
	f, err := env.Root.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	names, err := f.Readdirnames(-1)
	if err != nil {
		t.Fatal(err)
	}
	return names
}

// ---------------------------------------------------------------------------
// write_file
// ---------------------------------------------------------------------------

func TestWriteFile_Flags(t *testing.T) {
	var w WriteFile
	if w.ReadOnly() || !w.Confirm() {
		t.Fatal("write_file must be mutating and need confirmation")
	}
	spec := w.Spec()
	if spec.Name != "write_file" || !strings.Contains(string(spec.Schema), `"required"`) {
		t.Fatalf("bad spec: %+v", spec)
	}
	if w.Summary(json.RawMessage(`{"path":"a.txt"}`)) != "a.txt" || w.Summary(json.RawMessage(`[`)) != "" {
		t.Fatal("Summary must return the path, or empty on bad JSON")
	}
}

func TestWriteFile_CreatesParentsAndOverwrites(t *testing.T) {
	env := newTestEnv(t, nil)
	args := json.RawMessage(`{"path":"a/b/c.txt","content":"hi\nthere"}`)
	res, err := WriteFile{}.Run(context.Background(), env, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Brief != "2 lines" {
		t.Fatalf("brief = %q", res.Brief)
	}
	got, _ := env.Root.ReadFile("a/b/c.txt")
	if string(got) != "hi\nthere" {
		t.Fatalf("content = %q", got)
	}
	if _, err := (WriteFile{}).Run(context.Background(), env, json.RawMessage(`{"path":"a/b/c.txt","content":"new"}`)); err != nil {
		t.Fatal(err)
	}
	got, _ = env.Root.ReadFile("a/b/c.txt")
	if string(got) != "new" {
		t.Fatalf("overwrite gave %q", got)
	}
}

func TestWriteFile_EmptyContent(t *testing.T) {
	env := newTestEnv(t, nil)
	if _, err := (WriteFile{}).Run(context.Background(), env, json.RawMessage(`{"path":"e.txt","content":""}`)); err != nil {
		t.Fatal(err)
	}
	got, err := env.Root.ReadFile("e.txt")
	if err != nil || len(got) != 0 {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestWriteFile_ExistingCRLFGetsCRLF(t *testing.T) {
	env := newTestEnv(t, nil)
	env.Root.WriteFile("w.txt", []byte("a\r\nb\r\n"), 0o644)
	if _, err := (WriteFile{}).Run(context.Background(), env, json.RawMessage(`{"path":"w.txt","content":"x\ny\n"}`)); err != nil {
		t.Fatal(err)
	}
	got, _ := env.Root.ReadFile("w.txt")
	if string(got) != "x\r\ny\r\n" {
		t.Fatalf("bytes = %q, want CRLF", got)
	}
}

func TestWriteFile_ContentWithOwnCRLFIsAsIs(t *testing.T) {
	env := newTestEnv(t, nil)
	env.Root.WriteFile("w.txt", []byte("a\r\n"), 0o644)
	if _, err := (WriteFile{}).Run(context.Background(), env, json.RawMessage(`{"path":"w.txt","content":"x\r\ny\n"}`)); err != nil {
		t.Fatal(err)
	}
	got, _ := env.Root.ReadFile("w.txt")
	if string(got) != "x\r\ny\n" {
		t.Fatalf("bytes = %q, want content as is", got)
	}
}

func TestWriteFile_NewFileKeepsLF(t *testing.T) {
	env := newTestEnv(t, nil)
	if _, err := (WriteFile{}).Run(context.Background(), env, json.RawMessage(`{"path":"n.txt","content":"a\nb"}`)); err != nil {
		t.Fatal(err)
	}
	got, _ := env.Root.ReadFile("n.txt")
	if string(got) != "a\nb" {
		t.Fatalf("bytes = %q", got)
	}
}

func TestWriteFile_KeepsModeAndLeavesNoTemp(t *testing.T) {
	env := newTestEnv(t, nil)
	env.Root.WriteFile("m.sh", []byte("old"), 0o600)
	if _, err := (WriteFile{}).Run(context.Background(), env, json.RawMessage(`{"path":"m.sh","content":"new"}`)); err != nil {
		t.Fatal(err)
	}
	fi, err := env.Root.Stat("m.sh")
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, %v; want 0600", fi.Mode().Perm(), err)
	}
	if names := listNames(t, env, "."); len(names) != 1 || names[0] != "m.sh" {
		t.Fatalf("entries = %v, want only m.sh", names)
	}
}

func TestWriteFile_DirectoryTargetChangesNothing(t *testing.T) {
	env := newTestEnv(t, nil)
	env.Root.Mkdir("d", 0o755)
	if _, err := (WriteFile{}).Run(context.Background(), env, json.RawMessage(`{"path":"d","content":"x"}`)); err == nil {
		t.Fatal("writing over a directory must fail")
	}
	if fi, err := env.Root.Stat("d"); err != nil || !fi.IsDir() {
		t.Fatalf("directory changed: %v", err)
	}
	if names := listNames(t, env, "."); len(names) != 1 {
		t.Fatalf("temp file left behind: %v", names)
	}
}

func TestWriteFile_RefusesSymlinkTarget(t *testing.T) {
	env := newTestEnv(t, nil)
	env.Root.WriteFile("real.txt", []byte("keep"), 0o644)
	if err := env.Root.Symlink("real.txt", "link.txt"); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	_, err := (WriteFile{}).Run(context.Background(), env, json.RawMessage(`{"path":"link.txt","content":"x"}`))
	if err == nil || !strings.Contains(err.Error(), "target is a symlink") {
		t.Fatalf("err = %v, want target is a symlink", err)
	}
	if got, _ := env.Root.ReadFile("real.txt"); string(got) != "keep" {
		t.Fatalf("link target changed: %q", got)
	}
}

func TestWriteFile_EscapeRejected(t *testing.T) {
	env := newTestEnv(t, nil)
	_, err := (WriteFile{}).Run(context.Background(), env, json.RawMessage(`{"path":"../x.txt","content":"x"}`))
	if err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("err = %v, want outside", err)
	}
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(env.Dir), "x.txt")); statErr == nil {
		t.Fatal("file was written outside the root")
	}
}

func TestWriteFile_MissingArgs(t *testing.T) {
	env := newTestEnv(t, nil)
	if _, err := (WriteFile{}).Run(context.Background(), env, json.RawMessage(`{"path":"a.txt"}`)); err == nil ||
		!strings.Contains(err.Error(), `"content"`) {
		t.Fatalf("err = %v, want missing content", err)
	}
}

func TestWriteFile_RootStylePathWritesInsideProject(t *testing.T) {
	env := newTestEnv(t, nil)
	if _, err := (WriteFile{}).Run(context.Background(), env, json.RawMessage(`{"path":"/config/settings.yaml","content":"x: 1"}`)); err != nil {
		t.Fatal(err)
	}
	if got, err := env.Root.ReadFile("config/settings.yaml"); err != nil || string(got) != "x: 1" {
		t.Fatalf("file = %q, %v", got, err)
	}
}

// R-46: a call without "path" names the argument and shows the call shape.
func TestWriteFile_MissingPathShowsCallShape(t *testing.T) {
	for _, a := range []string{`{}`, `{"path":""}`, `{"content":"x","old":"a","new":"b"}`} {
		_, err := (WriteFile{}).Run(context.Background(), Env{Dir: t.TempDir()}, json.RawMessage(a))
		if err == nil || !strings.Contains(err.Error(), `"path"`) || !strings.Contains(err.Error(), `write_file`) {
			t.Errorf("%s: error %v does not name path and show the call", a, err)
		}
	}
}

// R-66: "work/x" written from the project folder "work" is a slip of small models (the cwd name
// repeated); without a "work" subfolder the write is refused with the right path in the hint.
func TestWriteFile_PathRepeatingTheProjectFolderIsRefused(t *testing.T) {
	env := newTestEnv(t, nil)
	base := filepath.Base(env.Dir)
	_, err := WriteFile{}.Run(context.Background(), env, json.RawMessage(`{"path":"`+base+`/sub/a.txt","content":"x"}`))
	if err == nil || !strings.Contains(err.Error(), `"sub/a.txt"`) || !strings.Contains(err.Error(), base) {
		t.Fatalf("err = %v", err)
	}
	if _, err := env.Root.Stat(base); err == nil {
		t.Fatal("the nested folder was created")
	}
	if err := env.Root.Mkdir(base, 0o755); err != nil { // the subfolder exists: a normal path
		t.Fatal(err)
	}
	if _, err := (WriteFile{}).Run(context.Background(), env, json.RawMessage(`{"path":"`+base+`/a.txt","content":"x"}`)); err != nil {
		t.Fatal(err)
	}
}

// R-67: "append": true adds the content to the end; a missing file is created.
func TestWriteFile_AppendAddsToTheEnd(t *testing.T) {
	env := newTestEnv(t, nil)
	run := func(args string) Result {
		res, err := WriteFile{}.Run(context.Background(), env, json.RawMessage(args))
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if res := run(`{"path":"a.txt","content":"one\n","append":true}`); res.Brief != "1 lines" {
		t.Fatalf("brief = %q", res.Brief)
	}
	if res := run(`{"path":"a.txt","content":"two\n","append":true}`); res.Output != "appended to a.txt" {
		t.Fatalf("output = %q", res.Output)
	}
	if got, _ := env.Root.ReadFile("a.txt"); string(got) != "one\ntwo\n" {
		t.Fatalf("content = %q", got)
	}
	run(`{"path":"a.txt","content":"three\n"}`) // without append the file is replaced, as before
	if got, _ := env.Root.ReadFile("a.txt"); string(got) != "three\n" {
		t.Fatalf("content = %q", got)
	}
}

func TestWriteFile_AppendKeepsCRLF(t *testing.T) {
	env := newTestEnv(t, nil)
	if err := env.Root.WriteFile("w.txt", []byte("a\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (WriteFile{}).Run(context.Background(), env, json.RawMessage(`{"path":"w.txt","content":"b\n","append":true}`)); err != nil {
		t.Fatal(err)
	}
	if got, _ := env.Root.ReadFile("w.txt"); string(got) != "a\r\nb\r\n" {
		t.Fatalf("content = %q", got)
	}
}
