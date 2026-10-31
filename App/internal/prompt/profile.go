package prompt

import (
	_ "embed"
	"fmt"
	"slices"
	"strings"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Prompt Profiles (R-115): compact, standard and full are cumulative layers over the base Block
// ---------------------------------------------------------------------------

// Profile names how much instruction text the SystemPrompt carries.
type Profile string

// The Profiles, smallest first. Compact is the base Block alone.
const (
	Compact  Profile = "compact"
	Standard Profile = "standard"
	Full     Profile = "full"
)

// Names of the layer Blocks. They are override file stems (R-119): `profile-standard.md`, ...
const (
	nameStandard = "profile-standard"
	nameFull     = "profile-full"
	nameSubagent = "subagents"
)

//go:embed standard.md
var standardText string

//go:embed full.md
var fullText string

// subagentText is the sub-agent policy. EMPTY on purpose: T13 part C fills it (SetText or this
// constant); an empty text leaves no gap in Build.
const subagentText = ""

// Profiles lists every Profile, smallest first.
func Profiles() []Profile { return []Profile{Compact, Standard, Full} }

// ParseProfile checks a profile name; the error lists the known ones.
func ParseProfile(s string) (Profile, error) {
	for _, p := range Profiles() {
		if string(p) == s {
			return p, nil
		}
	}
	return "", fmt.Errorf("unknown prompt profile %q, known: %s", s, strings.Join(profileNames(), ", "))
}

func profileNames() []string {
	var out []string
	for _, p := range Profiles() {
		out = append(out, string(p))
	}
	return out
}

// level is the rank of a Profile: compact 0, standard 1, full 2.
func (p Profile) level() int { return slices.Index(Profiles(), p) }

// layer is a Block that joins the SystemPrompt from a Profile on.
type layer struct {
	Block
	from Profile
}

// defaultLayers are the layers in build order. Their Order sits between the base (0) and the
// tool Blocks (500+), so the static text stays in front of the dynamic parts (prefix cache).
func defaultLayers() []layer {
	return []layer{
		{Block{Name: nameStandard, Source: SourceBase, Order: 50, Text: standardText, Enabled: true}, Standard},
		{Block{Name: nameFull, Source: SourceBase, Order: 60, Text: fullText, Enabled: true}, Full},
		{Block{Name: nameSubagent, Source: SourceBase, Order: 70, Text: subagentText, Enabled: true}, Standard},
	}
}

// layerIndex finds a layer by name, -1 when there is none.
func layerIndex(layers []layer, name string) int {
	return slices.IndexFunc(layers, func(l layer) bool { return l.Name == name })
}

// Profile is the Profile in force (compact after Load).
func (s *Set) Profile() Profile {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.profile
}

// SetProfile switches the Profile: the layers of the Profile join the Set and the others leave
// it, so List and Build show exactly what is sent. It applies from the next Build (the agent
// rebuilds the SystemPrompt every Turn, R-4). A layer switched off with SetEnabled is on again
// after a switch. An unknown name is an error and changes nothing.
func (s *Set) SetProfile(p Profile) error {
	if _, err := ParseProfile(string(p)); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.blocks, s.profile = s.layered(p), p
	return nil
}

// layered is the Block list with the layers of Profile p and none of the others, sorted; the
// other Blocks are kept as they are. Caller holds mu.
func (s *Set) layered(p Profile) []Block {
	kept := s.blocks[:0:0]
	for _, b := range s.blocks {
		if layerIndex(s.layers, b.Name) < 0 {
			kept = append(kept, b)
		}
	}
	for _, l := range s.layers {
		if l.from.level() <= p.level() {
			b := l.Block
			b.Enabled = true
			kept = append(kept, b)
		}
	}
	sortBlocks(kept)
	return kept
}

// BuildAgent is the SystemPrompt of a sub-agent (R-123): the Blocks of Profile p without the
// sub-agent policy (an agent cannot delegate), the Tool Blocks of the tools tool allows, the
// type's body, then the project file and the env block with the agent's own model. Switched-off
// Blocks stay out. An unknown Profile name builds as compact.
func (s *Set) BuildAgent(p Profile, model, body string, tool func(name string) bool) string {
	if _, err := ParseProfile(string(p)); err != nil {
		p = Compact
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	env := s.env
	env.Model = model
	var parts []string
	add := func(t string) {
		if t = strings.TrimRight(t, " \t\r\n"); t != "" {
			parts = append(parts, t)
		}
	}
	bodyDone := false
	for _, b := range s.layered(p) {
		switch {
		case !b.Enabled, b.Name == nameSubagent:
			continue
		case b.Source == SourceTool && (!strings.HasPrefix(b.Name, "tool-") || tool == nil || !tool(strings.TrimPrefix(b.Name, "tool-"))):
			continue
		}
		if !bodyDone && b.Order >= orderProject {
			add(body)
			bodyDone = true
		}
		if b.Name == nameEnv {
			b.Text = envText(env)
		}
		add(blockText(b))
	}
	if !bodyDone {
		add(body)
	}
	return strings.Join(parts, "\n\n")
}

// ProfileTokens is the estimated size of the instruction text of a Profile (the base Block and
// the layers, without tool Blocks, project file and env), for the /prompt menu.
func (s *Set) ProfileTokens(p Profile) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, b := range s.blocks {
		if b.Name == nameBase {
			n += chat.EstimateTokens(blockText(b))
		}
	}
	for _, l := range s.layers {
		if l.from.level() <= p.level() {
			n += chat.EstimateTokens(blockText(l.Block))
		}
	}
	return n
}
