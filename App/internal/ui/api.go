// Package ui is the terminal front end of swarg: a Bubble Tea UI for terminals
// and a plain stdin/stdout mode for pipes. It drives the agent only through the
// small interfaces below.
package ui

import (
	"context"

	"swarg/internal/agent"
	"swarg/internal/chat"
	"swarg/internal/prompt"
)

// ---------------------------------------------------------------------------
// Contracts with the core
// ---------------------------------------------------------------------------

// AgentAPI is the part of *agent.Agent the UI uses.
type AgentAPI interface {
	Start(ctx context.Context)
	Submit(text string)
	Interrupt()
	Abort()
	Clear() error
	Busy() bool
	Wait()
	Background() bool // move the running detachable tool call to the background
}

// BlockAPI is the part of *prompt.Set the UI uses.
type BlockAPI interface {
	SetEnabled(name string, on bool) error
	List() []prompt.BlockInfo
}

// JobRow is one background job as the UI shows it.
type JobRow struct {
	ID, Command string
	Running     bool
	ExitCode    int
}

// JobsAPI is the part of the background job manager the UI uses (main adapts tools.Jobs).
type JobsAPI interface {
	List() []JobRow
}

// SessionRow is one saved session as the in-app picker lists it (the picker's own row type:
// Title is the Name when the session has one, else its first user message; Err stays empty).
type SessionRow = PickRow

// Opened is a saved session the app switched to: its history for the agent (Load with
// WithMaxSeq), the model it was run with and its title for the notice.
type Opened struct {
	History []chat.Message
	MaxSeq  int
	Model   string
	Think   bool               // the thinking mode at the end of the session (R-60)
	Spend   map[string]float64 // the cost so far per model (R-111)
	Tokens  agent.Tokens       // the token usage so far (R-131)
	Title   string
}

// SessionsAPI is the saved sessions of the project as the UI uses them (main adapts the
// session package and the files being written). Open SWITCHES the SessionFile being written
// to the chosen one (true continuation) and returns its history; Delete removes a saved
// session (never the current one); Rename names the current one (/rename); Current is the
// id of the session being written, "" when none. Deps.Sessions may be nil: the session
// commands then say that saving is off.
type SessionsAPI interface {
	List() ([]SessionRow, error)
	Open(id string) (Opened, error)
	Delete(id string) error
	Rename(name string) error
	Current() string
}

// ModelRow is one model of the active Source as /model lists it (R-101, R-102): Name and Price
// are empty on a plain server; Price is "free" or the price text; Recent marks a model used before
// on this Source (listed first).
type ModelRow struct {
	ID, Name, Price string
	Recent          bool
}

// ModelsAPI lists the models of the active Source (main adapts the Catalog). Deps.Models may be
// nil: /model then can only set an id given on the command line.
type ModelsAPI interface {
	List(ctx context.Context) ([]ModelRow, error)
}

// ServerRow is one configured Source as /server lists it: State is "ok", "down: ...",
// "no key: set X" or "bad config: ..."; Active marks the one in use.
type ServerRow struct {
	Name, Type, Address, State string
	Active                     bool
}

// ServersAPI is what /server needs (R-100); main adapts the config and the clients. Switch makes
// name the Source of the agent and returns the one-time cloud notice when it applies (R-104).
// Deps.Servers may be nil: the command then says so.
type ServersAPI interface {
	Servers(ctx context.Context) ([]ServerRow, error)
	Switch(name string) (notice string, err error)
}

// Optional parts of the agent, used through a type assertion so a core without them (or the
// test fakes) keeps compiling: *agent.Agent gains them in later waves.
type (
	compacter interface {
		Compact(ctx context.Context) error
	} // /compact
	loader interface {
		Load(history []chat.Message, opts ...agent.LoadOption) error
	} // opening a saved session (/resume, the picker)
	modelSetter interface {
		SetModel(name string) error
	} // /model, the Bottom panel's model item (the test fakes lack it)
	thinker interface {
		SetThinking(on bool)
	} // /think
	taskSubmitter interface {
		SubmitTask(goal string) error
	} // /task
	skillSubmitter interface {
		SubmitSkill(name, text string) error
	} // /skill
)

// SkillsAPI is what /skills and /skill need; main adapts the skills package (ui imports no tools).
// SetEnabled applies for this run and persists `skills.disabled` into the scope's config file
// ("" = this run only).
type SkillsAPI interface {
	List() []SkillRow
	SetEnabled(name string, on bool, scope string) error
}

// SkillRow is one Skill as the UI shows it.
type SkillRow struct {
	Name, Description, Source string
	Enabled                   bool
}

// AgentsAPI is what /agents needs; main adapts the agents package. SetEnabled applies for this run and persists `agents.disabled` into the scope's config file ("" = this run only). New writes a template agent-type file for the scope and returns its path. The default scope is global (R-128).
type AgentsAPI interface {
	List() []AgentRow
	SetEnabled(name string, on bool, scope string) error
	New(name, scope string) (path string, err error)
	// Edit returns the file to edit for a type: its own file, or, for a built-in, a copy of its
	// definition written to the global agents folder (created true; an existing file is kept).
	Edit(name string) (path string, created bool, err error)
}

// AgentRow is one sub-agent type as the UI shows it. Source: builtin | global | project.
type AgentRow struct {
	Name, Description, Source, Path string
	Enabled                         bool
}

