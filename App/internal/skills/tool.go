package skills

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"swarg/internal/chat"
	"swarg/internal/tools"
)

// ---------------------------------------------------------------------------
// The skill Tool (R-82)
// ---------------------------------------------------------------------------

// Tool applies a Skill: its result is the body of SKILL.md, or one file of the Skill folder.
type Tool struct{ set *Set }

var (
	_ tools.Tool    = Tool{}
	_ tools.Stubber = Tool{}
)

// Spec describes the skill Tool for the model.
func (Tool) Spec() chat.ToolSpec {
	return chat.ToolSpec{
		Name:        "skill",
		Description: "Load the instructions of a skill from the list.",
		Schema: json.RawMessage(`{"type":"object","properties":{` +
			`"name":{"type":"string","description":"skill name"},` +
			`"file":{"type":"string","description":"a file of the skill folder instead of the instructions"}},` +
			`"required":["name"],"additionalProperties":false}`),
	}
}

// ReadOnly is true: applying a Skill changes nothing.
func (Tool) ReadOnly() bool { return true }

// Confirm is false: no approval.
func (Tool) Confirm() bool { return false }

// Block tells the model when to use skill.
func (Tool) Block() string {
	return "skill: load the instructions of a skill from the list below before doing what it covers.\n" +
		`Example: {"name":"deploy"}`
}

// Summary is the Skill name, plus the file when given.
func (Tool) Summary(args json.RawMessage) string {
	var a struct{ Name, File string }
	if json.Unmarshal(args, &a) != nil {
		return ""
	}
	if a.File != "" {
		return a.Name + "/" + a.File
	}
	return a.Name
}

// Run returns the body (with the list of the folder's other files) or the named file, confined
// to the Skill folder by os.Root. Unknown and disabled Skills are errors the model can act on.
func (t Tool) Run(_ context.Context, _ tools.Env, args json.RawMessage) (tools.Result, error) {
	var a struct {
		Name string `json:"name"`
		File string `json:"file"`
	}
	if err := tools.DecodeFor(t, args, &a, "name"); err != nil {
		return tools.Result{}, err
	}
	t.set.mu.Lock()
	k := t.set.find(a.Name)
	known := list(t.set.names(true))
	t.set.mu.Unlock()
	switch {
	case k == nil:
		return tools.Result{}, fmt.Errorf("unknown skill %q; enabled skills: %s", a.Name, known)
	case !k.Enabled:
		return tools.Result{}, fmt.Errorf("skill %q is disabled", a.Name)
	}
	text := k.body
	if a.File != "" {
		b, err := readIn(k.Dir, a.File)
		if err != nil {
			return tools.Result{}, err
		}
		text = strings.ReplaceAll(string(b), "\r\n", "\n")
	} else if files := otherFiles(k.Dir); len(files) > 0 {
		text += "\n\nfiles: " + strings.Join(files, ", ")
	}
	return tools.Result{Output: tools.Cap(text), Brief: fmt.Sprintf("%d lines", strings.Count(text, "\n")+1)}, nil
}

// Stub shortens an applied Skill in history (the body can be long; the model may call again).
func (Tool) Stub(args json.RawMessage, _ tools.Result) string {
	return fmt.Sprintf("[skill %s: applied earlier; call it again if needed]", Tool{}.Summary(args))
}

// readIn reads name inside dir; a path leading out of dir is refused.
func readIn(dir, name string) ([]byte, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	rel := filepath.Clean(filepath.FromSlash(name))
	if filepath.IsAbs(rel) || !filepath.IsLocal(rel) {
		return nil, errors.New("file is outside the skill folder")
	}
	b, err := root.ReadFile(rel)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("no file %q in the skill folder; files: %s", name, strings.Join(otherFiles(dir), ", "))
		}
		return nil, errors.New("file is outside the skill folder")
	}
	return b, nil
}

// otherFiles lists the files of the Skill folder except SKILL.md, slash paths, sorted.
func otherFiles(dir string) []string {
	var out []string
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if rel, _ := filepath.Rel(dir, p); rel != skillFile {
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(out)
	return out
}
