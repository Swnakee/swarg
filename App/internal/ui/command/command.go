// Package command parses the slash commands: one table drives parsing, /help and the suggestions.
package command

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
)

// ---------------------------------------------------------------------------
// Slash commands: one table drives parsing, /help and the suggestions
// ---------------------------------------------------------------------------

// Kind tells what a parsed input line asks for.
type Kind int

// Kinds. KindText is a normal message (not a command).
const (
	KindText Kind = iota
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
	Kind  Kind
	Text  string
	Name  string
	On    bool
	N     int
	Scope string // "global" or "project" for /skills and /mcp switches; "" = the default scope
}

// Spec describes one slash command: how it is typed, what /help and the suggestions say about
// it, and how its arguments parse. parse nil means the command takes no arguments; otherwise
// it gets the fields of the line (f[0] is the command).
type Spec struct {
	Name, Args, Desc string
	Kind             Kind
	parse            func(f []string) (Command, error)
}

// commands is the one list of slash commands, in display order (/help, suggestions). Adding
// a command is one entry here plus its case in model.submit.
var commands = []Spec{
	{Name: "/help", Desc: "list the commands", Kind: KindHelp},
	{Name: "/clear", Desc: "empty the history", Kind: KindClear},
	{Name: "/compact", Desc: "summarize the work done so far", Kind: KindCompact},
	{Name: "/task", Args: "<goal>", Desc: "run the goal as a Task (own context, returns a report)", Kind: KindTask, parse: parseTask},
	{Name: "/sessions", Desc: "open a saved session (picker)", Kind: KindSessions},
	{Name: "/resume", Desc: "the same as /sessions", Kind: KindSessions},
	{Name: "/rename", Args: "<name>", Desc: "name this session", Kind: KindRename, parse: parseRename},
	{Name: "/context", Desc: "what fills the context", Kind: KindContext},
	{Name: "/tool", Args: "N", Desc: "open tool call #N", Kind: KindTool, parse: parseTool},
	{Name: "/bg", Desc: "list the background jobs", Kind: KindBg},
	{Name: "/auto", Desc: "toggle auto-approve of tool calls", Kind: KindAuto},
	{Name: "/blocks", Desc: "list the system prompt blocks", Kind: KindBlocks},
	{Name: "/block", Args: "on|off <name>", Desc: "switch a prompt block", Kind: KindBlock, parse: parseBlock},
	{Name: "/model", Args: "[id|N]", Desc: "choose the model (list from the server)", Kind: KindModel, parse: optionalArg(KindModel, "usage: /model or /model <id>")},
	{Name: "/server", Args: "[name]", Desc: "choose the model source (server); the list shows their state", Kind: KindServer, parse: optionalArg(KindServer, "usage: /server or /server <name>")},
	{Name: "/think", Args: "[on|off]", Desc: "thinking mode of the model (no argument shows it)", Kind: KindThink, parse: optionalArg(KindThink, "usage: /think or /think on | /think off", "on", "off")},
	{Name: "/skill", Args: "<name> [text]", Desc: "apply a skill, then answer the text", Kind: KindSkill, parse: parseSkill},
	{Name: "/skills", Args: "[on|off <name> [global|project]]", Desc: "skills menu, or switch one", Kind: KindSkills, parse: parseSkills},
	{Name: "/mcp", Args: "[<server> | on|off|restart|approve <server> [global|project]]", Desc: "MCP servers menu: state, tools, switch, restart, approve", Kind: KindMCP, parse: parseMCP},
	{Name: "/agents", Args: "[on|off <name> | new <name> | edit <name>] [global|project]", Desc: "sub-agent types (global by default): menu, switch one, create or edit a type file", Kind: KindAgents, parse: parseAgents},
	{Name: "/prompt", Args: "[low|medium|high [global|project]]", Desc: "prompt profile: how much instruction text the model gets", Kind: KindPrompt, parse: parsePrompt},
	{Name: "/config", Desc: "settings menu (saved to the chosen scope)", Kind: KindConfig},
	{Name: "/keys", Args: "[init]", Desc: "API keys menu (view only); init creates the keys file template", Kind: KindKeys, parse: parseKeys},
	{Name: "/cost", Desc: "what this session has cost, and the key's usage and limit", Kind: KindCost},
	{Name: "/exit", Desc: "quit swarg", Kind: KindExit},
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
	errKeysUsage   = errors.New("usage: /keys | /keys init (keys are written in the keys file, never typed here)")
	errPromptUsage = errors.New("usage: /prompt | /prompt <low|medium|high> [global|project]")
	errMCPUsage    = errors.New("usage: /mcp | /mcp <server> | /mcp on|off|restart|approve <server> [global|project]")
)

// Parse parses one input line. Commands are case-sensitive and the line is
// trimmed. "//x" is the message "/x". Unknown "/x" is an error pointing to /help.
func Parse(line string) (Command, error) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "/") {
		return Command{Kind: KindText, Text: line}, nil
	}
	if strings.HasPrefix(line, "//") {
		return Command{Kind: KindText, Text: line[1:]}, nil
	}
	f := strings.Fields(line)
	spec := Lookup(f[0])
	if spec == nil {
		return Command{}, fmt.Errorf("unknown command %s (see /help)", f[0])
	}
	if spec.parse == nil {
		if len(f) != 1 {
			return Command{}, fmt.Errorf("usage: %s", spec.Name)
		}
		return Command{Kind: spec.Kind}, nil
	}
	return spec.parse(f)
}

// All is the table of commands in display order; callers must not change it.
func All() []Spec { return commands }

// Lookup finds the command named name, nil when there is none.
func Lookup(name string) *Spec {
	for i := range commands {
		if commands[i].Name == name {
			return &commands[i]
		}
	}
	return nil
}

// Help is /help: every command with its arguments and description, in table order.
func Help() string {
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for _, c := range commands {
		fmt.Fprintf(w, "%s\t%s\n", strings.TrimSpace(c.Name+" "+c.Args), c.Desc)
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

// optionalArg parses "/cmd [arg]": no argument leaves Name empty, one argument is Name; more
// arguments, or a value outside allowed (when given), fail with usage.
func optionalArg(kind Kind, usage string, allowed ...string) func([]string) (Command, error) {
	return func(f []string) (Command, error) {
		if len(f) > 2 || (len(f) == 2 && len(allowed) > 0 && !slices.Contains(allowed, f[1])) {
			return Command{}, errors.New(usage)
		}
		c := Command{Kind: kind}
		if len(f) == 2 {
			c.Name = f[1]
		}
		return c, nil
	}
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

// parsePrompt is /prompt (the menu) or /prompt <profile> [global|project]; the scope saves the
// profile as the config default, no scope applies it to this Session only.
func parsePrompt(f []string) (Command, error) {
	if len(f) == 1 {
		return Command{Kind: KindPrompt}, nil
	}
	if scope, ok := parseScope(f, 2); ok {
		return Command{Kind: KindPrompt, Name: f[1], Scope: scope}, nil
	}
	return Command{}, errPromptUsage
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
