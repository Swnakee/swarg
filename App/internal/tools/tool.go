// Package tools holds the built-in tools the model can call, the Set that
// registers them and the shared helpers (Decode, Rel, Cap) every tool uses.
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"swarg/internal/chat"
	"swarg/internal/prompt"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// Tool is one capability the model can call. One tool = one file + one line in all.go.
type Tool interface {
	Spec() chat.ToolSpec
	ReadOnly() bool                      // read-only tools run in parallel
	Confirm() bool                       // needs user approval before running
	Block() string                       // context block text for the system prompt
	Summary(args json.RawMessage) string // short call label, e.g. `README.md`
	Run(ctx context.Context, env Env, args json.RawMessage) (Result, error)
}

// Env is what a tool may touch. All file access goes through Root.
type Env struct {
	Root         *os.Root // the project folder, opened once by main
	Dir          string   // absolute project path (shell cwd, display, Rel base)
	OS, Shell    string
	ShellTimeout time.Duration

	Jobs *Jobs // background job manager (nil = background unsupported)
	// BackgroundGrace: a background:true command that ends within it returns a normal result
	// and never becomes a job (small models background even `ls`). 0 = adopt at once.
	BackgroundGrace time.Duration
	// OutputWait: shell_output waits up to this long for a running job to end. 0 = no wait.
	OutputWait time.Duration
	Web        Web                // web_search and web_fetch settings
	Progress   func(chunk string) // nil-safe: live output while a tool runs; never called after Run returns
	Detach     <-chan struct{}    // closed by the user (Ctrl+B): move the running command to the background
}

// Result is a tool's outcome.
type Result struct {
	Output string // text returned to the model (capped by Set.Run)
	Brief  string // one-line summary for the collapsed UI line
}

// MaxOutput is the byte limit of a tool result.
const MaxOutput = 12000

// ---------------------------------------------------------------------------
// Set
// ---------------------------------------------------------------------------

// Set is the enabled tools in registry order.
type Set struct {
	list []Tool
}

// NewSet keeps the tools of all that are not disabled; an unknown disabled name is an error.
func NewSet(all []Tool, disabled []string) (*Set, error) {
	known := map[string]bool{}
	for _, t := range all {
		known[t.Spec().Name] = true
	}
	off := map[string]bool{}
	for _, n := range disabled {
		if !known[n] {
			names := make([]string, 0, len(known))
			for k := range known {
				names = append(names, k)
			}
			sort.Strings(names)
			return nil, fmt.Errorf("unknown tool %q (known: %s)", n, strings.Join(names, ", "))
		}
		off[n] = true
	}
	s := &Set{}
	for _, t := range all {
		if !off[t.Spec().Name] {
			s.list = append(s.list, t)
		}
	}
	return s, nil
}

// Specs returns the specs of the enabled tools.
func (s *Set) Specs() []chat.ToolSpec {
	out := make([]chat.ToolSpec, len(s.list))
	for i, t := range s.list {
		out[i] = t.Spec()
	}
	return out
}

// Get finds an enabled tool by name.
func (s *Set) Get(name string) (Tool, bool) {
	for _, t := range s.list {
		if t.Spec().Name == name {
			return t, true
		}
	}
	return nil, false
}

// Blocks returns one prompt block per enabled tool.
func (s *Set) Blocks() []prompt.Block {
	out := make([]prompt.Block, len(s.list))
	for i, t := range s.list {
		out[i] = prompt.Block{Name: "tool-" + t.Spec().Name, Source: prompt.SourceTool,
			Order: 500 + i, Text: t.Block(), Enabled: true}
	}
	return out
}

// Kind reports whether the tool exists and its flags.
func (s *Set) Kind(name string) (known, readOnly, confirm bool) {
	t, ok := s.Get(name)
	if !ok {
		return false, false, false
	}
	return true, t.ReadOnly(), t.Confirm()
}

// Label is the short call label for the UI; it never panics and tolerates bad JSON.
func (s *Set) Label(name string, args json.RawMessage) (label string) {
	t, ok := s.Get(name)
	if !ok {
		return ""
	}
	defer func() {
		if recover() != nil {
			label = ""
		}
	}()
	return t.Summary(args)
}

