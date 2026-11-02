// Package agents loads sub-agent TYPES (markdown files with front matter: description, tools,
// model choice, profile; the body is the agent's own instructions) from the built-ins, the
// global config dir and the project, and renders the list for the system prompt (T13 part C).
package agents

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"swarg/internal/prompt"
	"swarg/internal/skills"
	"swarg/internal/tools"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

const (
	SourceBuiltin = "builtin"
	SourceGlobal  = "global"
	SourceProject = "project"
	ProjectDir    = ".swarg/agents" // under the project (working) directory

	descriptionCut = 160 // runes of a description shown in the Block
	maxStepsLimit  = 200
	inherit        = "inherit"
)

// namePattern is the allowed shape of an agent type name (the file stem).
var namePattern = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

// Type is one agent type.
type Type struct {
	Name, Description string
	Server, Model     string   // "" or "inherit" = the Session's current Source / model
	Profile           string   // "" or "inherit" -> low; else low|medium|high (an old name is read as the new one, R-134)
	Tools             []string // tool names; "*" = every tool; "readonly" = every read-only tool; empty = ["*"]
	MaxSteps          int      // 0 = the default of the agent runtime
	Body              string   // markdown body = the agent's own instructions
	Source, Path      string   // SourceBuiltin/Global/Project; Path "" for built-ins
	Enabled           bool
}

// Info is what the UI shows about a type.
type Info struct {
	Name, Description, Source, Path string
	Enabled                         bool
}

// Set is the loaded agent types, sorted by name. Safe for concurrent use.
type Set struct {
	mu                         sync.Mutex
	types                      []*Type
	globalDir, projectDir, sig string // where the types came from and a fingerprint of those folders (Reload)
}

// ValidName reports whether name is an allowed agent type name.
func ValidName(name string) bool { return namePattern.MatchString(name) }

// ---------------------------------------------------------------------------
// Loading
// ---------------------------------------------------------------------------

// Load reads the built-ins, then <globalDir>/*.md, then <projectDir>/.swarg/agents/*.md; a later
// type replaces an earlier one of the same name. disabled names start off. A missing dir means
// none. The warnings name skipped files and unknown disabled names.
func Load(globalDir, projectDir string, disabled []string) (*Set, []string) {
	set := &Set{globalDir: globalDir, projectDir: projectDir}
	warnings := set.fill()
	for _, name := range disabled {
		if set.find(name) == nil {
			warnings = append(warnings, fmt.Sprintf("agents: unknown agent type %q", name))
			continue
		}
		_ = set.SetEnabled(name, false)
	}
	return set, warnings
}

// folders lists the agent folders in precedence order, weakest first: project beats global
// beats the built-ins.
func (s *Set) folders() []struct{ dir, source string } {
	return []struct{ dir, source string }{
		{s.globalDir, SourceGlobal},
		{filepath.Join(s.projectDir, filepath.FromSlash(ProjectDir)), SourceProject},
	}
}

// fill (re)reads every type from the built-ins and the folders into s.types; the caller holds
// the lock or owns s alone. Every type starts enabled.
func (s *Set) fill() []string {
	byName := map[string]*Type{}
	var warnings []string
	for _, t := range builtins() {
		t.Source, t.Enabled = SourceBuiltin, true
		byName[t.Name] = &t
	}
	for _, src := range s.folders() {
		entries, err := os.ReadDir(src.dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			stem, ok := strings.CutSuffix(e.Name(), ".md")
			if e.IsDir() || !ok {
				continue
			}
			path := filepath.Join(src.dir, e.Name())
			t, err := read(path, stem, src.source)
			if err != nil {
				warnings = append(warnings, "agents: "+err.Error())
				continue
			}
			byName[t.Name] = t
		}
	}
	s.types = slices.SortedFunc(maps.Values(byName), func(a, b *Type) int { return strings.Compare(a.Name, b.Name) })
	s.sig = s.signature()
	return warnings
}

// signature fingerprints the agent folders: file names, sizes and modification times.
func (s *Set) signature() string {
	var b strings.Builder
	for _, src := range s.folders() {
		entries, _ := os.ReadDir(src.dir)
		for _, e := range entries {
			if info, err := e.Info(); err == nil && !e.IsDir() {
				fmt.Fprintf(&b, "%s|%d|%d\n", filepath.Join(src.dir, e.Name()), info.Size(), info.ModTime().UnixNano())
			}
		}
		b.WriteString("--\n")
	}
	return b.String()
}

