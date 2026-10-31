package agent

import (
	"context"
	"errors"
	"strings"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// The summary request: one short model call per WorkSpan (PLAN 2.6)
// ---------------------------------------------------------------------------

// summaryPrompt is the fixed system prompt of every summary request. A request is recognised
// by it and by having no tools.
const summaryPrompt = "Write a short work log of the steps below. At most 8 lines, plain text. " +
	"Keep: files read or changed (paths), commands run and their result (exit code, key numbers), " +
	"errors with exact text, what is unfinished. Skip file contents and long outputs."

const (
	summaryMaxTokens = 300 // MaxTokens of a summary request
	chunkPercent     = 50  // a summary request input is at most this share of ContextLimit
	chunkMinBytes    = 1000
	foldReserve      = 1500 // bytes kept free in a chunk for the summary of the chunks before it
)

// summaryRecord is what the Trace learns about the summary requests of one span.
type summaryRecord struct {
	inputs  []string // user text of every request
	replies []string
}

// summarize asks the model for the work log of s: one request, or several when the input would
// pass chunkPercent of ContextLimit (each chunk's summary leads the next chunk's input). It
// returns the trimmed text ("" when the model said nothing) or the first error.
func (a *Agent) summarize(ctx context.Context, s span, rec *summaryRecord) (string, error) {
	prev := ""
	for _, steps := range a.chunks(s) {
		in := summaryInput(s.task, prev, steps)
		reply, err := a.ask(ctx, a.summaryRequest(in))
		rec.inputs, rec.replies = append(rec.inputs, in), append(rec.replies, reply)
		if err != nil {
			return "", err
		}
		if prev = strings.TrimSpace(reply); prev == "" {
			return "", nil
		}
	}
	return prev, nil
}

// summaryRequest is the Request for one chunk: the chosen model, the same Extra, no tools.
func (a *Agent) summaryRequest(input string) chat.Request {
	a.mu.Lock()
	model, extra := a.model, a.src.Extra
	a.mu.Unlock()
	return chat.Request{
		Model:     model,
		Messages:  []chat.Message{{Role: chat.RoleSystem, Content: summaryPrompt}, {Role: chat.RoleUser, Content: input}},
		MaxTokens: summaryMaxTokens,
		Extra:     extra,
	}
}

// summaryInput renders the user text of a summary request.
func summaryInput(task, prev string, steps []string) string {
	var b strings.Builder
	if task != "" {
		b.WriteString("Task: " + clipHead(task, taskClip) + "\n\n")
	}
	if prev != "" {
		b.WriteString("Work log so far:\n" + prev + "\n\n")
	}
	b.WriteString("Steps:\n" + strings.Join(steps, "\n"))
	return b.String()
}

// chunks splits the steps of s so that no request input passes chunkPercent of ContextLimit;
// with no limit, or when everything fits, it is one chunk.
func (a *Agent) chunks(s span) [][]string {
	limit := a.limit()
	whole := summaryInput(s.task, "", s.steps)
	if limit <= 0 || chat.EstimateTokens(summaryPrompt+whole)*100 <= chunkPercent*limit {
		return [][]string{s.steps}
	}
	budget := max(chunkMinBytes, limit*chunkPercent/100*4-len(summaryPrompt)-len(summaryInput(s.task, "", nil))-foldReserve)
	var out [][]string
	var cur []string
	size := 0
	for _, st := range s.steps {
		if len(cur) > 0 && size+len(st)+1 > budget {
			out, cur, size = append(out, cur), nil, 0
		}
		cur, size = append(cur, st), size+len(st)+1
	}
	return append(out, cur)
}

// ask performs one summary request and reads its stream to the end. Its usage only adds to the spend:
// the meter follows the Turn requests.
func (a *Agent) ask(ctx context.Context, req chat.Request) (string, error) {
	ch, err := a.provider().Stream(ctx, req)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case ev, ok := <-ch:
			if !ok {
				return "", errStreamClosed
			}
			switch ev.Kind {
			case chat.EventDelta:
				b.WriteString(ev.Text)
			case chat.EventUsage: // the summary costs money too, though the meter ignores it
				a.addSpend(req.Model, ev.Usage.Cost)
			case chat.EventDone:
				if ev.Err != nil {
					return "", ev.Err
				}
				if ev.Finish == chat.FinishError {
					return "", errors.New("the summary request failed")
				}
				return b.String(), nil
			}
		}
	}
}