// Run executes a tool; a panic becomes an error and the output is capped.
func (s *Set) Run(ctx context.Context, env Env, name string, args json.RawMessage) (res Result, err error) {
	t, ok := s.Get(name)
	if !ok {
		return Result{}, fmt.Errorf("unknown tool %q", name)
	}
	defer func() {
		if r := recover(); r != nil {
			res, err = Result{}, fmt.Errorf("tool panicked: %v", r)
		}
	}()
	res, err = t.Run(ctx, env, args)
	if err != nil && strings.Contains(err.Error(), "path escapes from parent") { // os.Root refused a symlink or ..
		err = errOutside
	}
	var pe *fs.PathError
	if errors.As(err, &pe) && errors.Is(err, fs.ErrNotExist) { // "openat x: no such file..." -> plain words
		err = fmt.Errorf("file not found: %s", filepath.ToSlash(pe.Path))
	}
	res.Output = Cap(res.Output)
	return res, err
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// missingArg is the error of a required argument that is absent or an empty string.
type missingArg struct{ name string }

func (e *missingArg) Error() string { return fmt.Sprintf("missing required argument %q", e.name) }

// Decode strictly decodes an args object into v. Unknown fields, wrong types,
// null and trailing data are errors; each name in required must be present (a *missingArg
// error otherwise). Empty args mean {}.
func Decode(args json.RawMessage, v any, required ...string) error {
	if len(bytes.TrimSpace(args)) == 0 {
		args = json.RawMessage("{}")
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(args, &keys); err != nil || keys == nil {
		return errors.New("arguments must be a JSON object")
	}
	for _, r := range required {
		if _, ok := keys[r]; !ok {
			return &missingArg{r}
		}
	}
	dec := json.NewDecoder(bytes.NewReader(args))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("bad arguments: %s", strings.TrimPrefix(err.Error(), "json: "))
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("bad arguments: trailing data")
	}
	return nil
}

// DecodeFor is Decode for a Tool's own arguments: a missing required argument names the
// argument and shows the correct call, so a small model can repeat it (R-46). required[0] is
// the call's key (path, pattern, command, id, url, query) and must not be an empty string either;
// later arguments may be empty (write_file content, edit_file new).
func DecodeFor(t Tool, args json.RawMessage, v any, required ...string) error {
	err := Decode(args, v, required...)
	var m *missingArg
	if err == nil && len(required) > 0 {
		var keys map[string]json.RawMessage
		if json.Unmarshal(args, &keys) == nil && string(bytes.TrimSpace(keys[required[0]])) == `""` {
			m = &missingArg{required[0]}
		}
	} else if !errors.As(err, &m) {
		return err
	}
	if m == nil {
		return nil
	}
	return fmt.Errorf("missing argument %q; call %s like %s", m.name, t.Spec().Name, exampleOf(t))
}

// exampleOf is the call example of a Tool's Block ("Example: <name> {...}" or "Example: {...}").
func exampleOf(t Tool) string {
	_, ex, _ := strings.Cut(t.Block(), "Example: ")
	ex, _, _ = strings.Cut(ex, "\n")
	return strings.TrimSpace(strings.TrimPrefix(ex, t.Spec().Name+" "))
}

var errOutside = errors.New("path is outside the project folder; use a path relative to it, e.g. main.go")

// Rel turns a model path into a clean path relative to dir, for use with
// Env.Root. Relative paths and absolute paths inside dir are accepted. An absolute path
// outside dir is read as written from the project root: the volume and leading separators
// are dropped ("/strutil.py" -> "strutil.py"). Confinement stays with os.Root and IsLocal.
func Rel(dir, p string) (string, error) {
	if p == "" {
		return "", errors.New("path is empty")
	}
	if runtime.GOOS == "windows" && (strings.HasPrefix(p, `\\`) || strings.HasPrefix(p, "//") ||
		(!filepath.IsAbs(p) && filepath.VolumeName(p) != "")) {
		return "", errOutside // drive-relative, UNC, \\?\
	}
	rel := filepath.Clean(p)
	if filepath.IsAbs(p) {
		var err error
		if rel, err = filepath.Rel(dir, rel); err != nil || !filepath.IsLocal(rel) {
			// Dropped from the raw path, not the cleaned one: Clean would hide a leading "..".
			rel = filepath.Clean(strings.TrimLeft(p[len(filepath.VolumeName(p)):], `/\`))
		}
	}
	if !filepath.IsLocal(rel) { // also rejects reserved device names on Windows
		return "", errOutside
	}
	return rel, nil
}

// nestedHint catches a path that repeats the name of the project folder ("work/x" written from
// /work, a slip of small models) while no such subfolder exists: the right path goes into the
// error instead of a nested copy of the folder appearing (R-66). Nil when the subfolder exists.
func nestedHint(env Env, name string) error {
	first, rest, found := strings.Cut(name, string(filepath.Separator))
	if !found || first != filepath.Base(env.Dir) {
		return nil
	}
	if _, err := env.Root.Stat(first); err == nil {
		return nil
	}
	return fmt.Errorf("there is no %q inside the working directory %s; paths are relative to it: use %q", first, env.Dir, filepath.ToSlash(rest))
}

// Cap limits s to MaxOutput bytes of content: the head (2/3) and the tail (1/3) are kept, the
// middle is replaced by a note with the number of cut bytes. Cuts fall on line boundaries
// (or, inside one huge line, on UTF-8 boundaries), so the result may exceed MaxOutput by the note.
func Cap(s string) string {
	head, tail, cut := cutMiddle(s, MaxOutput)
	if cut == 0 {
		return s
	}
	return head + "\n" + cutNote(cut, hintFile) + "\n" + tail
}

// What the model is told to do to get the cut part back: for a file or a page result, and for
// the output of a command (a file read does not help there).
const (
	hintFile = "use read_file offset/limit or grep for the rest"
	hintCmd  = "rerun with a filter (grep, head, tail) for the rest"
)

// cutNote is the middle marker of a capped result.
func cutNote(n int, hint string) string {
	return fmt.Sprintf("[... %d bytes cut; %s]", n, hint)
}

// cutMiddle keeps about limit bytes of s: the head gets 2/3 and the tail 1/3, both cut on line
// boundaries when possible, never inside a UTF-8 sequence. cut is the number of bytes dropped
// (including the newlines at the two cuts); 0 means s fits and head is s.
func cutMiddle(s string, limit int) (head, tail string, cut int) {
	if len(s) <= limit {
		return s, "", 0
	}
	h := limit * 2 / 3
	if i := strings.LastIndexByte(s[:h], '\n'); i > 0 {
		h = i
	} else {
		for h > 0 && !utf8.RuneStart(s[h]) {
			h--
		}
	}
	t := len(s) - (limit - limit*2/3)
	if j := strings.IndexByte(s[t:], '\n'); j >= 0 && t+j+1 < len(s) {
		t += j + 1
	} else {
		for t < len(s) && !utf8.RuneStart(s[t]) {
			t++
		}
	}
	return s[:h], s[t:], t - h
}
