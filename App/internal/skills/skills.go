// Package skills loads Skills (folders with a SKILL.md: front matter name and description, body
// of instructions) from the global config dir and the project, lists them for the system prompt
// and applies one through the `skill` Tool (R-80..R-84).
package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"swarg/internal/tools"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

const (
	skillFile       = "SKILL.md"
	descriptionCut  = 160 // runes of a description shown in the list (R-80)
	manySkills      = 20  // above this many enabled Skills a warning is given (R-84)
	SourceGlobal    = "global"
	SourceProject   = "project"
	projectSkillDir = ".swarg/skills" // under the working directory
)

// namePattern is the allowed shape of a Skill name (its folder name).
var namePattern = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

// Skill is one loaded Skill.
type Skill struct {
	Name        string
	Description string
	Source      string // SourceGlobal or SourceProject
	Dir         string // the folder, absolute
	Enabled     bool
	body        string // SKILL.md without the front matter
}

// Info is what the UI shows about a Skill.
type Info struct {
	Name, Description, Source string
	Enabled                   bool
}

// Set is the loaded Skills, sorted by name. Safe for concurrent use.
type Set struct {
	mu     sync.Mutex
	skills []*Skill
}

// ---------------------------------------------------------------------------
// Loading
// ---------------------------------------------------------------------------

// Load reads <globalDir>/<name>/SKILL.md and <projectDir>/.swarg/skills/<name>/SKILL.md; a
// project Skill replaces a global one of the same name (R-80). disabled names start off (R-84).
// A missing dir means no Skills from it. The warnings name skipped folders and unknown disabled names.
func Load(globalDir, projectDir string, disabled []string) (*Set, []string) {
	byName := map[string]*Skill{}
	var warnings []string
	for _, src := range []struct{ dir, source string }{
		{globalDir, SourceGlobal},
		{filepath.Join(projectDir, filepath.FromSlash(projectSkillDir)), SourceProject},
	} {
		entries, err := os.ReadDir(src.dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			s, err := read(filepath.Join(src.dir, e.Name()), src.source)
			if err != nil {
				warnings = append(warnings, "skills: "+err.Error())
				continue
			}
			byName[s.Name] = s
		}
	}
	set := &Set{}
	for _, s := range byName {
		set.skills = append(set.skills, s)
	}
	sort.Slice(set.skills, func(i, j int) bool { return set.skills[i].Name < set.skills[j].Name })
	for _, name := range disabled {
		if err := set.SetEnabled(name, false); err != nil {
			warnings = append(warnings, "skills: "+err.Error())
		}
	}
	if n := set.enabled(); n > manySkills {
		warnings = append(warnings, fmt.Sprintf("skills: %d skills listed, the small models pick worse from long lists", n))
	}
	return set, warnings
}

// read loads one Skill folder: the folder name is the Skill's name; the front matter must carry
// a description; a front-matter name that differs from the folder is kept as the folder's.
func read(dir, source string) (*Skill, error) {
	name := filepath.Base(dir)
	if !namePattern.MatchString(name) {
		return nil, fmt.Errorf("%s: folder name must match %s", dir, namePattern)
	}
	b, err := os.ReadFile(filepath.Join(dir, skillFile))
	if err != nil {
		return nil, fmt.Errorf("%s: no %s", dir, skillFile)
	}
	meta, body := frontMatter(strings.ReplaceAll(string(b), "\r\n", "\n"))
	if meta["description"] == "" {
		return nil, fmt.Errorf("%s: no description in the front matter of %s", dir, skillFile)
	}
	return &Skill{Name: name, Description: meta["description"], Source: source, Dir: dir, Enabled: true, body: body}, nil
}

// frontMatter splits a "---" block of key: value lines from the body. No block: empty meta.
func frontMatter(text string) (meta map[string]string, body string) {
	meta = map[string]string{}
	rest, ok := strings.CutPrefix(text, "---\n")
	if !ok {
		return meta, strings.TrimSpace(text)
	}
	block, body, ok := strings.Cut(rest, "\n---")
	if !ok {
		return meta, strings.TrimSpace(text)
	}
	for _, line := range strings.Split(block, "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok {
			meta[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return meta, strings.TrimSpace(body)
}

// ---------------------------------------------------------------------------
// Set
// ---------------------------------------------------------------------------

// Len is the number of loaded Skills (enabled or not).
func (s *Set) Len() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.skills)
}

// List returns every Skill in name order.
func (s *Set) List() []Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Info, 0, len(s.skills))
	for _, k := range s.skills {
		out = append(out, Info{Name: k.Name, Description: k.Description, Source: k.Source, Enabled: k.Enabled})
	}
	return out
}

// SetEnabled switches a Skill on or off; an unknown name is an error listing the known names.
func (s *Set) SetEnabled(name string, on bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := s.find(name)
	if k == nil {
		return fmt.Errorf("unknown skill %q; known skills: %s", name, s.names(false))
	}
	k.Enabled = on
	return nil
}

// Block is the Skill list for the system prompt: one line per enabled Skill, "" when none (R-81).
func (s *Set) Block() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var b strings.Builder
	for _, k := range s.skills {
		if k.Enabled {
			fmt.Fprintf(&b, "\n- %s: %s", k.Name, tools.ClipRunes(k.Description, descriptionCut))
		}
	}
	if b.Len() == 0 {
		return ""
	}
	return "Skills (apply one with the skill tool when the task matches):" + b.String()
}

// Tool is the `skill` Tool bound to this Set.
func (s *Set) Tool() tools.Tool { return Tool{set: s} }

func (s *Set) find(name string) *Skill {
	for _, k := range s.skills {
		if k.Name == name {
			return k
		}
	}
	return nil
}

// names lists the Skill names, the enabled ones only when enabledOnly; "none" when empty.
func (s *Set) names(enabledOnly bool) string {
	var out []string
	for _, k := range s.skills {
		if k.Enabled || !enabledOnly {
			out = append(out, k.Name)
		}
	}
	if len(out) == 0 {
		return "none"
	}
	return strings.Join(out, ", ")
}

func (s *Set) enabled() int {
	n := 0
	for _, k := range s.skills {
		if k.Enabled {
			n++
		}
	}
	return n
}
