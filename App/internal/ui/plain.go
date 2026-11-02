package ui

import (
	"context"
	"fmt"
	"io"
	"strings"

	"swarg/internal/agent"
	"swarg/internal/chat"
	"swarg/internal/ui/command"
)

// ---------------------------------------------------------------------------
// Plain mode
// ---------------------------------------------------------------------------

// indent is the left margin of a line printed inside depth Tasks.
func indent(depth int) string { return strings.Repeat("  ", depth) }

// RunPlain runs one non-interactive turn: all of in is one user message and the
// answer is streamed to out as is. Of the slash commands only /skill <name> [text] is
// understood (R-83; the eval suite drives swarg this way); the rest is text. It returns
// the error of a failed turn. Cancelling ctx interrupts the turn (the answer is
// drained, not cut) and RunPlain returns when the agent is idle.
func RunPlain(ctx context.Context, d Deps, in io.Reader, out io.Writer) error {
	raw, err := io.ReadAll(in)
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		fmt.Fprintln(out, `usage: echo "question" | swarg`)
		return nil
	}

	// The agent must not see ctx cancelled: cutting a request wedges the
	// single-lane server, so Ctrl+C only asks for Interrupt (drain).
	d.Agent.Start(context.WithoutCancel(ctx))
	if c, err := command.Parse(text); err == nil && c.Kind == command.KindSkill {
		ss, ok := d.Agent.(skillSubmitter)
		if !ok {
			fmt.Fprintln(out, "no skills loaded")
			return nil
		}
		if err := ss.SubmitSkill(c.Name, c.Text); err != nil {
			fmt.Fprintln(out, "skill: "+err.Error())
			return nil
		}
	} else {
		d.Agent.Submit(text)
	}

	var (
		turnErr error
		ended   bool
		atBOL   = true // output is at the start of a line
		calls   int    // finished tool calls of this run, for the #N label
		depth   int    // running Tasks: their tool lines are indented, their text is not printed
		done    = ctx.Done()
	)
	endLine := func() {
		if !atBOL {
			fmt.Fprintln(out)
			atBOL = true
		}
	}
	notice := func(s string) {
		endLine()
		fmt.Fprintln(out, s)
	}
	for !ended || len(d.Events) > 0 {
		select {
		case ev := <-d.Events:
			switch ev.Kind {
			case agent.EvTaskStarted:
				notice(indent(depth) + "[task: " + firstLine(Clean(ev.Text)) + "]")
				depth++
			case agent.EvTaskEnded:
				depth = max(depth-1, 0)
				notice(indent(depth) + "[task " + firstLine(Clean(ev.Brief)) + "]")
			case agent.EvDelta:
				if ev.Task != "" { // a Task's chatter stays off stdout
					break
				}
				text := Clean(ev.Text)
				fmt.Fprint(out, text)
				if text != "" {
					atBOL = strings.HasSuffix(text, "\n")
				}
			case agent.EvToolEnd:
				calls++
				head, tail := ToolLine(calls, ev.Call.Name, ev.Label, ev.Brief, ev.Err)
				notice(indent(depth) + head)
				notice(indent(depth) + tail)
			case agent.EvWarning:
				notice("[warning: " + ev.Text + "]")
			case agent.EvTurnEnded:
				endLine()
				ended, turnErr = true, ev.Err
				switch ev.Finish {
				case chat.FinishLength:
					notice("[answer truncated: token limit]")
				case chat.FinishInterrupted:
					notice("[interrupted]")
				case chat.FinishError:
					notice(fmt.Sprintf("[error: %v]", ev.Err))
				}
				// After Wait all events of the turn are buffered; the loop drains them.
				d.Agent.Wait()
			default:
				if line, ok := compactLine(ev); ok {
					notice(line)
				}
			}
		case <-done:
			d.Agent.Interrupt()
			done = nil
		}
	}
	return turnErr
}
