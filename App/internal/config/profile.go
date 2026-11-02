package config

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// ---------------------------------------------------------------------------
// Prompt Profile selection (R-116): which profile the SystemPrompt starts with
// ---------------------------------------------------------------------------

// ProfileNames are the prompt profiles, smallest first. The prompt package owns the texts; a
// test in cmd/swarg keeps this list equal to prompt.Profiles.
var ProfileNames = []string{"low", "medium", "high"}

// profileAliases are the old profile names (before the 2026-11-01 rename), still accepted in a
// config file and always read as the new name (R-134).
var profileAliases = map[string]string{"compact": "low", "standard": "medium", "full": "high"}

// DefaultProfile is the profile when nothing in the config names one.
const DefaultProfile = "low"

// CanonicalProfile maps an old profile name to the new one; any other name comes back as it is.
func CanonicalProfile(name string) string {
	if n, ok := profileAliases[name]; ok {
		return n
	}
	return name
}

// normalizeProfiles rewrites the old profile names in prompt.profile, prompt.profiles and the
// providers to the new ones, so the rest of the app only sees the new names (R-134).
func (c *Config) normalizeProfiles() {
	c.Prompt.Profile = CanonicalProfile(c.Prompt.Profile)
	for glob, name := range c.Prompt.Profiles {
		c.Prompt.Profiles[glob] = CanonicalProfile(name)
	}
	for key, p := range c.Providers {
		p.Profile = CanonicalProfile(p.Profile)
		c.Providers[key] = p
	}
}

// checkProfile validates one profile name from the file; the error names the config key.
func checkProfile(key, name string) error {
	if slices.Contains(ProfileNames, CanonicalProfile(name)) {
		return nil
	}
	return fmt.Errorf("config: %s must be one of %s, got %q", key, strings.Join(ProfileNames, ", "), name)
}

// validateProfiles checks prompt.profile and the prompt.profiles map (glob -> profile).
func (p Prompt) validateProfiles() error {
	if p.Profile != "" {
		if err := checkProfile("prompt.profile", p.Profile); err != nil {
			return err
		}
	}
	for _, glob := range slices.Sorted(maps.Keys(p.Profiles)) {
		if strings.TrimSpace(glob) == "" {
			return fmt.Errorf("config: prompt.profiles has an empty model pattern")
		}
		if err := checkProfile("prompt.profiles."+glob, p.Profiles[glob]); err != nil {
			return err
		}
	}
	return nil
}

// ProfileFor is the prompt profile of a Source and a model when the user has not chosen one: the
// most specific `prompt.profiles` pattern that matches the model (the longest; ties alphabetical),
// else `providers.<provider>.profile`, else `prompt.profile`, else low (an old name reads as its new one, R-134). Case is ignored in the
// patterns; `*` matches any run of characters (also `/`) and `?` one character.
func (c Config) ProfileFor(provider, model string) string {
	best := ""
	for _, glob := range slices.Sorted(maps.Keys(c.Prompt.Profiles)) {
		if globMatch(strings.ToLower(glob), strings.ToLower(model)) && len(glob) > len(best) {
			best = glob
		}
	}
	if best != "" {
		return CanonicalProfile(c.Prompt.Profiles[best])
	}
	if p := c.Providers[provider].Profile; p != "" {
		return CanonicalProfile(p)
	}
	if c.Prompt.Profile != "" {
		return CanonicalProfile(c.Prompt.Profile)
	}
	return DefaultProfile
}

// globMatch reports whether s matches the pattern: `*` any run (maybe empty), `?` one byte.
func globMatch(pattern, s string) bool {
	for len(pattern) > 0 {
		switch pattern[0] {
		case '*':
			for len(pattern) > 0 && pattern[0] == '*' {
				pattern = pattern[1:]
			}
			if pattern == "" {
				return true
			}
			for i := 0; i <= len(s); i++ {
				if globMatch(pattern, s[i:]) {
					return true
				}
			}
			return false
		case '?':
			if s == "" {
				return false
			}
		default:
			if s == "" || s[0] != pattern[0] {
				return false
			}
		}
		pattern, s = pattern[1:], s[1:]
	}
	return s == ""
}
