package config

import "testing"

import "strings"

func TestKeyUsers_ProvidersAndMCP(t *testing.T) {
	cfg := Defaults()
	cfg.MCP.Servers = map[string]MCPServer{
		"web":   {URL: "https://example.com/mcp", Headers: map[string]string{"Authorization": "Bearer ${T12_MCP_TOK}"}},
		"files": {Command: "x", Env: map[string]string{"TOKEN": "$T12_MCP_TOK", "N": "$1"}},
	}
	u := KeyUsers(cfg)
	if got := strings.Join(u["OPENROUTER_API_KEY"], ","); got != "provider openrouter" {
		t.Errorf("provider users %q", got)
	}
	if got := strings.Join(u["T12_MCP_TOK"], ","); got != "mcp files env TOKEN,mcp web header Authorization" {
		t.Errorf("mcp users %q", got)
	}
	if _, ok := u["1"]; ok {
		t.Error("$1 is not a key")
	}
}
