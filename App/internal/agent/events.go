package agent

import "swarg/internal/chat"

// ---------------------------------------------------------------------------
// Events
// ---------------------------------------------------------------------------

// EventKind tells which fields of an Event are meaningful.
type EventKind int

// Event kinds emitted through Deps.OnEvent.
const (
	EvTurnStarted EventKind = iota + 1
	EvDelta
	EvActivity // a streamed chunk that is not visible text: reasoning or tool-call arguments (R-61)
	EvProgress
	EvDraining
	EvTurnEnded
	EvMeter
	EvWarning
	EvQueue
	EvUserText   // a queued user text was injected into the running Turn
	EvToolStart  // a tool call begins
	EvToolEnd    // a tool call has a result
	EvToolOutput // a running tool produced a chunk of live output (display only)

	EvCompactStarted // a Compaction began: Spans = WorkSpans it will look at, Before = estimated tokens of the next request
	EvCompacted      // a Compaction ended (always follows EvCompactStarted): Spans replaced, Before and After estimated tokens

	EvModel    // the Model choice changed (SetModel, Load WithModel): Text = the model id
	EvProvider // the Source changed (SetSource): Text = the provider name (R-100)
	EvThinking // the thinking mode changed (SetThinking, Load WithThinking): Text = "on" or "off"
	EvSpend    // the Session spend changed (a response had a cost, Load WithSpend): Spend = the new total in USD (R-111)
	EvSpeed    // a Step's stream ended with a measured Generation speed: Speed

	EvTaskStarted // a Task began: Task = its id, Depth, Text = the goal, Call = the `task` call
	EvTaskEnded   // a Task ended: Task = its id, Brief = line 1 of the TaskResult, Output = the whole result
)

// Event is one notification from the worker to the UI. Task names the Task an event of the loop
// comes from (Delta, Progress, Draining, Tool*, Meter, Speed); it is "" for the Session.
type Event struct {
	Kind        EventKind
	Task        string            // the Task id of the loop that emitted it; "" = the Session
	Depth       int               // TaskStarted: the Task's depth (1 = started by the Session)
	Text        string            // TurnStarted, UserText: user text; Delta, Activity, ToolOutput: chunk; Warning: message; TaskStarted: goal
	Done, Total int               // Progress
	Message     chat.Message      // TurnEnded: stored assistant message (zero if none)
	Finish      chat.FinishReason // TurnEnded
	Err         error             // TurnEnded; ToolEnd: tool or validation error (nil on success)
	Used, Limit int               // Meter
	Estimated   bool              // Meter
	CachePct    int               // Meter: share of the last request's prompt served from the server cache
	HasCache    bool              // Meter: CachePct is known (a usage report has been seen)
	Breakdown   Breakdown         // Meter: estimated tokens of the next request by category
	Spend       float64           // Spend: the Session spend in USD
	Tokens      int               // Spend: the Session tokens used (prompt + completion, R-131)
	Queued      int               // Queue: items waiting (not counting the running one)
	Spans       int               // CompactStarted: WorkSpans to look at; Compacted: WorkSpans replaced
	Before      int               // CompactStarted, Compacted: estimated tokens of the next request before the Compaction
	After       int               // Compacted: ... after it

	Call   chat.ToolCall // ToolStart, ToolOutput, ToolEnd: the call as the model sent it (raw args)
	Label  string        // ToolStart, ToolEnd: one-line description of the call
	Brief  string        // ToolEnd: short result for the screen
	Output string        // ToolEnd: the full result text sent to the model

	Speed float64 // Speed: completion tokens per second of the Step (R-29)
}
