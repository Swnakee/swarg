package chat

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"a", 1},
		{"abcd", 1},
		{"abcde", 2},
		{"привет", 3}, // 12 bytes, Cyrillic counts by bytes
	}
	for _, tt := range tests {
		if got := EstimateTokens(tt.in); got != tt.want {
			t.Errorf("EstimateTokens(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestMessage_JSONRoundTripAllFields(t *testing.T) {
	in := Message{Role: RoleTool, Content: "full", ToolCallID: "c1", Name: "shell",
		Brief: "exit 0", Stub: "[exit 0]", Summary: true, Seq: 7, TurnStart: true,
		Injected: true, Label: "ls", Failed: true,
		ToolCalls: []ToolCall{{ID: "c1", Name: "shell", Args: `{"command":"ls"}`}}}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out Message
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Errorf("round trip changed the message:\n in  %+v\n out %+v", in, out)
	}
}

func TestEstimateTokens_Unchanged(t *testing.T) {
	for n := 0; n < 50; n++ {
		if got, want := EstimateTokens(strings.Repeat("x", n)), (n+3)/4; got != want {
			t.Errorf("EstimateTokens(%d bytes) = %d, want %d", n, got, want)
		}
	}
}
