package tools

import (
	"testing"

	"swarg/internal/chat"
	"swarg/internal/prompt"
)

// ---------------------------------------------------------------------------
// Prompt Profile budgets (R-115): the whole fixed prompt (system prompt + tool schemas)
// ---------------------------------------------------------------------------

var profileCeilings = map[prompt.Profile]int{
	prompt.Compact:  1200, // the same ceiling as TestBlocks_TokenCeiling
	prompt.Standard: 5000,
	prompt.Full:     12000,
}

func TestProfiles_TokenBudget(t *testing.T) {
	s, err := NewSet(All(), nil)
	if err != nil {
		t.Fatal(err)
	}
	env := prompt.Env{OS: "darwin", Shell: "sh", Dir: "/Users/someone/Projects/app", Date: "2026-10-27", Model: "mlx-community/Qwen3.5-4B-4bit"}
	set, _, err := prompt.Load(t.TempDir(), t.TempDir(), env, s.Blocks()...)
	if err != nil {
		t.Fatal(err)
	}
	_, schemas := fixedPromptTokens(t)
	prev := 0
	for _, p := range prompt.Profiles() {
		if err := set.SetProfile(p); err != nil {
			t.Fatal(err)
		}
		total := chat.EstimateTokens(set.Build()) + schemas
		t.Logf("%s: %d tokens (ceiling %d)", p, total, profileCeilings[p])
		if total > profileCeilings[p] {
			t.Errorf("%s fixed prompt is %d tokens, ceiling %d", p, total, profileCeilings[p])
		}
		if total <= prev {
			t.Errorf("%s (%d) is not larger than the previous profile (%d)", p, total, prev)
		}
		prev = total
	}
}
