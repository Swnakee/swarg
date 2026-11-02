package prompt

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Profiles (R-115, R-119, I-40)
// ---------------------------------------------------------------------------

func loadProfile(t *testing.T, global string) (*Set, string) {
	t.Helper()
	cwd := t.TempDir()
	s, _ := mustLoad(t, global, cwd)
	return s, cwd
}

func TestProfile_CompactByteIdentical(t *testing.T) {
	s, cwd := loadProfile(t, t.TempDir())
	if got, want := s.Build(), testBase+"\n\n"+envBlock(cwd); got != want {
		t.Errorf("low Build changed:\n%s\nwant:\n%s", got, want)
	}
	if s.Profile() != Low {
		t.Errorf("Profile() = %q after Load, want low", s.Profile())
	}
	if n := len(blockNames(s)); n != 2 {
		t.Errorf("low lists %d blocks (%v), want base and env only", n, blockNames(s))
	}
}

func TestSetProfile_LayersAreCumulative(t *testing.T) {
	s, cwd := loadProfile(t, t.TempDir())
	std, high := strings.TrimSpace(mediumText), strings.TrimSpace(highText)

	if err := s.SetProfile(Medium); err != nil {
		t.Fatal(err)
	}
	got := s.Build()
	if !strings.Contains(got, std) || strings.Contains(got, high) {
		t.Errorf("medium must carry the medium layer only")
	}
	if !strings.HasPrefix(got, testBase+"\n\n"+std) || !strings.HasSuffix(got, envBlock(cwd)) {
		t.Errorf("medium order wrong: base, layer, env expected")
	}

	if err := s.SetProfile(High); err != nil {
		t.Fatal(err)
	}
	got = s.Build()
	if !strings.Contains(got, std+"\n\n"+high) {
		t.Errorf("high must carry the medium layer and then the high layer")
	}

	if err := s.SetProfile(Low); err != nil {
		t.Fatal(err)
	}
	if got, want := s.Build(), testBase+"\n\n"+envBlock(cwd); got != want {
		t.Errorf("back to low is not byte-identical:\n%s", got)
	}
}

func TestSetProfile_LayersSitBeforeToolBlocks(t *testing.T) {
	cwd := t.TempDir()
	s, _, err := Load(t.TempDir(), cwd, testEnv(cwd), Block{Name: "tool-x", Source: SourceTool, Order: 500, Text: "TOOLX"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetProfile(High); err != nil {
		t.Fatal(err)
	}
	got := s.Build()
	if strings.Index(got, strings.TrimSpace(highText)) > strings.Index(got, "TOOLX") {
		t.Error("the layers must come before the tool blocks")
	}
}

func TestSetProfile_Unknown(t *testing.T) {
	s, _ := loadProfile(t, t.TempDir())
	_ = s.SetProfile(Medium)
	err := s.SetProfile("huge")
	if err == nil {
		t.Fatal("unknown profile accepted")
	}
	for _, p := range Profiles() {
		if !strings.Contains(err.Error(), string(p)) {
			t.Errorf("error %q does not list %q", err, p)
		}
	}
	if s.Profile() != Medium {
		t.Errorf("a refused switch changed the profile to %q", s.Profile())
	}
}

func TestParseProfile(t *testing.T) {
	for _, p := range Profiles() {
		if got, err := ParseProfile(string(p)); err != nil || got != p {
			t.Errorf("ParseProfile(%q) = %q, %v", p, got, err)
		}
	}
	for _, bad := range []string{"", "Medium", "big"} {
		if _, err := ParseProfile(bad); err == nil {
			t.Errorf("ParseProfile(%q) = nil error", bad)
		}
	}
}

func TestLoad_ProfileOverride(t *testing.T) {
	global := t.TempDir()
	writeFile(t, filepath.Join(global, "profile-medium.md"), "MY MEDIUM\n")
	writeFile(t, filepath.Join(global, "profile-high.md"), "MY HIGH\n")
	cwd := t.TempDir()
	s, warnings, err := Load(global, cwd, testEnv(cwd))
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 2 || !strings.Contains(warnings[0], "profile-high") || !strings.Contains(warnings[1], "profile-medium") {
		t.Errorf("warnings = %v, want one per replaced layer", warnings)
	}
	if strings.Contains(s.Build(), "MY MEDIUM") {
		t.Error("low must not carry an overridden layer")
	}
	_ = s.SetProfile(High)
	got := s.Build()
	if !strings.Contains(got, "MY MEDIUM\n\nMY HIGH") || strings.Contains(got, "## How you work") {
		t.Errorf("override files not applied:\n%.300s", got)
	}
}

func TestSubagentsPlaceholder_EmptyNoGap(t *testing.T) {
	s, _ := loadProfile(t, t.TempDir())
	_ = s.SetProfile(Medium)
	if strings.Contains(s.Build(), "\n\n\n") {
		t.Error("the empty sub-agent placeholder left a blank gap")
	}
	found := false
	for _, b := range s.List() {
		if b.Name == "subagents" {
			found = true
			if b.Tokens != 0 {
				t.Errorf("placeholder has %d tokens, want 0 until part C fills it", b.Tokens)
			}
		}
	}
	if !found {
		t.Error("the subagents placeholder is missing from the medium profile")
	}
	if err := s.SetText("subagents", "DELEGATE"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s.Build(), "DELEGATE") {
		t.Error("SetText on the placeholder is not shown")
	}
	_ = s.SetProfile(Low)
	if strings.Contains(s.Build(), "DELEGATE") {
		t.Error("low must never carry sub-agent policy")
	}
	_ = s.SetProfile(High)
	if !strings.Contains(s.Build(), "DELEGATE") {
		t.Error("the text set on the placeholder was lost across profile switches")
	}
}

func TestLoad_LayerNamesAreReserved(t *testing.T) {
	for _, name := range []string{"profile-medium", "profile-high", "subagents"} {
		global := t.TempDir()
		writeFile(t, filepath.Join(global, "10-"+name+".md"), "x")
		cwd := t.TempDir()
		if _, _, err := Load(global, cwd, testEnv(cwd)); err == nil {
			t.Errorf("a global block named %q was accepted", name)
		}
	}
}

func TestLayers_NoInternalNamesOrToolJSON(t *testing.T) {
	jsonCall := regexp.MustCompile(`\{\s*"[a-z_]+"\s*:`)
	for name, text := range map[string]string{"medium": mediumText, "high": highText} {
		for _, internal := range []string{"profile-medium", "profile-high", "subagents", "tool-"} {
			if strings.Contains(text, internal) {
				t.Errorf("%s layer mentions the internal name %q", name, internal)
			}
		}
		if jsonCall.MatchString(text) {
			t.Errorf("%s layer has a JSON tool-call example (notes 10, 11: examples act as templates)", name)
		}
		if strings.Contains(text, "<!--") {
			t.Errorf("%s layer carries an HTML comment into the prompt", name)
		}
	}
}

func TestProfileTokens_Grows(t *testing.T) {
	s, _ := loadProfile(t, t.TempDir())
	c, st, f := s.ProfileTokens(Low), s.ProfileTokens(Medium), s.ProfileTokens(High)
	if c != chat.EstimateTokens(testBase) || !(c < st && st < f) {
		t.Errorf("sizes low %d, medium %d, high %d", c, st, f)
	}
}

func TestSetProfile_ConcurrentWithBuild(t *testing.T) {
	s, _ := loadProfile(t, t.TempDir())
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			_ = s.SetProfile(Profiles()[i%3])
		}
	}()
	for i := 0; i < 200; i++ {
		_ = s.Build()
		_ = s.List()
	}
	<-done
}
