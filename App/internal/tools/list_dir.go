package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// list_dir
// ---------------------------------------------------------------------------

const maxListEntries = 500

// ListDir lists one folder inside the project.
type ListDir struct{}

// Spec describes list_dir for the model.
func (ListDir) Spec() chat.ToolSpec {
	return chat.ToolSpec{
		Name:        "list_dir",
		Description: "List the entries of a folder. Folders end with /.",
		Schema: json.RawMessage(`{"type":"object","properties":{` +
			`"path":{"type":"string","description":"folder path (default \".\")"},` +
			`"glob":{"type":"string","description":"file name pattern, e.g. *_test.go: list matching files at any depth"}},` +
			`"required":[]}`),
	}
}

// ReadOnly is true: list_dir never changes files.
func (ListDir) ReadOnly() bool { return true }

// Confirm is false: list_dir runs without approval.
func (ListDir) Confirm() bool { return false }

// Block is the system prompt text for list_dir.
func (ListDir) Block() string {
	return "list_dir: list a folder (default \".\"); folders end with /. With glob: matching files at any depth.\n" +
		`Example: {"path": "internal/tools"} or {"glob": "*_test.go"}`
}

// Summary is the folder path, "." when none is given, or "" when the args are not valid JSON.
func (ListDir) Summary(args json.RawMessage) string {
	var a struct {
		Path string `json:"path"`
	}
	if json.Unmarshal(args, &a) != nil {
		return ""
	}
	if a.Path == "" {
		return "."
	}
	return a.Path
}

// Run returns the sorted entries of a folder, capped at 500 names; with glob it lists the
// matching files under the folder instead.
func (ListDir) Run(ctx context.Context, env Env, args json.RawMessage) (Result, error) {
	var a struct {
		Path string `json:"path"`
		Glob string `json:"glob"`
	}
	if err := Decode(args, &a); err != nil {
		return Result{}, err
	}
	if _, err := filepath.Match(a.Glob, ""); err != nil {
		return Result{}, fmt.Errorf("invalid glob: %w", err)
	}
	if a.Path == "" {
		a.Path = "."
	}
	rel, err := Rel(env.Dir, a.Path)
	if err != nil {
		return Result{}, err
	}
	fi, err := env.Root.Stat(rel)
	if err != nil {
		return Result{}, err
	}
	if !fi.IsDir() {
		return Result{}, fmt.Errorf("%s is not a directory", a.Path)
	}
	if a.Glob != "" {
		return globFiles(ctx, env, filepath.ToSlash(rel), a.Glob)
	}
	entries, err := fs.ReadDir(env.Root.FS(), filepath.ToSlash(rel)) // sorted by name
	if err != nil {
		return Result{}, err
	}
	var out []string
	for i, e := range entries {
		if i == maxListEntries {
			break
		}
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		out = append(out, name)
	}
	if n := len(entries) - maxListEntries; n > 0 {
		out = append(out, fmt.Sprintf("[... %d more entries]", n))
	}
	return Result{Output: strings.Join(out, "\n"), Brief: fmt.Sprintf("%d entries", len(entries))}, nil
}

// globFiles lists the files under root whose name matches glob, capped at 500.
func globFiles(ctx context.Context, env Env, root, glob string) (Result, error) {
	var out []string
	more := 0
	err := walkFiles(ctx, env.Root.FS(), root, glob, func(p string, _ fs.DirEntry) error {
		if len(out) < maxListEntries {
			out = append(out, p)
		} else {
			more++
		}
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	if len(out) == 0 {
		return Result{Output: "no matches", Brief: "0 files"}, nil
	}
	n := len(out) + more
	if more > 0 {
		out = append(out, fmt.Sprintf("[... %d more files]", more))
	}
	return Result{Output: strings.Join(out, "\n"), Brief: fmt.Sprintf("%d files", n)}, nil
}
