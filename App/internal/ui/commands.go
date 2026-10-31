package ui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"
)

// ---------------------------------------------------------------------------
// Slash commands: one table drives parsing, /help and the suggestions
// ---------------------------------------------------------------------------

// CommandKind tells what a parsed input line asks for.
type CommandKind int

// Command kinds. KindText is a normal message (not a command).
const (
	KindText CommandKind = iota
	KindExit
	KindClear
	KindBlocks
	KindBlock
	KindAuto
	KindTool
	KindBg
	KindCompact
	KindSessions
	KindContext
	KindHelp
	KindRename
	KindModel
	KindServer // /server [name]: the model source (R-100)
	KindTask
	KindThink
	KindSkill  // /skill <name> [text]
	KindSkills // /skills [on|off <name>]
	KindMCP    // /mcp ...
	KindConfig // /config
	KindKeys   // /keys [init] (R-106, R-110)
	KindCost   // /cost (R-113)
	KindAgents // /agents [on|off <name> | new <name> | edit <name>] [global|project]
	KindPrompt // /prompt [profile [global|project]] (R-117)
)

// Command is a parsed input line. Text is set for KindText, Name and On for KindBlock, Name
// for KindRename, Text (the goal) for KindTask, N for KindTool, Name (optional) for KindModel.
type Command struct {
	Kind  CommandKind
	Text  string
	Name  string
	On    bool
	N     int
	Scope string // "global" or "project" for /skills and /mcp switches; "" = the default scope
}

// cmdSpec describes one slash command: how it is typed, what /help and the suggestions say
// about it, and how its arguments parse. parse nil means the command takes no arguments;
// otherwise it gets the fields of the line (f[0] is the command).
type cmdSpec struct {
	name, args, desc string
	kind             CommandKind
	parse            func(f []string) (Command, error)
}

// commands is the one list of slash commands, in display order (/help, suggestions). Adding
// a command is one entry here plus its case in model.submit.
var commands = []cmdSpec{
	{name: "/help", desc: "list the commands", kind: KindHelp},
	{name: "/clear", desc: "empty the history", kind: KindClear},
	{name: "/compact", desc: "summarize the work done so far", kind: KindCompact},
	{name: "/task", args: "<goal>", desc: "run the goal as a Task (own context, returns a report)", kind: KindTask, parse: parseTask},
	{name: "/sessions", desc: "open a saved session (picker)", kind: KindSessions},
	{name: "/resume", desc: "the same as /sessions", kind: KindSessions},
	{name: "/rename", args: "<name>", desc: "name this session", kind: KindRename, parse: parseRename},
	{name: "/context", desc: "what fills the context", kind: KindContext},
	{name: "/tool", args: "N", desc: "open tool call #N", kind: KindTool, parse: parseTool},
	{name: "/bg", desc: "list the background jobs", kind: KindBg},
	{name: "/auto", desc: "toggle auto-approve of tool calls", kind: KindAuto},
	{name: "/blocks", desc: "list the system prompt blocks", kind: KindBlocks},
	{name: "/block", args: "on|off <name>", desc: "switch a prompt block", kind: KindBlock, parse: parseBlock},
	{name: "/model", args: "[id|N]", desc: "choose the model (list from the server)", kind: KindModel, parse: parseModel},
	{name: "/server", args: "[name]", desc: "choose the model source (server); the list shows their state", kind: KindServer, parse: parseServer},
	{name: "/think", args: "[on|off]", desc: "thinking mode of the model (no argument shows it)", kind: KindThink, parse: parseThink},
	{name: "/skill", args: "<name> [text]", desc: "apply a skill, then answer the text", kind: KindSkill, parse: parseSkill},
	{name: "/skills", args: "[on|off <name> [global|project]]", desc: "skills menu, or switch one", kind: KindSkills, parse: parseSkills},
	{name: "/mcp", args: "[<server> | on|off|restart|approve <server> [global|project]]", desc: "MCP servers menu: state, tools, switch, restart, approve", kind: KindMCP, parse: parseMCP},
	{name: "/agents", args: "[on|off <name> | new <name> | edit <name>] [global|project]", desc: "sub-agent types (global by default): menu, switch one, create or edit a type file", kind: KindAgents, parse: parseAgents},
	{name: "/prompt", args: "[compact|standard|full [global|project]]", desc: "prompt profile: how much instruction text the model gets", kind: KindPrompt, parse: parsePrompt},
	{name: "/config", desc: "settings menu (saved to the chosen scope)", kind: KindConfig},
	{name: "/keys", args: "[init]", desc: "API keys menu (view only); init creates the keys file template", kind: KindKeys, parse: parseKeys},
	{name: "/cost", desc: "what this session has cost, and the key's usage and limit", kind: KindCost},
	{name: "/exit", desc: "quit swarg", kind: KindExit},
}

