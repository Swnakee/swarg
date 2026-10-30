package main

import (
	"fmt"
	"sort"
	"strings"

	"swarg/internal/chat"
	"swarg/internal/mcp"
	"swarg/internal/skills"
)

// ---------------------------------------------------------------------------
// Extra prompt budget: Skills and MCP (R-89)
// ---------------------------------------------------------------------------

// extraPromptCeiling is the budget of what Skills and MCP add to every request (the Skill list,
// the skill Tool, the MCP tool specs and the mcp block); the built-ins have their own ceiling.
const extraPromptCeiling = 1500

type sized struct {
	name   string
	tokens int
}

// extraPromptWarning measures the Skill and MCP additions and names the biggest items when they
// exceed the ceiling; "" when they fit. Nothing is dropped: the owner trims by config.
func extraPromptWarning(sk *skills.Set, mc *mcp.Client) string {
	var items []sized
	if sk != nil && sk.Len() > 0 {
		t := sk.Tool()
		items = append(items, sized{fmt.Sprintf("skills list (%d)", len(sk.List())), chat.EstimateTokens(sk.Block())},
			sized{"skill tool", chat.EstimateTokens(specText(t.Spec()) + t.Block())})
	}
	if mc != nil {
		per := map[string]int{}
		for _, sp := range mc.Specs() {
			server, _, _ := strings.Cut(strings.TrimPrefix(sp.Name, mcp.Prefix), "__")
			per[server] += chat.EstimateTokens(specText(sp))
		}
		for name, n := range per {
			items = append(items, sized{"mcp " + name, n})
		}
		if ins := mc.Instructions(); ins != "" {
			items = append(items, sized{"mcp instructions", chat.EstimateTokens(ins)})
		}
	}
	total := 0
	for _, it := range items {
		total += it.tokens
	}
	if total <= extraPromptCeiling {
		return ""
	}
	sort.Slice(items, func(i, j int) bool { return items[i].tokens > items[j].tokens })
	var parts []string
	for _, it := range items[:min(3, len(items))] {
		parts = append(parts, fmt.Sprintf("%s %d", it.name, it.tokens))
	}
	return fmt.Sprintf("prompt: skills and MCP add %d tokens to every request (limit %d): %s; trim with mcp.servers.<name>.tools or skills.disabled",
		total, extraPromptCeiling, strings.Join(parts, ", "))
}

func specText(sp chat.ToolSpec) string { return sp.Name + sp.Description + string(sp.Schema) }