// Reload re-reads the folders when a file was added, changed or removed since the last read and
// reports whether it did (R-129). Which types are off is kept by name; a new type starts on.
func (s *Set) Reload() (changed bool, warnings []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.signature() == s.sig {
		return false, nil
	}
	off := map[string]bool{}
	for _, t := range s.types {
		off[t.Name] = !t.Enabled
	}
	warnings = s.fill()
	for _, t := range s.types {
		t.Enabled = !off[t.Name]
	}
	return true, warnings
}

// read loads one agent file; the file stem is the type's name.
func read(path, name, source string) (*Type, error) {
	if !ValidName(name) {
		return nil, fmt.Errorf("%s: file name must match %s", path, namePattern)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", path, err)
	}
	meta, body := skills.FrontMatter(strings.ReplaceAll(string(b), "\r\n", "\n"))
	if meta["description"] == "" {
		return nil, fmt.Errorf("%s: no description", path)
	}
	t := &Type{
		Name: name, Description: meta["description"], Server: meta["server"], Model: meta["model"],
		Profile: meta["profile"], Body: body, Source: source, Path: path, Enabled: true,
	}
	if t.Profile != "" && t.Profile != inherit {
		pr, err := prompt.ParseProfile(t.Profile)
		if err != nil {
			return nil, fmt.Errorf("%s: profile %q must be inherit, low, medium or high", path, t.Profile)
		}
		t.Profile = string(pr)
	}
	for _, item := range strings.Split(meta["tools"], ",") {
		if item = strings.TrimSpace(item); item != "" {
			t.Tools = append(t.Tools, item)
		}
	}
	if len(t.Tools) == 0 {
		t.Tools = []string{"*"}
	}
	if v, ok := meta["max_steps"]; ok {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxStepsLimit {
			return nil, fmt.Errorf("%s: max_steps %q must be an integer 1..%d", path, v, maxStepsLimit)
		}
		t.MaxSteps = n
	}
	return t, nil
}

// ---------------------------------------------------------------------------
// Set
// ---------------------------------------------------------------------------

// List returns every type in name order.
func (s *Set) List() []Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Info, 0, len(s.types))
	for _, t := range s.types {
		out = append(out, Info{Name: t.Name, Description: t.Description, Source: t.Source, Path: t.Path, Enabled: t.Enabled})
	}
	return out
}

// Enabled returns copies of the enabled types in name order.
func (s *Set) Enabled() []Type {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Type
	for _, t := range s.types {
		if t.Enabled {
			out = append(out, copyType(t))
		}
	}
	return out
}

// Get returns a copy of the named type, enabled or not.
func (s *Set) Get(name string) (Type, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t := s.find(name); t != nil {
		return copyType(t), true
	}
	return Type{}, false
}

// SetEnabled switches a type on or off; an unknown name is an error listing the known names.
func (s *Set) SetEnabled(name string, on bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.find(name)
	if t == nil {
		var names []string
		for _, k := range s.types {
			names = append(names, k.Name)
		}
		return fmt.Errorf("unknown agent type %q; known agent types: %s", name, strings.Join(names, ", "))
	}
	t.Enabled = on
	return nil
}

// Block is the text of the prompt layer `subagents`; "" when no type is enabled.
func (s *Set) Block() string {
	var b strings.Builder
	for _, t := range s.Enabled() {
		desc := strings.Join(strings.Fields(t.Description), " ")
		fmt.Fprintf(&b, "- %s: %s\n", t.Name, tools.ClipRunes(desc, descriptionCut))
	}
	if b.Len() == 0 {
		return ""
	}
	return blockHead + "\nTypes:\n" + b.String() + blockTail
}

const blockHead = `Sub-agents: hand a self-contained job to a helper with the agent tool {"type": "<type>", "goal": "<what to do>"}. The helper starts with an empty history and sees only the goal, so put everything it needs in the goal: files, names, constraints, what to return, and whether it may edit files. It returns one short report.`

const blockTail = `Delegate when: the job reads many files or prints long output, or is clearly separable (search, review, an independent change). Several agent calls in one step run in parallel (up to 4); give parallel agents different files.
Do not delegate: a single known file to read, one grep, or a step you need the result of right now to decide the next one. After a helper reports, check its result before you rely on it.`

func (s *Set) find(name string) *Type {
	if i := slices.IndexFunc(s.types, func(t *Type) bool { return t.Name == name }); i >= 0 {
		return s.types[i]
	}
	return nil
}

func copyType(t *Type) Type {
	c := *t
	c.Tools = append([]string(nil), t.Tools...)
	return c
}
