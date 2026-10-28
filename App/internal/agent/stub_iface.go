package agent

import "swarg/internal/chat"

// ---------------------------------------------------------------------------
// Optional tool history policy
// ---------------------------------------------------------------------------

// ToolStubber is an optional interface of a ToolRunner (found by type assertion, so the
// ToolRunner interface and its fakes stay as they are). It tells the agent how a finished call
// shrinks in history.
type ToolStubber interface {
	// Stub is the short text sent to the model instead of output once the Turn is over, or ""
	// to keep the output. brief is the result's one-line summary; error results give "".
	Stub(call chat.ToolCall, output, brief string) string
	// Touched is the clean path a file-changing call (edit_file, write_file) changed, or "".
	// Relative to the project folder, slash form.
	Touched(call chat.ToolCall) string
}
