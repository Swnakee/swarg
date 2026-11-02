package prompt

import (
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Old Profile names (R-134)
// ---------------------------------------------------------------------------

func TestParseProfile_OldNamesGiveTheNewOnes(t *testing.T) {
	for old, want := range map[string]Profile{"compact": Low, "standard": Medium, "full": High} {
		if got, err := ParseProfile(old); err != nil || got != want {
			t.Errorf("ParseProfile(%q) = %q, %v, want %q", old, got, err, want)
		}
	}
	if _, err := ParseProfile("big"); err == nil || strings.Contains(err.Error(), "compact") {
		t.Errorf("an unknown name must list only the new names, got %v", err)
	}
}

func TestLoad_OldProfileOverrideFileStillApplies(t *testing.T) {
	global := t.TempDir()
	writeFile(t, filepath.Join(global, "profile-standard.md"), "OLD MEDIUM\n")
	writeFile(t, filepath.Join(global, "profile-full.md"), "OLD HIGH\n")
	writeFile(t, filepath.Join(global, "profile-high.md"), "NEW HIGH\n") // the new name wins
	cwd := t.TempDir()
	s, _, err := Load(global, cwd, testEnv(cwd))
	if err != nil {
		t.Fatal(err)
	}
	_ = s.SetProfile(High)
	if got := s.Build(); !strings.Contains(got, "OLD MEDIUM\n\nNEW HIGH") || strings.Contains(got, "OLD HIGH") {
		t.Errorf("old override files not mapped:\n%.300s", got)
	}
}

func TestSetProfile_OldNameIsStoredAsTheNewOne(t *testing.T) {
	s, _ := loadProfile(t, t.TempDir())
	if err := s.SetProfile(Profile("full")); err != nil {
		t.Fatal(err)
	}
	if s.Profile() != High || !strings.Contains(s.Build(), strings.TrimSpace(highText)) {
		t.Errorf("Profile() = %q, want high with its layer", s.Profile())
	}
	if !strings.Contains(s.BuildAgent(Profile("standard"), "m", "", nil), strings.TrimSpace(mediumText)) {
		t.Error("BuildAgent with an old name must carry the medium layer")
	}
}
