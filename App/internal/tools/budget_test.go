package tools

import (
	"strings"
	"testing"

	"swarg/internal/chat"
	"swarg/internal/prompt"
)

// ---------------------------------------------------------------------------
// Fixed prompt cost
// ---------------------------------------------------------------------------

// fixedPromptCeiling is the budget of the text sent with every request: the system prompt
// (base + 8 tool blocks + env) plus the tool schemas (name, description, JSON schema).
const fixedPromptCeiling = 1230

// fixedPromptTokens measures the fixed prompt of the full built-in tool set.
func fixedPromptTokens(t *testing.T) (system, schemas int) {
	t.Helper()
	s, err := NewSet(All(), nil)
	if err != nil {
		t.Fatal(err)
	}
	env := prompt.Env{OS: "darwin", Shell: "sh", Dir: "/Users/someone/Projects/app", Date: "2026-10-27", Model: "mlx-community/Qwen3.5-4B-4bit"}
	set, _, err := prompt.Load(t.TempDir(), t.TempDir(), env, s.Blocks()...)
	if err != nil {
		t.Fatal(err)
	}
	system = chat.EstimateTokens(set.Build())
	var b strings.Builder
	for _, sp := range s.Specs() {
		b.WriteString(sp.Name + sp.Description + string(sp.Schema))
	}
	return system, chat.EstimateTokens(b.String())
}

func TestBlocks_TokenCeiling(t *testing.T) {
	system, schemas := fixedPromptTokens(t)
	t.Logf("system prompt %d + schemas %d = %d tokens (ceiling %d)", system, schemas, system+schemas, fixedPromptCeiling)
	if total := system + schemas; total > fixedPromptCeiling {
		t.Errorf("fixed prompt is %d tokens (system %d + schemas %d), ceiling %d", total, system, schemas, fixedPromptCeiling)
	}
}

func TestBlocks_StillExplainBackground(t *testing.T) {
	s, _ := NewSet(All(), nil)
	sh, _ := s.Get("shell")
	if !strings.Contains(sh.Block(), "background") || !strings.Contains(sh.Spec().Description+string(sh.Spec().Schema), "background") {
		t.Errorf("shell must still mention background in block and schema")
	}
	so, _ := s.Get("shell_output")
	if !strings.Contains(so.Block(), "id") || !strings.Contains(string(so.Spec().Schema), "default") {
		t.Errorf("shell_output must explain the optional id: %q", so.Block())
	}
}

func TestBlocks_KeepExamples(t *testing.T) {
	s, _ := NewSet(All(), nil)
	for _, b := range s.Blocks() {
		if !strings.Contains(b.Text, "Example:") {
			t.Errorf("block %s lost its Example line", b.Name)
		}
	}
}
