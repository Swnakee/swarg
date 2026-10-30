package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"swarg/internal/chat"
)

// WriteFile creates or overwrites a file inside the project folder.
type WriteFile struct{}

// ---------------------------------------------------------------------------
// Tool
// ---------------------------------------------------------------------------

// Spec describes write_file for the model.
func (WriteFile) Spec() chat.ToolSpec {
	return chat.ToolSpec{
		Name:        "write_file",
		Description: "Create or overwrite a file with the given content.",
		Schema: json.RawMessage(`{"type":"object","properties":{` +
			`"path":{"type":"string","description":"file path"},` +
			`"content":{"type":"string","description":"the whole new file content"},` +
			`"append":{"type":"boolean","description":"add the content to the end of the file instead of replacing it"}},` +
			`"required":["path","content"],"additionalProperties":false}`),
	}
}

// ReadOnly is false: write_file changes files.
func (WriteFile) ReadOnly() bool { return false }

// Confirm is true: the user approves each write.
func (WriteFile) Confirm() bool { return true }

// Block tells the model when to use write_file.
func (WriteFile) Block() string {
	return "write_file: create a file or replace a whole file. Small changes: edit_file. Leading / = project root. " +
		"A long file: write the first part, then the next parts with \"append\": true.\n" +
		`Example: {"path":"notes/todo.md","content":"- first item\n"}`
}

// Summary is the path of the call, or "" for bad JSON.
func (WriteFile) Summary(args json.RawMessage) string { return pathOf(args) }

// Run writes the content atomically, keeping the existing mode and CRLF line endings; with
// append the content goes after the existing one (R-67).
func (t WriteFile) Run(_ context.Context, env Env, args json.RawMessage) (Result, error) {
	var a struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Append  bool   `json:"append"`
	}
	if err := DecodeFor(t, args, &a, "path", "content"); err != nil {
		return Result{}, err
	}
	name, err := Rel(env.Dir, a.Path)
	if err != nil {
		return Result{}, err
	}
	if err := nestedHint(env, name); err != nil {
		return Result{}, err
	}
	mode, exists, err := statTarget(env.Root, name)
	if err != nil {
		return Result{}, err
	}
	data, verb := []byte(a.Content), "wrote "
	if exists {
		old, err := env.Root.ReadFile(name)
		if err != nil {
			return Result{}, err
		}
		if bytes.Contains(old, []byte("\r\n")) && !bytes.Contains(data, []byte("\r\n")) {
			data = bytes.ReplaceAll(data, []byte("\n"), []byte("\r\n"))
		}
		if a.Append {
			data, verb = append(old, data...), "appended to "
		}
	}
	if dir := filepath.Dir(name); dir != "." {
		if err := env.Root.MkdirAll(dir, 0o755); err != nil {
			return Result{}, err
		}
	}
	if err := writeAtomic(env.Root, name, data, mode); err != nil {
		return Result{}, err
	}
	n := lineCount(a.Content)
	return Result{Output: verb + a.Path, Brief: strconv.Itoa(n) + " lines"}, nil
}

// ---------------------------------------------------------------------------
// Helpers (shared with edit_file.go)
// ---------------------------------------------------------------------------

// pathOf returns the "path" argument, or "" when the JSON is bad.
func pathOf(args json.RawMessage) string {
	var a struct {
		Path string `json:"path"`
	}
	if json.Unmarshal(args, &a) != nil {
		return ""
	}
	return a.Path
}

// statTarget refuses a symlink at name. It reports the permission bits of an
// existing file (0644 when there is none) and whether the file exists.
func statTarget(root *os.Root, name string) (mode fs.FileMode, exists bool, err error) {
	fi, err := root.Lstat(name)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return 0o644, false, nil
	case err != nil:
		return 0, false, err
	case fi.Mode()&fs.ModeSymlink != 0:
		return 0, false, errors.New("target is a symlink")
	}
	return fi.Mode().Perm(), true, nil
}

// writeAtomic writes data to name through root: a temp file in the same folder,
// then a rename over the target. The temp file is removed on any error.
func writeAtomic(root *os.Root, name string, data []byte, mode fs.FileMode) error {
	tmp := filepath.Join(filepath.Dir(name),
		"."+filepath.Base(name)+"."+strconv.FormatInt(time.Now().UnixNano(), 36)+".tmp")
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = root.Chmod(tmp, mode)
	}
	if err == nil {
		err = root.Rename(tmp, name)
	}
	if err != nil {
		_ = root.Remove(tmp)
	}
	return err
}

// lineCount counts the lines of s; a last line without "\n" counts too.
func lineCount(s string) int {
	if s == "" {
		return 0
	}
	n := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
}
