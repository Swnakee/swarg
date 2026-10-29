// Command swarg is a light terminal coding agent for local LLMs.
//
// Interactive when stdin and stdout are terminals, otherwise plain mode:
// echo "question" | swarg.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"time"

	"golang.org/x/term"

	"swarg/internal/agent"
	"swarg/internal/config"
	"swarg/internal/llm/openai"
	"swarg/internal/prompt"
	"swarg/internal/session"
	"swarg/internal/tools"
	"swarg/internal/ui"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "0.0.0-dev"

// ---------------------------------------------------------------------------
// Entry point
// ---------------------------------------------------------------------------

func main() {
	code, err := run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "swarg: %v\n", err)
	}
	os.Exit(code)
}

// run returns the exit code (0 ok, 1 runtime error, 2 config or flag error)
// and the error to report.
func run() (int, error) {
	o, err := parseArgs(os.Args[1:], os.Stderr)
	switch {
	case errors.Is(err, errHelp):
		return 0, nil
	case err != nil:
		return 2, err
	}
	if o.version {
		fmt.Println("swarg", version)
		return 0, nil
	}

	dir, err := config.Dir()
	if err != nil {
		return 2, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return 1, err
	}
	sdir := session.Dir(dir, cwd) // SessionFiles of this project
	if o.trace {
		return runTrace(sdir, o.traceID, os.Stdout)
	}
	cfg, err := loadConfig(o.configPath, dir)
	if err != nil {
		return 2, err
	}
	cfg.Override(o.providerOverride())
	if err := cfg.Validate(); err != nil {
		return 2, err
	}

	// which session to continue, if any
	var resume session.Info
	if o.sessions || o.cont || o.resume != "" || o.pick {
		infos, err := session.List(sdir)
		if err != nil {
			return 1, err
		}
		if o.sessions {
			printSessions(infos, os.Stdout)
			return 0, nil
		}
		if o.pick {
			id, code, done := choosePick(infos, isTTY(), ui.PickSession,
				func(id string) error { return session.Delete(sdir, id) }, os.Stdout, os.Stderr)
			if done {
				return code, nil
			}
			o.resume = id
		}
		if resume, err = pickSession(infos, o.resume, o.cont); err != nil {
			return 1, err
		}
		if o.cont && resume.Path == "" {
			fmt.Fprintln(os.Stderr, "swarg: no previous session in this project, starting a new one")
		}
	}

	root, err := os.OpenRoot(cwd)
	if err != nil {
		return 1, err
	}
	defer root.Close()
	all := tools.All()
	if !cfg.Web.Confirm {
		all = tools.FreeWeb(all)
	}
	ts, err := tools.NewSet(all, cfg.Tools.Disabled)
	if err != nil {
		return 2, err
	}
	jobs := tools.NewJobs()
	defer jobs.KillAll() // runs before main's os.Exit on every return path
	var cl cleanups      // session and trace files; run on every exit path, signals included
	defer cl.Run()
	killOnSignal(jobs, &cl)

	p := cfg.Active()
	sv, err := openSaving(sdir, resume, session.Header{Cwd: cwd, Model: p.Model},
		cfg.Session.Save && !o.noSave, cfg.Session.Trace && !o.traceOff, cfg.Session.Keep, &cl, os.Stderr)
	if err != nil {
		return 1, err
	}
	model := startModel(o.model, sv.model, p.Model) // R-30: a resumed session keeps its last model
	if o.model == "" && sv.model != "" && sv.model != p.Model {
		fmt.Fprintf(os.Stderr, "swarg: continuing with the session's model %s (--model changes it)\n", sv.model)
	}
	set, warnings, err := prompt.Load(filepath.Join(dir, "blocks"), cwd, promptEnv(cwd, model), promptBlocks(ts, cfg.Tasks.MaxDepth)...)
	for _, w := range warnings {
		fmt.Fprintf(os.Stderr, "swarg: %s\n", w)
	}
	if err != nil {
		return 2, err
	}

	send, events := ui.NewInbox()
	approver := ui.NewApprover(o.yes)
	approver.SetAuto(cfg.Tools.AutoApprove)
	client := openai.New(p.BaseURL, p.APIKey(), nil)
	client.IdleTimeout = p.IdleTimeout() // R-63
	first := model                       // the agent starts with the session's own model; a --model flag is a change
	if sv.model != "" {
		first = sv.model
	}
	var a *agent.Agent
	a = agent.New(agent.Deps{
		Provider:     client,
		SystemPrompt: set.Build,
		Model:        first,
		MaxTokens:    p.MaxTokens,
		ContextLimit: p.ContextLimit,
		Extra:        p.ExtraBody,
		ThinkExtra:   p.ThinkingBody(),
		ThinkTokens:  p.ThinkingLimit(),
		OnEvent:      onModel(set, send),
		Tools: toolRunner{set: ts, env: tools.Env{
			Root:         root,
			Dir:          cwd,
			OS:           runtime.GOOS,
			ShellTimeout: time.Duration(cfg.Tools.ShellTimeoutSec) * time.Second,
			Jobs:         jobs,
			// a background:true command that ends this fast is returned as a normal result
			BackgroundGrace: time.Duration(cfg.Tools.BackgroundGraceSec) * time.Second,
			// shell_output waits this long for a running job to end (fewer polls)
			OutputWait: time.Duration(cfg.Tools.OutputWaitSec) * time.Second,
			Web: tools.Web{SearxURL: cfg.Web.SearxURL, AllowPrivate: cfg.Web.AllowPrivate,
				Timeout: time.Duration(cfg.Web.TimeoutSec) * time.Second},
		}},
		Approve:     approver.Approve,
		Health:      func(ctx context.Context) error { return client.Health(ctx, a.Model()) },
		MaxSteps:    cfg.Tools.MaxSteps,
		Store:       sv.store,
		Trace:       sv.trace,
		AutoCompact: cfg.Context.AutoCompact,
		LimitFor:    p.LimitFor, // R-41: the ContextLimit follows the model
		Tasks:       agent.TaskLimits{MaxDepth: cfg.Tasks.MaxDepth, MaxSteps: cfg.Tasks.MaxSteps, MaxPerTurn: cfg.Tasks.MaxPerTurn},
	})
	if len(sv.history) > 0 {
		if err := a.Load(sv.history, agent.WithMaxSeq(sv.maxSeq), agent.WithThinking(sv.thinking)); err != nil {
			return 1, err
		}
	}
	think := p.Thinking // R-60: a new session starts as configured, a resumed one with its own mode
	if len(sv.history) > 0 {
		think = sv.thinking
	} else {
		a.SetThinking(think)
	}
	if err := a.SetModel(model); err != nil { // records --model over a resumed session's choice
		return 2, err
	}
	d := ui.Deps{
		Agent:       a,
		Blocks:      set,
		Events:      events,
		Approver:    approver,
		Jobs:        jobsView{jobs},
		Models:      modelsView{client},
		Yes:         o.yes,
		AutoApprove: cfg.Tools.AutoApprove,
		Model:       model,
		Think:       think,
		Project:     filepath.Base(cwd),
		Limit:       p.LimitFor(first), // the initial value; EvMeter carries the live one
	}
	if cfg.Session.Save {
		d.Sessions = sv // the picker, /resume (switches the file), /rename, delete
	}
	d.History = sv.history // a resumed session pre-fills the list (the UI prints no saved hint; main does)
	err = runUI(d)
	cl.Run() // flush before the hint: the file is complete when the user reads it
	if sv.saved() {
		fmt.Fprintln(os.Stderr, "session saved: swarg --continue")
	}
	if err != nil {
		return 1, err
	}
	return 0, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// promptBlocks are the tool blocks of the SystemPrompt: the Tools' own and, when Tasks are on
// (max_depth > 0), the `task` line of the agent's built-in Tool (Order after the last tool block).
func promptBlocks(ts *tools.Set, maxDepth int) []prompt.Block {
	blocks := ts.Blocks()
	if maxDepth > 0 {
		blocks = append(blocks, prompt.Block{Name: "tool-task", Source: prompt.SourceTool, Order: 599, Text: agent.TaskBlock, Enabled: true})
	}
	return blocks
}

// onModel passes every event on and, for a Model choice, first rewrites the env block of the
// SystemPrompt, so the next Turn tells the model its new name (R-4, R-30).
func onModel(set *prompt.Set, send func(agent.Event)) func(agent.Event) {
	return func(ev agent.Event) {
		if ev.Kind == agent.EvModel {
			set.SetModel(ev.Text)
		}
		send(ev)
	}
}

// loadConfig loads the explicit --config path, or <dir>/config.json.
func loadConfig(path, dir string) (config.Config, error) {
	if path != "" {
		return config.Load(path, true)
	}
	return config.Load(filepath.Join(dir, "config.json"), false)
}

// promptEnv describes the host for the environment block of the system prompt.
func promptEnv(cwd, model string) prompt.Env {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "sh"
		if runtime.GOOS == "windows" {
			shell = "pwsh"
		}
	}
	return prompt.Env{
		OS:    runtime.GOOS,
		Shell: shell,
		Dir:   cwd,
		Date:  time.Now().Format("2006-01-02"),
		Model: model,
	}
}

// runUI starts the terminal UI when stdin and stdout are terminals, otherwise
// plain mode. Only plain mode turns Ctrl+C into an interrupt of the turn: the
// first one interrupts (drain), the second one aborts the turn at once.
func runUI(d ui.Deps) error {
	if isTTY() {
		return ui.Run(context.Background(), d)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	sigs := make(chan os.Signal, 2)
	signal.Notify(sigs, os.Interrupt)
	defer signal.Stop(sigs)
	go func() {
		<-sigs // first: RunPlain sees ctx done and interrupts
		<-sigs // second
		d.Agent.Abort()
	}()
	return ui.RunPlain(ctx, d, os.Stdin, os.Stdout)
}

// isTTY reports whether stdin and stdout are terminals (the full-screen UI runs).
func isTTY() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}
