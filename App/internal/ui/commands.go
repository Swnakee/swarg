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
	KindTask
	KindThink
)

// Command is a parsed input line. Text is set for KindText, Name and On for KindBlock, Name
// for KindRename, Text (the goal) for KindTask, N for KindTool, Name (optional) for KindModel.
type Command struct {
	Kind CommandKind
	Text string
	Name string
	On   bool
	N    int
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
	{name: "/think", args: "[on|off]", desc: "thinking mode of the model (no argument shows it)", kind: KindThink, parse: parseThink},
	{name: "/exit", desc: "quit swarg", kind: KindExit},
}

var (
	errBlockUsage  = errors.New("usage: /block on <name> | /block off <name>")
	errRenameUsage = errors.New("usage: /rename <name>")
	errTaskUsage   = errors.New("usage: /task <goal>")
	errToolUsage   = errors.New("usage: /tool N (N is the number after # on a tool line)")
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
