package session

import (
	"bytes"
	"encoding/json"
	"time"

	"swarg/internal/chat"
)

// ---------------------------------------------------------------------------
// Versioned records (the on-disk format, independent of chat.Message)
// ---------------------------------------------------------------------------

// Version is the SessionFile format version written in the header.
const Version = 1

// Header is the first line of a SessionFile.
type Header struct {
	V        int       `json:"v"`
	ID       string    `json:"id"`
	Cwd      string    `json:"cwd"`
	Model    string    `json:"model"`
	Started  time.Time `json:"started"`
	Provider string    `json:"provider,omitempty"` // the Source at the start (R-101); "" = the configured one
}

// record is one line of the file. Exactly one of the payload fields is set;
// a header line has V != 0 and none of them.
type record struct {
	V        int       `json:"v,omitempty"`
	ID       string    `json:"id,omitempty"`
	Cwd      string    `json:"cwd,omitempty"`
	Model    string    `json:"model,omitempty"`
	Started  time.Time `json:"started,omitzero"`
	Provider string    `json:"provider,omitempty"` // the header's Source, or a switch line (the last one wins)

	Msg     *msgRecord     `json:"msg,omitempty"`
	Patch   []patchRecord  `json:"patch,omitempty"`
	Clear   bool           `json:"clear,omitempty"`
	Compact *compactRecord `json:"compact,omitempty"`
	Repair  []msgRecord    `json:"repair,omitempty"`
	Think   *bool          `json:"think,omitempty"` // the thinking mode was switched (/think); the last one wins
	Name    string         `json:"name,omitempty"`  // the Session was given a Name (/rename); the last one wins
}

type msgRecord struct {
	Seq         int          `json:"seq"`
	Role        string       `json:"role"`
	Content     string       `json:"content,omitempty"`
	Reasoning   string       `json:"reasoning,omitempty"`
	Truncated   bool         `json:"truncated,omitempty"`
	Interrupted bool         `json:"interrupted,omitempty"`
	ToolCalls   []callRecord `json:"tool_calls,omitempty"`
	ToolCallID  string       `json:"tool_call_id,omitempty"`
	Name        string       `json:"name,omitempty"`
	Brief       string       `json:"brief,omitempty"`
	Stub        string       `json:"stub,omitempty"`
	Summary     bool         `json:"summary,omitempty"`
	TurnStart   bool         `json:"turn_start,omitempty"`
	Injected    bool         `json:"injected,omitempty"`
	Label       string       `json:"label,omitempty"`
	Failed      bool         `json:"failed,omitempty"`
}

type callRecord struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Args string `json:"args"`
}

type patchRecord struct {
	Seq  int               `json:"seq"`
	Stub string            `json:"stub,omitempty"`
	Args map[string]string `json:"args,omitempty"` // tool call id -> shrunk raw JSON arguments
}

type compactRecord struct {
	FromSeq int       `json:"from_seq"`
	ToSeq   int       `json:"to_seq"`
	Summary msgRecord `json:"summary"`
}

// ---------------------------------------------------------------------------
// Message mapping (every chat.Message field is carried: replay must equal the live history)
// ---------------------------------------------------------------------------

func toDTO(seq int, m chat.Message) msgRecord {
	r := msgRecord{
		Seq:         seq,
		Role:        string(m.Role),
		Content:     m.Content,
		Reasoning:   m.Reasoning,
		Truncated:   m.Truncated,
		Interrupted: m.Interrupted,
		ToolCallID:  m.ToolCallID,
		Name:        m.Name,
		Brief:       m.Brief,
		Stub:        m.Stub,
		Summary:     m.Summary,
		TurnStart:   m.TurnStart,
		Injected:    m.Injected,
		Label:       m.Label,
		Failed:      m.Failed,
	}
	for _, c := range m.ToolCalls {
		r.ToolCalls = append(r.ToolCalls, callRecord{ID: c.ID, Name: c.Name, Args: c.Args})
	}
	return r
}

func fromDTO(r msgRecord) chat.Message {
	m := chat.Message{
		Role:        chat.Role(r.Role),
		Content:     r.Content,
		Reasoning:   r.Reasoning,
		Truncated:   r.Truncated,
		Interrupted: r.Interrupted,
		ToolCallID:  r.ToolCallID,
		Name:        r.Name,
		Brief:       r.Brief,
		Stub:        r.Stub,
		Summary:     r.Summary,
		Seq:         r.Seq,
		TurnStart:   r.TurnStart,
		Injected:    r.Injected,
		Label:       r.Label,
		Failed:      r.Failed,
	}
	for _, c := range r.ToolCalls {
		m.ToolCalls = append(m.ToolCalls, chat.ToolCall{ID: c.ID, Name: c.Name, Args: c.Args})
	}
	return m
}

// ---------------------------------------------------------------------------
// Encoding
// ---------------------------------------------------------------------------

// encodeLine marshals v as one JSON line (no HTML escaping, trailing newline).
func encodeLine(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil // Encode already ends with '\n'
}
