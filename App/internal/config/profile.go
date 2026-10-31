package config

import (
	"fmt"
	"slices"
	"strings"
)

// ---------------------------------------------------------------------------
// Prompt Profile selection (R-116): which profile the SystemPrompt starts with
// ---------------------------------------------------------------------------

// ProfileNames are the prompt profiles, smallest first. The prompt package owns the texts; a
// test in cmd/swarg keeps this list equal to prompt.Profiles.
var ProfileNames = []string{"compact", "standard", "full"}

// DefaultProfile is the profile when nothing in the config names one.
const DefaultProfile = "compact"

// checkProfile validates one profile name from the file; the error names the config key.
func checkProfile(key, name string) error {
	if slices.Contains(ProfileNames, name) {
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
	for _, glob := range sortedKeys(p.Profiles) {
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
// else `providers.<provider>.profile`, else `prompt.profile`, else compact. Case is ignored in the
// patterns; `*` matches any run of characters (also `/`) and `?` one character.
func (c Config) ProfileFor(provider, model string) string {
	best := ""
	for _, glob := range sortedKeys(c.Prompt.Profiles) {
		if globMatch(strings.ToLower(glob), strings.ToLower(model)) && len(glob) > len(best) {
			best = glob
		}
	}
	if best != "" {
		return c.Prompt.Profiles[best]
	}
	if p := c.Providers[provider].Profile; p != "" {
		return p
	}
	if c.Prompt.Profile != "" {
		return c.Prompt.Profile
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
