package prompt

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// BuildAgent: the SystemPrompt of a sub-agent (T13 C, R-123, I-40)
// ---------------------------------------------------------------------------

func loadWithTools(t *testing.T) (*Set, string) {
	t.Helper()
	cwd := t.TempDir()
	s, _, err := Load(t.TempDir(), cwd, testEnv(cwd),
		Block{Name: "tool-read_file", Source: SourceTool, Order: 500, Text: "READ-LINE"},
		Block{Name: "tool-shell", Source: SourceTool, Order: 501, Text: "SHELL-LINE"},
		Block{Name: "tool-task", Source: SourceTool, Order: 599, Text: "TASK-LINE"},
		Block{Name: "skills", Source: SourceTool, Order: 1500, Text: "SKILL-LIST"},
	)
	if err != nil {
		t.Fatal(err)
	}
	return s, cwd
}

func allowOnly(names ...string) func(string) bool {
	return func(n string) bool {
		for _, x := range names {
			if x == n {
				return true
			}
		}
		return false
	}
}

func TestBuildAgent_CompactIsBaseToolsBodyEnv(t *testing.T) {
	s, cwd := loadWithTools(t)
	got := s.BuildAgent(Compact, "agent-model", "BODY", allowOnly("read_file"))
	want := testBase + "\n\nREAD-LINE\n\nBODY\n\n" + strings.Replace(envBlock(cwd), "Model: qwen", "Model: agent-model", 1)
	if got != want {
		t.Errorf("BuildAgent =\n%s\nwant\n%s", got, want)
	}
}

func TestBuildAgent_OnlyAllowedToolBlocks(t *testing.T) {
	s, _ := loadWithTools(t)
	got := s.BuildAgent(Compact, "m", "", allowOnly("read_file", "shell"))
	for _, no := range []string{"TASK-LINE", "SKILL-LIST"} {
		if strings.Contains(got, no) {
			t.Errorf("agent prompt carries %q", no)
		}
	}
	if !strings.Contains(got, "READ-LINE") || !strings.Contains(got, "SHELL-LINE") {
		t.Errorf("allowed tool blocks missing:\n%s", got)
	}
	if strings.Contains(s.BuildAgent(Compact, "m", "", nil), "READ-LINE") {
		t.Error("a nil allow func must offer no tool block")
	}
}

func TestBuildAgent_ProfileLayersWithoutSubagents(t *testing.T) {
	s, _ := loadWithTools(t)
	if err := s.SetText("subagents", "DELEGATE-RULES"); err != nil {
		t.Fatal(err)
	}
	std := s.BuildAgent(Standard, "m", "BODY", nil)
	if !strings.Contains(std, strings.TrimSpace(standardText)) || strings.Contains(std, strings.TrimSpace(fullText)) {
		t.Error("standard agent must carry the standard layer only")
	}
	if strings.Contains(s.BuildAgent(Full, "m", "BODY", nil), "DELEGATE-RULES") {
		t.Error("an agent prompt must never carry the sub-agent policy")
	}
	if !strings.Contains(s.BuildAgent(Full, "m", "", nil), strings.TrimSpace(fullText)) {
		t.Error("full agent must carry the full layer")
	}
}

func TestBuildAgent_DoesNotChangeTheSession(t *testing.T) {
	s, _ := loadWithTools(t)
	_ = s.SetProfile(Compact)
	before := s.Build()
	_ = s.BuildAgent(Full, "other", "BODY", allowOnly("shell"))
	if s.Build() != before || s.Profile() != Compact {
		t.Error("BuildAgent changed the Session's prompt or profile")
	}
}

func TestBuildAgent_UnknownProfileIsCompact(t *testing.T) {
	s, _ := loadWithTools(t)
	if s.BuildAgent("nope", "m", "B", nil) != s.BuildAgent(Compact, "m", "B", nil) {
		t.Error("an unknown profile must build as compact")
	}
}

func TestBuildAgent_DisabledBlockStaysOut(t *testing.T) {
	cwd := t.TempDir()
	global := t.TempDir()
	writeFile(t, global+"/10-extra.md", "EXTRA")
	s, _ := mustLoad(t, global, cwd)
	_ = s.SetEnabled("extra", false)
	if strings.Contains(s.BuildAgent(Compact, "m", "B", nil), "EXTRA") {
		t.Error("a switched-off Block reached the agent prompt")
	}
}
