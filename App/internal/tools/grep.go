package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// grep
// ---------------------------------------------------------------------------

const (
	maxGrepMatches = 60
	maxGrepFile    = 2 << 20 // files over 2 MB are skipped
	grepLineMax    = 160     // bytes of a matching line shown
)

// Grep searches text files under a path for a Go regular expression.
type Grep struct{}

// Spec describes grep for the model.
func (Grep) Spec() chat.ToolSpec {
	return chat.ToolSpec{
		Name:        "grep",
		Description: "Search files for a Go regex. Matches: path:line: text.",
		Schema: json.RawMessage(`{"type":"object","properties":{` +
			`"pattern":{"type":"string","description":"Go regular expression"},` +
			`"path":{"type":"string","description":"folder or file (default \".\")"},` +
			`"glob":{"type":"string","description":"file name pattern, e.g. *.go"}},` +
			`"required":["pattern"]}`),
	}
}

// ReadOnly is true: grep never changes files.
func (Grep) ReadOnly() bool { return true }

// Confirm is false: grep runs without approval.
func (Grep) Confirm() bool { return false }

// Block is the system prompt text for grep.
func (Grep) Block() string {
	return "grep: find text in project files; lines are path:line: text. Locate code before reading it.\n" +
		`Example: {"pattern": "func Run", "path": "internal", "glob": "*.go"}`
}

// Summary is "pattern in path", or the pattern alone, or "" when the args are not valid JSON.
func (Grep) Summary(args json.RawMessage) string {
	var a struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
	}
	if json.Unmarshal(args, &a) != nil {
		return ""
	}
	if a.Path == "" {
		return a.Pattern
	}
	return a.Pattern + " in " + a.Path
}

// Run walks the path in lexical order and returns up to 60 matching lines.
// Skips .git, node_modules, binary files and files over 2 MB.
func (t Grep) Run(ctx context.Context, env Env, args json.RawMessage) (Result, error) {
	var a struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
		Glob    string `json:"glob"`
	}
	if err := DecodeFor(t, args, &a, "pattern"); err != nil {
		return Result{}, err
	}
	re, err := regexp.Compile(a.Pattern)
	if err != nil {
		return Result{}, fmt.Errorf("invalid pattern: %w", err)
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

	root := filepath.ToSlash(rel)
	fsys := env.Root.FS()
	var matches []string
	more := false
	err = walkFiles(ctx, fsys, root, a.Glob, func(p string, d fs.DirEntry) error {
		info, err := d.Info()
		if err != nil || info.Size() > maxGrepFile {
			return nil
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil || isBinary(data) {
			return nil
		}
		for i, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSuffix(line, "\r")
			if !re.MatchString(line) {
				continue
			}
			if len(matches) == maxGrepMatches {
				more = true
				return fs.SkipAll
			}
			matches = append(matches, fmt.Sprintf("%s:%d: %s", p, i+1, clipBytes(line, grepLineMax)))
		}
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	if len(matches) == 0 {
		return Result{Output: "no matches", Brief: "0 matches"}, nil
	}
	out := strings.Join(matches, "\n")
	if more {
		out += "\n[... more matches]"
	}
	return Result{Output: out, Brief: fmt.Sprintf("%d matches", len(matches))}, nil
}

// walkFiles calls visit for every file under root whose base name matches glob ("" = all), in
// lexical order, skipping .git and node_modules. An unreadable entry is skipped; visit may return
// fs.SkipAll to stop.
func walkFiles(ctx context.Context, fsys fs.FS, root, glob string, visit func(p string, d fs.DirEntry) error) error {
	return fs.WalkDir(fsys, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == root {
				return err
			}
			return nil
		}
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if d.IsDir() {
			if p != root && (d.Name() == ".git" || d.Name() == "node_modules") {
				return fs.SkipDir
			}
			return nil
		}
		if glob != "" {
			if ok, _ := filepath.Match(glob, d.Name()); !ok {
				return nil
			}
		}
		return visit(p, d)
	})
}
