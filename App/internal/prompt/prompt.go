// Package prompt assembles the system prompt from named Blocks: the built-in
// base text, global NN-name.md files, the project rules file (SWARG.md, else AGENTS.md, else CLAUDE.md) and the env block.
// Blocks can be switched on and off at runtime.
package prompt

import (
	"cmp"
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"swarg/internal/chat"
	"sync"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// Source tells where a Block comes from.
type Source string

// Block sources.
const (
	SourceBase    Source = "base"
	SourceGlobal  Source = "global"
	SourceProject Source = "project"
	SourceEnv     Source = "env"
	SourceTool    Source = "tool"
)

// Fixed block names and orders.
const (
	nameBase    = "base"
	nameProject = "project"
	nameEnv     = "env"

	orderBase    = 0
	orderProject = 1000
	orderEnv     = 2000
)

// projectFiles are the project rules files in order of preference; the first one found in
// the working directory is read, the others are ignored (R-135).
var projectFiles = []string{"SWARG.md", "AGENTS.md", "CLAUDE.md"}

//go:embed base.md
var baseText string

// Block is one piece of the system prompt.
type Block struct {
	Name    string // file stem without "NN-"; fixed names "base", "project", "env"
	Source  Source
	Order   int // global NN; base=0, project=1000, env=2000; ties sorted by Name
	Text    string
	Enabled bool
}

// Env describes the session for the env block.
type Env struct {
	OS, Shell, Dir, Date, Model string
	Git                         bool // Dir is inside a git work tree (InGitRepo, R-136)
}

// BlockInfo is a read-only view of a Block for listings.
type BlockInfo struct {
	Name    string
	Source  Source
	Enabled bool
	Tokens  int // chat.EstimateTokens of the block text as Build emits it
}

// Set is the ordered list of Blocks. It is safe for concurrent use.
type Set struct {
	mu     sync.Mutex
	blocks []Block // sorted by Order, then Name
	env    Env     // what the env block describes

	layers  []layer // the Profile layers, in or out of blocks (profile.go)
	profile Profile // the Profile in force
}

// ---------------------------------------------------------------------------
// Load
// ---------------------------------------------------------------------------

// Load builds the set: base (embedded base.md) + global NN-name.md files from
// globalDir (a missing dir means none) + the first of SWARG.md, AGENTS.md, CLAUDE.md found in cwd only (R-135) + the env block
// + the extra blocks (for example tool blocks), enabled and added as given.
// A <name>.md file in globalDir replaces the text of the base or tool Block of that name
// (global, project and env Blocks cannot be replaced) and is reported in warnings (R-49).
// Other entries that do not match ^\d{2}-[a-z0-9_-]+\.md$ are skipped and reported in
// warnings. A duplicate name (including base, project, env and extra) is an error.
func Load(globalDir, cwd string, env Env, extra ...Block) (set *Set, warnings []string, err error) {
	blocks := []Block{{Name: nameBase, Source: SourceBase, Order: orderBase, Text: baseText, Enabled: true}}

	global, overrides, warnings, err := loadGlobal(globalDir)
	if err != nil {
		return nil, nil, err
	}
	if err := checkUnique(append(append([]Block(nil), global...), extra...)); err != nil {
		return nil, nil, err
	}
	blocks = append(blocks, global...)

	for _, name := range projectFiles {
		project, err := readText(filepath.Join(cwd, name))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		blocks = append(blocks, Block{Name: nameProject, Source: SourceProject, Order: orderProject, Text: project, Enabled: true})
		break
	}

	blocks = append(blocks, Block{Name: nameEnv, Source: SourceEnv, Order: orderEnv, Text: envText(env), Enabled: true})
	for _, b := range extra {
		b.Enabled = true
		blocks = append(blocks, b)
	}
	layers := defaultLayers()
	stems := map[string]string{} // layer name -> file stem, when the file has an old name (R-134)
	for old, name := range map[string]string{oldNameMedium: nameMedium, oldNameHigh: nameHigh} {
		if text, ok := overrides[old]; ok {
			delete(overrides, old)
			if _, newer := overrides[name]; !newer {
				overrides[name], stems[name] = text, old
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(overrides)) { // sorted: stable warnings
		path := filepath.Join(globalDir, cmp.Or(stems[name], name)+".md")
		if k := layerIndex(layers, name); k >= 0 { // a Profile layer (R-119)
			layers[k].Text = overrides[name]
			warnings = append(warnings, fmt.Sprintf("prompt: block %q replaced by %s", name, path))
			continue
		}
		i := slices.IndexFunc(blocks, func(b Block) bool { return b.Name == name })
		if i < 0 || (blocks[i].Source != SourceBase && blocks[i].Source != SourceTool) {
			warnings = append(warnings, fmt.Sprintf("prompt: skipped %s: not an NN-name.md file or a replaceable Block", path))
			continue
		}
		blocks[i].Text = overrides[name]
		warnings = append(warnings, fmt.Sprintf("prompt: block %q replaced by %s", name, path))
	}

	sortBlocks(blocks)
	return &Set{blocks: blocks, env: env, layers: layers, profile: Low}, warnings, nil
}

// sortBlocks orders Blocks by Order, then Name.
func sortBlocks(blocks []Block) {
	slices.SortFunc(blocks, func(a, b Block) int {
		return cmp.Or(cmp.Compare(a.Order, b.Order), strings.Compare(a.Name, b.Name))
	})
}

// checkUnique rejects a block whose name repeats another checked name or a
// reserved fixed name (including the Profile layer names). The fixed names count even when their block is absent.
func checkUnique(blocks []Block) error {
	seen := map[string]bool{nameBase: true, nameProject: true, nameEnv: true, nameMedium: true, nameHigh: true, nameSubagent: true}
	for _, b := range blocks {
		if seen[b.Name] {
			return fmt.Errorf("prompt: duplicate block name %q", b.Name)
		}
		seen[b.Name] = true
	}
	return nil
}

// globalFile matches NN-name.md (a block) and name.md (an override candidate): group 1 is NN or empty, group 2 the name.
var globalFile = regexp.MustCompile(`^(?:(\d{2})-)?([a-z0-9_-]+)\.md$`)

// loadGlobal reads globalDir: NN-name.md files become blocks, plain name.md files are
// override candidates (stem -> text, resolved by Load); every other entry gets a warning.
func loadGlobal(dir string) (blocks []Block, overrides map[string]string, warnings []string, err error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil, nil
	}
	if err != nil {
		return nil, nil, nil, fmt.Errorf("prompt: read dir %s: %w", dir, err)
	}
	overrides = map[string]string{}
	for _, e := range entries {
		m := globalFile.FindStringSubmatch(e.Name())
		if e.IsDir() || m == nil {
			warnings = append(warnings, fmt.Sprintf("prompt: skipped %s: not an NN-name.md file", filepath.Join(dir, e.Name())))
			continue
		}
		text, err := readText(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, nil, nil, err
		}
		if m[1] == "" {
			overrides[m[2]] = text
			continue
		}
		order, _ := strconv.Atoi(m[1]) // the regexp guarantees two digits
		blocks = append(blocks, Block{Name: m[2], Source: SourceGlobal, Order: order, Text: text, Enabled: true})
	}
	return blocks, overrides, warnings, nil
}

// readText reads a file and normalizes CRLF line endings to LF.
func readText(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("prompt: read %s: %w", path, err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n"), nil
}

// envText renders the env block lines.
func envText(e Env) string {
	git := "no"
	if e.Git {
		git = "yes"
	}
	return fmt.Sprintf("OS: %s\nShell: %s\nWorking directory: %s\nGit repo: %s\nDate: %s\nModel: %s",
		e.OS, e.Shell, e.Dir, git, e.Date, e.Model)
}

// InGitRepo reports whether dir or a parent folder holds a .git entry (a directory, or a file
// in a linked work tree). It does not run git (R-136).
func InGitRepo(dir string) bool {
	for {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return true
		}
		up := filepath.Dir(dir)
		if up == dir {
			return false
		}
		dir = up
	}
}

// ---------------------------------------------------------------------------
// Set operations
// ---------------------------------------------------------------------------

// Build joins the enabled blocks, in order, separated by a blank line; block names are internal
// (never sent to the model). Trailing whitespace of each text is trimmed.
func (s *Set) Build() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var parts []string
	for _, b := range s.blocks {
		if t := blockText(b); b.Enabled && t != "" { // block names are internal: never sent to the model
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, "\n\n")
}

// SetModel rewrites the env block for a new Model choice; Build shows it from the next call
// (the agent rebuilds the SystemPrompt at every Turn start, R-4). The block's on/off state stays.
func (s *Set) SetModel(model string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.env.Model = model
	for i := range s.blocks {
		if s.blocks[i].Name == nameEnv {
			s.blocks[i].Text = envText(s.env)
		}
	}
}

// SetText replaces the text of the named block (a block whose content changes at run time, e.g.
// the Skill list); Build shows it from the next call. An unknown name is an error.
func (s *Set) SetText(name, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	found := false
	if k := layerIndex(s.layers, name); k >= 0 { // a Profile layer keeps the text across profile switches
		s.layers[k].Text, found = text, true
	}
	for i := range s.blocks {
		if s.blocks[i].Name == name {
			s.blocks[i].Text = text
			found = true
		}
	}
	if !found {
		return fmt.Errorf("prompt: unknown block %q", name)
	}
	return nil
}

// SetEnabled switches the named block on or off. An unknown name is an error
// that lists the known names.
func (s *Set) SetEnabled(name string, on bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.blocks {
		if s.blocks[i].Name == name {
			s.blocks[i].Enabled = on
			return nil
		}
	}
	known := make([]string, 0, len(s.blocks))
	for _, b := range s.blocks {
		known = append(known, b.Name)
	}
	return fmt.Errorf("prompt: unknown block %q, known: %s", name, strings.Join(known, ", "))
}

// List returns every block in build order with its state and token estimate.
func (s *Set) List() []BlockInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]BlockInfo, 0, len(s.blocks))
	for _, b := range s.blocks {
		out = append(out, BlockInfo{Name: b.Name, Source: b.Source, Enabled: b.Enabled, Tokens: chat.EstimateTokens(blockText(b))})
	}
	return out
}

// blockText is the text of a block as Build emits it.
func blockText(b Block) string {
	return strings.TrimRight(b.Text, " \t\r\n")
}