var (
	errBlockUsage  = errors.New("usage: /block on <name> | /block off <name>")
	errRenameUsage = errors.New("usage: /rename <name>")
	errTaskUsage   = errors.New("usage: /task <goal>")
	errToolUsage   = errors.New("usage: /tool N (N is the number after # on a tool line)")
	errSkillUsage  = errors.New("usage: /skill <name> [text]")
	errAgentsUsage = errors.New("usage: /agents | /agents on|off <name> [global|project] | /agents new <name> [global|project] | /agents edit <name>")
	errSkillsUsage = errors.New("usage: /skills | /skills on|off <name> [global|project]")
	// errKeysUsage never echoes the line: a mistyped "name" or an extra word may be the key itself.
	errKeysUsage = errors.New("usage: /keys | /keys init (keys are written in the keys file, never typed here)")
	errMCPUsage  = errors.New("usage: /mcp | /mcp <server> | /mcp on|off|restart|approve <server> [global|project]")
)

// ParseCommand parses one input line. Commands are case-sensitive and the line is
// trimmed. "//x" is the message "/x". Unknown "/x" is an error pointing to /help.
func ParseCommand(line string) (Command, error) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "/") {
		return Command{Kind: KindText, Text: line}, nil
	}
	if strings.HasPrefix(line, "//") {
		return Command{Kind: KindText, Text: line[1:]}, nil
	}
	f := strings.Fields(line)
	spec := lookup(f[0])
	if spec == nil {
		return Command{}, fmt.Errorf("unknown command %s (see /help)", f[0])
	}
	if spec.parse == nil {
		if len(f) != 1 {
			return Command{}, fmt.Errorf("usage: %s", spec.name)
		}
		return Command{Kind: spec.kind}, nil
	}
	return spec.parse(f)
}

// lookup finds the command named name, nil when there is none.
func lookup(name string) *cmdSpec {
	for i := range commands {
		if commands[i].name == name {
			return &commands[i]
		}
	}
	return nil
}

// helpText is /help: every command with its arguments and description, in table order.
func helpText() string {
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for _, c := range commands {
		fmt.Fprintf(w, "%s\t%s\n", strings.TrimSpace(c.name+" "+c.args), c.desc)
	}
	w.Flush()
	return strings.TrimRight(b.String(), "\n")
}

// ---------------------------------------------------------------------------
// Argument parsers
// ---------------------------------------------------------------------------

// parseRename is /rename <name>: the name is the rest of the line, blanks squeezed.
func parseRename(f []string) (Command, error) {
	name := strings.Join(f[1:], " ")
	if name == "" {
		return Command{}, errRenameUsage
	}
	return Command{Kind: KindRename, Name: name}, nil
}

// parseTask is /task <goal>: the goal is the rest of the line, blanks squeezed.
func parseTask(f []string) (Command, error) {
	goal := strings.Join(f[1:], " ")
	if goal == "" {
		return Command{}, errTaskUsage
	}
	return Command{Kind: KindTask, Text: goal}, nil
}

// parseModel is /model [id|N]: no argument opens the list, one argument chooses.
func parseModel(f []string) (Command, error) {
	if len(f) > 2 {
		return Command{}, errors.New("usage: /model or /model <id>")
	}
	c := Command{Kind: KindModel}
	if len(f) == 2 {
		c.Name = f[1]
	}
	return c, nil
}

// parseServer is /server [name]: no argument opens the list, one argument switches.
func parseServer(f []string) (Command, error) {
	if len(f) > 2 {
		return Command{}, errors.New("usage: /server or /server <name>")
	}
	c := Command{Kind: KindServer}
	if len(f) == 2 {
		c.Name = f[1]
	}
	return c, nil
}

