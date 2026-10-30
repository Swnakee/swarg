package main

import (
	"strings"
	"testing"

	"swarg/internal/tools"
)

// TestPromptBlocks_TaskBlockOnOff: the task line is in the SystemPrompt blocks iff Tasks are on.
func TestPromptBlocks_TaskBlockOnOff(t *testing.T) {
	ts, err := tools.NewSet(tools.All(), nil)
	if err != nil {
		t.Fatal(err)
	}
	has := func(depth int) bool {
		for _, b := range promptBlocks(ts, depth, nil, nil) {
			if b.Name == "tool-task" && strings.HasPrefix(b.Text, "task: ") {
				return true
			}
		}
		return false
	}
	if !has(3) || has(0) {
		t.Fatalf("task block: depth 3 -> %v, depth 0 -> %v; want true, false", has(3), has(0))
	}
}
