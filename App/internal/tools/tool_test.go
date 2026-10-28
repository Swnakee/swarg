package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

type fake struct {
	name  string
	ro    bool
	run   func() (Result, error)
	label func(json.RawMessage) string
}

func (f fake) Spec() chat.ToolSpec {
	return chat.ToolSpec{Name: f.name, Description: "d", Schema: json.RawMessage(`{"type":"object"}`)}
}
func (f fake) ReadOnly() bool { return f.ro }
func (f fake) Confirm() bool  { return !f.ro }
func (f fake) Block() string  { return "block " + f.name }
func (f fake) Summary(a json.RawMessage) string {
	if f.label != nil {
		return f.label(a)
	}
	return string(a)
}
func (f fake) Run(context.Context, Env, json.RawMessage) (Result, error) { return f.run() }

func newFakeSet(t *testing.T, disabled ...string) *Set {
	t.Helper()
	all := []Tool{
		fake{name: "a", ro: true, run: func() (Result, error) { return Result{Output: "ok"}, nil }},
		fake{name: "b", run: func() (Result, error) { panic("boom") }},
		fake{name: "c", run: func() (Result, error) { return Result{Output: strings.Repeat("x", 40000)}, nil },
			label: func(json.RawMessage) string { panic("bad") }},
	}
	s, err := NewSet(all, disabled)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// ---------------------------------------------------------------------------
// Decode
// ---------------------------------------------------------------------------

func TestDecode(t *testing.T) {
	type A struct {
		Path string `json:"path"`
		N    int    `json:"n"`
	}
	cases := []struct {
		name, in, want string // want "" = ok
	}{
		{"ok", `{"path":"a","n":2}`, ""},
		{"empty is object", ``, `missing required argument "path"`},
		{"unknown field", `{"path":"a","zzz":1}`, "zzz"},
		{"missing", `{"n":1}`, `missing required argument "path"`},
		{"wrong type", `{"path":5}`, "bad arguments"},
		{"trailing garbage", `{"path":"a"} x`, "JSON object"},
		{"null", `null`, "JSON object"},
		{"not object", `[1]`, "JSON object"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var a A
			err := Decode(json.RawMessage(c.in), &a, "path")
			if c.want == "" {
				if err != nil || a.Path != "a" || a.N != 2 {
					t.Fatalf("err=%v a=%+v", err, a)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err=%v, want %q", err, c.want)
			}
		})
	}
	var a A
	if err := Decode(nil, &a); err != nil {
		t.Fatalf("empty without required: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Rel and os.Root confinement
// ---------------------------------------------------------------------------

func TestRel(t *testing.T) {
	dir := t.TempDir()
	ok := map[string]string{
		"a.txt":                              "a.txt",
		"./a/../b":                           "b",
		".":                                  ".",
		filepath.Join(dir, "sub", "f"):       filepath.Join("sub", "f"),
		filepath.Join(dir, "missing", "new"): filepath.Join("missing", "new"),
		dir:                                  ".",
	}
	for in, want := range ok {
		if got, err := Rel(dir, in); err != nil || got != want {
			t.Errorf("Rel(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	bad := []string{"", "..", "../x", "a/../../x", "/../etc/passwd", "/etc/../../x", "/a/../../x"}
	for _, in := range bad {
		if _, err := Rel(dir, in); err == nil {
			t.Errorf("Rel(%q) accepted", in)
		}
	}
}

// A root-style path outside the project means the project folder ("/strutil.py" -> "strutil.py").
func TestRel_RootStylePathMeansProjectFolder(t *testing.T) {
	if filepath.Separator != '/' {
		t.Skip("unix separators")
	}
	dir := t.TempDir()
	cases := map[string]string{
		"/strutil.py":                "strutil.py",
		"/config/settings.yaml":      "config/settings.yaml",
		"//words.txt":                "words.txt",
		"/./a/../b.txt":              "b.txt",
		filepath.Dir(dir):            strings.TrimPrefix(filepath.Dir(dir), "/"),
		dir + "-evil/x":              strings.TrimPrefix(dir, "/") + "-evil/x",
		"/":                          ".",
		filepath.Join(dir, "in.txt"): "in.txt", // inside the project it stays as before
	}
	for in, want := range cases {
		if got, err := Rel(dir, in); err != nil || got != want {
			t.Errorf("Rel(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

func TestRel_WindowsRootStyle(t *testing.T) {
	if filepath.Separator != '\\' {
		t.Skip("windows only")
	}
	dir := t.TempDir()
	if got, err := Rel(dir, `C:\foo\a.txt`); err != nil || got != `foo\a.txt` {
		t.Errorf("Rel(C:\\foo\\a.txt) = %q, %v", got, err)
	}
	if got, err := Rel(dir, `\strutil.py`); err != nil || got != `strutil.py` {
		t.Errorf("Rel(\\strutil.py) = %q, %v", got, err)
	}
	for _, in := range []string{`\\srv\share\f`, `\\?\C:\x`, `//srv/share/f`, `C:\..\x`} {
		if _, err := Rel(dir, in); err == nil {
			t.Errorf("Rel(%q) accepted", in)
		}
	}
}

func TestRel_WindowsForms(t *testing.T) {
	if filepath.Separator != '\\' {
		t.Skip("windows only")
	}
	dir := t.TempDir()
	for _, in := range []string{`C:x`, `\\srv\share\f`, `\\?\C:\x`, `NUL`, `con.txt`} {
		if _, err := Rel(dir, in); err == nil {
			t.Errorf("Rel(%q) accepted", in)
		}
	}
}

// Documents why tools must use Env.Root: it refuses ".." and symlinks leaving the root.
func TestRoot_Confinement(t *testing.T) {
	base := t.TempDir()
	proj, outside := filepath.Join(base, "proj"), filepath.Join(base, "out")
	for _, d := range []string{proj, outside} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(base, "x"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "s"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(proj)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if _, err := root.Open("../x"); err == nil {
		t.Error("root.Open(../x) succeeded")
	}
	if err := os.Symlink(outside, filepath.Join(proj, "link")); err != nil {
		t.Skip("symlinks unsupported:", err)
	}
	if _, err := root.Open("link/s"); err == nil {
		t.Error("root.Open through an outward symlink succeeded")
	}
	if err := os.WriteFile(filepath.Join(proj, "in"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if rel, err := Rel(proj, filepath.Join(proj, "in")); err != nil {
		t.Error(err)
	} else if _, err := root.Open(rel); err != nil {
		t.Errorf("root.Open(%q): %v", rel, err)
	}
}

func TestRoot_SymlinkedDirsAndMissingParents(t *testing.T) {
	base := t.TempDir()
	proj, outside := filepath.Join(base, "proj"), filepath.Join(base, "out")
	for _, d := range []string{filepath.Join(proj, "real"), outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(proj, "real", "f"), []byte("in"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", filepath.Join(proj, "inside")); err != nil {
		t.Skip("symlinks unsupported:", err)
	}
	if err := os.Symlink(outside, filepath.Join(proj, "out")); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(proj)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if b, err := root.ReadFile("inside/f"); err != nil || string(b) != "in" {
		t.Errorf("symlinked dir inside the root: %q, %v", b, err)
	}
	if err := root.MkdirAll("out/new/deeper", 0o755); err == nil {
		t.Error("MkdirAll under a missing parent whose ancestor symlinks out succeeded")
	}
	if _, err := os.Stat(filepath.Join(outside, "new")); err == nil {
		t.Error("a directory was created outside the root")
	}
}

// ---------------------------------------------------------------------------
// Cap
// ---------------------------------------------------------------------------

func TestCap_HeadTailMarker(t *testing.T) {
	var b strings.Builder
	for i := 1; b.Len() < 40000; i++ {
		fmt.Fprintf(&b, "line %05d\n", i)
	}
	s := b.String()
	got := Cap(s)
	i := strings.Index(got, "\n[... ")
	j := strings.Index(got[i+1:], "]\n") + i + 2
	note, head, tail := got[i+1:j], got[:i], got[j+1:]
	if !strings.HasPrefix(got, "line 00001\n") || !strings.HasSuffix(got, "\n"+strings.TrimSuffix(s[strings.LastIndex(strings.TrimSuffix(s, "\n"), "\n")+1:], "\n")+"\n") {
		t.Errorf("first or last line lost: ...%q", got[len(got)-30:])
	}
	if want := cutNote(len(s)-len(head)-len(tail), hintFile); note != want {
		t.Errorf("note %q, want %q", note, want)
	}
	if len(head)+len(tail) > MaxOutput || len(got) > MaxOutput+len(note)+2 {
		t.Errorf("sizes: head %d tail %d total %d", len(head), len(tail), len(got))
	}
	if r := float64(len(head)) / float64(len(head)+len(tail)); r < 0.6 || r > 0.72 {
		t.Errorf("head share %.2f, want about 2/3", r)
	}
	if last := head[strings.LastIndexByte(head, '\n')+1:]; len(last) != len("line 00001") || !strings.HasPrefix(tail, "line ") {
		t.Errorf("cuts are not on line boundaries: %q / %q", last, tail[:12])
	}
}

func TestCap_Boundaries(t *testing.T) {
	if Cap("") != "" {
		t.Error("empty changed")
	}
	exact := strings.Repeat("a", MaxOutput)
	if Cap(exact) != exact {
		t.Error("exactly MaxOutput changed")
	}
	if Cap(exact+"a") == exact+"a" {
		t.Error("MaxOutput+1 not cut")
	}
	// No newlines, multi-byte runes: both cuts stay valid UTF-8.
	h := Cap(strings.Repeat("ж", 20000))
	if !utf8.ValidString(h) || !strings.Contains(h, "bytes cut;") || len(h) > MaxOutput+100 {
		t.Errorf("bad hard cut: len=%d valid=%v", len(h), utf8.ValidString(h))
	}
	// Odd offsets: three-byte runes with the limit not a multiple of 3.
	for _, r := range []string{"€", "😀"} {
		if o := Cap(strings.Repeat(r, 9000)); !utf8.ValidString(o) {
			t.Errorf("cut inside %q", r)
		}
	}
}

func TestCap_ShellKeepsStatusLine(t *testing.T) {
	s := strings.Repeat("compiler noise line\n", 2000) + "FAIL: boom\n[exit 1]"
	got := Cap(s)
	if !strings.HasSuffix(got, "FAIL: boom\n[exit 1]") {
		t.Errorf("tail lost: ...%q", got[len(got)-40:])
	}
}

// ---------------------------------------------------------------------------
// Set
// ---------------------------------------------------------------------------

func TestNewSet_Disabled(t *testing.T) {
	if _, err := NewSet(nil, []string{"nope"}); err == nil {
		t.Error("unknown disabled name accepted")
	}
	_, err := NewSet([]Tool{fake{name: "a"}}, []string{"nope"})
	if err == nil || !strings.Contains(err.Error(), "known: a") {
		t.Errorf("err=%v", err)
	}
	s := newFakeSet(t, "b")
	if len(s.Specs()) != 2 {
		t.Errorf("specs=%d", len(s.Specs()))
	}
	if _, ok := s.Get("b"); ok {
		t.Error("disabled tool found")
	}
	bl := s.Blocks()
	if len(bl) != 2 || bl[1].Name != "tool-c" || bl[1].Source != "tool" || bl[1].Order != 501 ||
		!bl[1].Enabled || bl[1].Text != "block c" {
		t.Errorf("blocks=%+v", bl)
	}
}

func TestSet_Kind(t *testing.T) {
	s := newFakeSet(t)
	if k, ro, c := s.Kind("a"); !k || !ro || c {
		t.Error("kind a")
	}
	if k, ro, c := s.Kind("b"); !k || ro || !c {
		t.Error("kind b")
	}
	if k, _, _ := s.Kind("zz"); k {
		t.Error("unknown is known")
	}
}

func TestSet_Label(t *testing.T) {
	s := newFakeSet(t)
	if s.Label("a", json.RawMessage(`{bad`)) != `{bad` {
		t.Error("label a")
	}
	if s.Label("c", nil) != "" || s.Label("zz", nil) != "" {
		t.Error("panicking or unknown label not empty")
	}
}

func TestSet_Run(t *testing.T) {
	s := newFakeSet(t)
	ctx := context.Background()
	if r, err := s.Run(ctx, Env{}, "a", nil); err != nil || r.Output != "ok" {
		t.Errorf("a: %+v %v", r, err)
	}
	if _, err := s.Run(ctx, Env{}, "b", nil); err == nil || !strings.Contains(err.Error(), "tool panicked: boom") {
		t.Errorf("panic: %v", err)
	}
	if r, _ := s.Run(ctx, Env{}, "c", nil); !strings.Contains(r.Output, "bytes cut;") {
		t.Error("output not capped")
	}
	if _, err := s.Run(ctx, Env{}, "zz", nil); err == nil {
		t.Error("unknown tool ran")
	}
}

func TestSet_RunSymlinkEscapeSaysOutside(t *testing.T) {
	base := t.TempDir()
	proj := filepath.Join(base, "proj")
	if err := os.Mkdir(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "x"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(base, "x"), filepath.Join(proj, "link")); err != nil {
		t.Skip("symlinks unsupported:", err)
	}
	root, err := os.OpenRoot(proj)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	set, err := NewSet(All(), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = set.Run(context.Background(), Env{Root: root, Dir: proj}, "read_file", json.RawMessage(`{"path":"link"}`))
	if err == nil || !strings.HasPrefix(err.Error(), "path is outside the project folder") {
		t.Errorf("symlink escape error = %v, want the outside message", err)
	}
}

// Table over the built-in tools (trivially passes while All is empty).
func TestAll_Contract(t *testing.T) {
	seen := map[string]bool{}
	for _, tl := range All() {
		sp := tl.Spec()
		if sp.Name == "" || seen[sp.Name] || sp.Description == "" || tl.Block() == "" {
			t.Errorf("bad tool %q", sp.Name)
		}
		seen[sp.Name] = true
		var schema map[string]any
		if json.Unmarshal(sp.Schema, &schema) != nil || schema["type"] != "object" {
			t.Errorf("%s: schema is not an object", sp.Name)
		}
		if tl.ReadOnly() && tl.Confirm() {
			t.Errorf("%s: read-only tool asks for confirmation", sp.Name)
		}
	}
}

func TestSet_NewToolsRegistered(t *testing.T) {
	set, err := NewSet(All(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"shell_output", "shell_kill"} {
		if _, ok := set.Get(n); !ok {
			t.Errorf("%s is not registered", n)
		}
	}
	if known, ro, confirm := set.Kind("shell_output"); !known || !ro || confirm {
		t.Error("shell_output must be read-only without confirmation")
	}
	off, err := NewSet(All(), []string{"shell_output", "shell_kill"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := off.Get("shell_kill"); ok {
		t.Error("disabled tool is still enabled")
	}
	if _, ok := off.Get("shell"); !ok {
		t.Error("shell must stay when only the job tools are disabled")
	}
	if !strings.Contains(strings.Join(blockTexts(set), "\n"), "background") {
		t.Error("the shell block must mention background")
	}
}

func blockTexts(s *Set) []string {
	var out []string
	for _, b := range s.Blocks() {
		out = append(out, b.Text)
	}
	return out
}

// The refusal must say how to fix the path.
func TestRel_OutsideErrorHintsRelativePath(t *testing.T) {
	_, err := Rel(t.TempDir(), "../main.go")
	if err == nil || !strings.Contains(err.Error(), "relative") {
		t.Errorf("err = %v, want a hint to use a relative path", err)
	}
}

// Eval 2026-10-27 (robust-01): a missing file came back as "openat config/settings.yaml: no such
// file or directory"; the 2B then kept looking instead of reporting it. Say it plainly.
func TestSet_RunMissingFileSaysNotFound(t *testing.T) {
	proj := t.TempDir()
	root, err := os.OpenRoot(proj)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	set, err := NewSet(All(), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = set.Run(context.Background(), Env{Root: root, Dir: proj}, "read_file", json.RawMessage(`{"path":"config/settings.yaml"}`))
	if err == nil || err.Error() != "file not found: config/settings.yaml" {
		t.Errorf("err = %v, want \"file not found: config/settings.yaml\"", err)
	}
}