// parseThink is /think [on|off]: no argument shows the state, one argument sets it (Name).
func parseThink(f []string) (Command, error) {
	if len(f) > 2 || (len(f) == 2 && f[1] != "on" && f[1] != "off") {
		return Command{}, errors.New("usage: /think or /think on | /think off")
	}
	c := Command{Kind: KindThink}
	if len(f) == 2 {
		c.Name = f[1]
	}
	return c, nil
}

func parseTool(f []string) (Command, error) {
	n, err := strconv.Atoi(strings.Join(f[1:], " "))
	if len(f) != 2 || err != nil || n < 1 {
		return Command{}, errToolUsage
	}
	return Command{Kind: KindTool, N: n}, nil
}

func parseBlock(f []string) (Command, error) {
	if len(f) != 3 || (f[1] != "on" && f[1] != "off") {
		return Command{}, errBlockUsage
	}
	return Command{Kind: KindBlock, Name: f[2], On: f[1] == "on"}, nil
}

// parseSkill is /skill <name> [text]: Name is the Skill, Text what the model answers afterwards.
func parseSkill(f []string) (Command, error) {
	if len(f) < 2 {
		return Command{}, errSkillUsage
	}
	return Command{Kind: KindSkill, Name: f[1], Text: strings.Join(f[2:], " ")}, nil
}

// parseSkills is /skills (the menu) or /skills on|off <name> [global|project].
func parseSkills(f []string) (Command, error) {
	scope, ok := parseScope(f, 3)
	switch {
	case len(f) == 1:
		return Command{Kind: KindSkills}, nil
	case ok && (f[1] == "on" || f[1] == "off"):
		return Command{Kind: KindSkills, Name: f[2], On: f[1] == "on", Scope: scope}, nil
	}
	return Command{}, errSkillsUsage
}

// parseAgents is /agents (the menu), /agents on|off <name> [global|project] or
// /agents new <name> [global|project] or /agents edit <name>; "new" and "edit" travel in Text.
func parseAgents(f []string) (Command, error) {
	scope, ok := parseScope(f, 3)
	switch {
	case len(f) == 1:
		return Command{Kind: KindAgents}, nil
	case ok && (f[1] == "on" || f[1] == "off"):
		return Command{Kind: KindAgents, Name: f[2], On: f[1] == "on", Scope: scope}, nil
	case ok && (f[1] == "new" || f[1] == "edit"):
		return Command{Kind: KindAgents, Name: f[2], Text: f[1], Scope: scope}, nil
	}
	return Command{}, errAgentsUsage
}

// parseMCP is /mcp (the menu), /mcp <server> (its tools) or /mcp on|off|restart|approve <server>
// [global|project]; the action travels in Text.
func parseMCP(f []string) (Command, error) {
	scope, ok := parseScope(f, 3)
	switch {
	case len(f) == 1:
		return Command{Kind: KindMCP}, nil
	case len(f) == 2 && !mcpAction(f[1]):
		return Command{Kind: KindMCP, Name: f[1], Text: "show"}, nil
	case ok && mcpAction(f[1]):
		return Command{Kind: KindMCP, Name: f[2], Text: f[1], On: f[1] == "on", Scope: scope}, nil
	}
	return Command{}, errMCPUsage
}

// parseKeys is /keys (the menu) or /keys init (the template); the action travels in Text. Anything
// else is refused without echoing the line (R-108).
func parseKeys(f []string) (Command, error) {
	switch {
	case len(f) == 1:
		return Command{Kind: KindKeys}, nil
	case len(f) == 2 && f[1] == "init":
		return Command{Kind: KindKeys, Text: "init"}, nil
	}
	return Command{}, errKeysUsage
}

func mcpAction(s string) bool { return s == "on" || s == "off" || s == "restart" || s == "approve" }

// parseScope reads an optional trailing "global" | "project" after n fields: ok when the line has
// exactly n fields, or n+1 with a valid scope.
func parseScope(f []string, n int) (scope string, ok bool) {
	switch {
	case len(f) == n:
		return "", true
	case len(f) == n+1 && (f[n] == "global" || f[n] == "project"):
		return f[n], true
	}
	return "", false
}
