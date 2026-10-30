package ui

import (
	"strings"
	"testing"
)

func TestParseCommand(t *testing.T) {
	tests := []struct {
		line    string
		want    Command
		errPart string
	}{
		{line: "/exit", want: Command{Kind: KindExit}},
		{line: "  /clear  ", want: Command{Kind: KindClear}},
		{line: "/blocks", want: Command{Kind: KindBlocks}},
		{line: "/block on x", want: Command{Kind: KindBlock, Name: "x", On: true}},
		{line: "/block   off   x ", want: Command{Kind: KindBlock, Name: "x"}},
		{line: "/auto", want: Command{Kind: KindAuto}},
		{line: "/tool 12", want: Command{Kind: KindTool, N: 12}},
		{line: " /bg ", want: Command{Kind: KindBg}},
		{line: "/bg x", errPart: "usage: /bg"},
		{line: "/nope", errPart: "unknown command /nope (see /help)"},
		{line: "/tool", errPart: "usage: /tool N"},
		{line: "/tool x", errPart: "usage: /tool N"},
		{line: "/tool -1", errPart: "usage: /tool N"},
		{line: "/block", errPart: "usage: /block"},
		{line: "/block on", errPart: "usage: /block"},
		{line: "/block maybe x", errPart: "usage: /block"},
		{line: "/think", want: Command{Kind: KindThink}},
		{line: "/server", want: Command{Kind: KindServer}},
		{line: "/server openrouter", want: Command{Kind: KindServer, Name: "openrouter"}},
		{line: "/server a b", errPart: "usage: /server"},
		{line: "/think on", want: Command{Kind: KindThink, Name: "on"}},
		{line: " /think  off ", want: Command{Kind: KindThink, Name: "off"}},
		{line: "/think maybe", errPart: "usage: /think"},
		{line: "/skill deploy ship it", want: Command{Kind: KindSkill, Name: "deploy", Text: "ship it"}},
		{line: "/skill deploy", want: Command{Kind: KindSkill, Name: "deploy"}},
		{line: "/skill", errPart: "usage: /skill"},
		{line: "/skills", want: Command{Kind: KindSkills}},
		{line: "/skills off deploy", want: Command{Kind: KindSkills, Name: "deploy"}},
		{line: "/skills off deploy project", want: Command{Kind: KindSkills, Name: "deploy", Scope: "project"}},
		{line: "/skills off deploy here", errPart: "usage: /skills"},
		{line: "/mcp approve demo", want: Command{Kind: KindMCP, Name: "demo", Text: "approve"}},
		{line: "/mcp off demo global", want: Command{Kind: KindMCP, Name: "demo", Text: "off", Scope: "global"}},
		{line: "/config", want: Command{Kind: KindConfig}},
		{line: "/config x", errPart: "usage: /config"},
		{line: "/skills deploy", errPart: "usage: /skills"},
		{line: "/mcp", want: Command{Kind: KindMCP}},
		{line: "/mcp demo", want: Command{Kind: KindMCP, Name: "demo", Text: "show"}},
		{line: "/mcp restart demo", want: Command{Kind: KindMCP, Name: "demo", Text: "restart"}},
		{line: "/mcp on demo", want: Command{Kind: KindMCP, Name: "demo", Text: "on", On: true}},
		{line: "/mcp on", errPart: "usage: /mcp"},
		{line: "/think on off", errPart: "usage: /think"},
		{line: "/unknown", errPart: "unknown command /unknown (see /help)"},
		{line: "/CLEAR", errPart: "unknown command /CLEAR"},
		{line: "/help", want: Command{Kind: KindHelp}},
		{line: "/help me", errPart: "usage: /help"},
		{line: "hello world ", want: Command{Kind: KindText, Text: "hello world"}},
		{line: "//x", want: Command{Kind: KindText, Text: "/x"}},
		{line: "help", want: Command{Kind: KindText, Text: "help"}}, // R-35: only / starts a command
		{line: "exit", want: Command{Kind: KindText, Text: "exit"}},
	}
	for _, tc := range tests {
		got, err := ParseCommand(tc.line)
		if tc.errPart != "" {
			if err == nil || !strings.Contains(err.Error(), tc.errPart) {
				t.Errorf("ParseCommand(%q) error = %v, want containing %q", tc.line, err, tc.errPart)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("ParseCommand(%q) = %+v, %v; want %+v", tc.line, got, err, tc.want)
		}
	}
}

// R-35: /help names every command once, with its arguments and description.
func TestHelp_ListsEveryCommandWithDescription(t *testing.T) {
	help := helpText()
	for _, c := range commands {
		line := ""
		for _, l := range strings.Split(help, "\n") {
			if strings.HasPrefix(l, c.name+" ") || l == c.name {
				line = l
			}
		}
		if line == "" || !strings.Contains(line, c.desc) || (c.args != "" && !strings.Contains(line, c.args)) {
			t.Errorf("/help lacks %s %s - %s:\n%s", c.name, c.args, c.desc, help)
		}
		if strings.Count(help, c.name+" ") != 1 {
			t.Errorf("%s listed %d times", c.name, strings.Count(help, c.name+" "))
		}
	}
	if strings.Count(help, "\n")+1 != len(commands) {
		t.Errorf("/help has %d lines for %d commands", strings.Count(help, "\n")+1, len(commands))
	}
}