// MCPAPI is what /mcp needs; main adapts the mcp client. SetEnabled applies for this run and
// persists `mcp.servers.<name>.disabled` into the scope's file ("" = this run only); Approve
// stores the user's approval of a project server and starts it (R-97).
type MCPAPI interface {
	Servers() []MCPServerRow
	Tools(server string) (tools []MCPToolRow, stderr string, err error)
	SetEnabled(server string, on bool, scope string) error
	Restart(server string) error
	Approve(server string) error
}

// MCPServerRow is one MCP Server as /mcp lists it.
type MCPServerRow struct {
	Name, State, Reason string
	Tools               int
	Enabled             bool
	Source, Transport   string // "global" / "project", "stdio" / "http"
}

// NotSavedError says a switch applied for this run but could not be written to its scope's file
// (SkillsAPI.SetEnabled, MCPAPI.SetEnabled): the UI shows the new state with the reason.
type NotSavedError struct{ Err error }

func (e *NotSavedError) Error() string { return "not saved: " + e.Err.Error() }
func (e *NotSavedError) Unwrap() error { return e.Err }

// ScopesAPI tells where settings can be saved: Scopes lists them ("global", and "project" inside a
// project), Default is the one a command without a scope uses (R-98). Deps.Scopes may be nil: then
// switches apply to this run only.
type ScopesAPI interface {
	Scopes() []string
	Default() string
}

// Setting is one row of /config: Kind is "bool", "int" or "string"; Min/Max/Step bound an int;
// Live names what the app applies at once ("auto", "think", "model"), "" = at the next start.
type Setting struct {
	Key, Label, Kind string
	Value            any
	Min, Max, Step   int
	Scope            string // where the effective value comes from ("global" / "project")
	Live             string
}

// ConfigAPI is what /config needs; main adapts the loaded config and config.Set.
type ConfigAPI interface {
	Settings() []Setting
	Set(scope, key string, value any) error
}

// KeyRow is one API key as /keys lists it: Users say who refers to it ("provider openrouter", "mcp
// web header Authorization"), State is "set (env)", "set (env, wins)", "set (keys.env)",
// "set (old keys dir)" or "missing", Masked is the value hint or "".
type KeyRow struct {
	Name, State, Masked string
	Users               []string
}

// KeysAPI is what /keys needs (R-106, R-110); main adapts the key store. It only shows: keys are
// written in the keys file, never in the UI. Keys re-reads the file on every call. File is the
// path shown to the user, Warnings the current problems of the file (never a key), Init writes the
// template when there is no file (error otherwise) and returns the path. Deps.Keys may be nil: the
// command then says so.
type KeysAPI interface {
	Keys() []KeyRow
	File() string
	Warnings() []string
	Init() (string, error)
}

// PromptRow is one prompt profile as /prompt lists it: Tokens is the estimated size of its
// instruction text, Active marks the one in force.
type PromptRow struct {
	Name, Note string
	Tokens     int
	Active     bool
}

// PromptAPI is what /prompt needs (R-117); main adapts the prompt Set, the config and the
// SessionFile. Choose applies the profile from the next request and records it in the SessionFile
// (an explicit choice: the config stops deciding for this Session); a scope ("global" or "project")
// also writes `prompt.profile` there, and a failed write is a NotSavedError after the profile was
// applied. Deps.Prompt may be nil: the command then says so.
type PromptAPI interface {
	Profiles() []PromptRow
	Choose(name, scope string) error
}

// MCPToolRow is one tool of /mcp <server>.
type MCPToolRow struct {
	Name, Description string
	ReadOnly, Offered bool
}

// Deps is everything a UI needs. Events carries agent events (fed by the
// agent's OnEvent, see NewInbox); the UI starts the agent only after it is ready to
// read them. Approver must be the one whose Approve is agent.Deps.Approve (nil gets
// a private one, so confirmations are then answered by Yes). AutoApprove starts
// always-yes on. Jobs may be nil (no background jobs).
type Deps struct {
	Agent       AgentAPI
	Blocks      BlockAPI
	Events      <-chan agent.Event
	Approver    *Approver
	Jobs        JobsAPI
	Sessions    SessionsAPI
	Models      ModelsAPI      // optional: the model list of /model
	Servers     ServersAPI     // optional: /server (nil = the command says so)
	Skills      SkillsAPI      // optional: /skills, /skill and /<skill> (nil = no Skills loaded)
	Agents      AgentsAPI      // optional: /agents (nil = the command says so)
	MCP         MCPAPI         // optional: /mcp (nil = no MCP servers configured)
	Scopes      ScopesAPI      // optional: the save scopes of /skills, /mcp and /config
	Config      ConfigAPI      // optional: /config (nil = the command says so)
	Cost        CostAPI        // optional: the key usage of /cost (nil = the Session spend only)
	Prompt      PromptAPI      // optional: /prompt and the prompt profile row of /config (nil = the command says so)
	Keys        KeysAPI        // optional: /keys and the Keys row of /config (nil = the command says so)
	History     []chat.Message // optional: the list is pre-filled from it at startup (a resumed session)
	SavedHint   string         // optional: main prints it after Run returns (e.g. "session saved: swarg --continue"); the UI prints nothing
	Tokens      int            // the Session tokens used at start (R-131); EvSpend events update the panel
	Spend       float64        // the Session spend in USD at start (a resumed session); EvSpend events update the panel
	Think       bool           // the thinking mode at start; EvThinking events update the Bottom panel
	Model       string         // the Model choice at start; EvModel events update the Bottom panel
	Provider    string         // the Source name at start ("" = not shown); EvProvider events update the panel
	Project     string
	Limit       int
	Yes         bool
	AutoApprove bool
}
