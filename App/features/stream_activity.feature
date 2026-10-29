Feature: Stream activity
  Everything the model streams is reported to the UI while it arrives, so the Progress line shows
  activity when the model reasons or writes the arguments of a tool call, not only when it writes
  visible text.

  Rule: Every streamed chunk is reported: visible text as a Delta, reasoning and tool-call arguments as Activity (R-61)

    @S-377
    Scenario: Reasoning chunks are reported as activity
      Given a Provider that thinks "deep thought" and then answers "ok"
      When the user sends "go"
      Then the UI received activity "deep thought"

    @S-378
    Scenario: Streamed tool-call arguments are reported as activity
      Given a Provider that streams the arguments of "list_dir" in chunks and then answers "ok"
      When the user sends "go"
      Then the UI received activity "path"
