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

// ModelsAPI lists the models the Provider offers (main adapts openai.Client.Models). Deps.Models
// may be nil: /model then can only set an id given on the command line.
type ModelsAPI interface {
	List(ctx context.Context) ([]string, error)
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
	taskSubmitter interface {
		SubmitTask(goal string) error
	} // /task
)

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
	History     []chat.Message // optional: the list is pre-filled from it at startup (a resumed session)
	SavedHint   string         // optional: main prints it after Run returns (e.g. "session saved: swarg --continue"); the UI prints nothing
	Model       string         // the Model choice at start; EvModel events update the Bottom panel
	Project     string
	Limit       int
	Yes         bool
	AutoApprove bool
}
