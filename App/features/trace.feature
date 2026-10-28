Feature: Trace
  The Trace is the debug log of what the agent did. It never changes or fails a Turn (Domain I-7).
  The Compaction trace scenarios are in compaction.feature.

  Rule: The Trace records what the agent did

    @S-138
    Scenario: A ToolCall is traced with its full arguments and the output the Provider received
      Given the Trace is recorded
      And a file "big.txt" with 2000 lines
      And a Provider that reads "big.txt" and answers "ok", and then answers "again"
      When the user submits "read it"
      Then the Trace holds the read_file ToolCall for "big.txt" with its full arguments and the capped output the Provider received

    @S-139
    Scenario: A request record holds only the Messages appended since the previous request
      Given the Trace is recorded
      And a file "a.txt" with 3 lines
      And a Provider that reads "a.txt" and answers "done", and then answers "again"
      When the user submits "read"
      Then the request records hold the user Message, then the assistant Message with its ToolResult

    @S-140
    Scenario: Usage and cache are traced
      Given the Trace is recorded
      And a Provider whose answer reports 1000 prompt tokens of which 800 were cached
      When the user submits "hi"
      Then the Trace holds a usage record with 1000 prompt and 800 cached tokens

    @S-141
    Scenario: Stub batches are traced
      Given the Trace is recorded
      And a file "big.txt" with 150 lines
      And a Provider that reads "big.txt" and answers "one", and then answers "two"
      When the user submits "read it"
      Then the Trace holds a stub batch naming the read_file ToolResult

    @S-142
    Scenario: A reply is traced once with its full text
      Given the Trace is recorded
      And a Provider that streams the answer "hello there" and finishes normally
      When the user submits "hi"
      Then the Trace holds one reply record with the text "hello there"

    @S-165
    Scenario: Compaction inputs and the fallback are traced
      Given the Trace is recorded
      And a Session with one finished Turn that used a read_file Tool
      And a Provider whose summary requests fail
      When the user compacts the Session
      Then the Trace holds the summary request input and a record that the fallback was used

  Rule: The Trace never changes or fails a Turn (I-7)

    @S-143
    Scenario: Tracing off writes nothing
      Given tracing is switched off
      And a Provider that streams the answer "hi" and finishes normally
      When the user submits "hello"
      Then no Trace exists

    @S-144
    Scenario: A Trace that cannot be written does not end the Turn
      Given a Trace that cannot be written
      And a Provider that streams the answer "hi" and finishes normally
      When the user submits "hello"
      Then the Session history is the user Message "hello" followed by the assistant Message "hi"

    @S-145
    Scenario: A Trace that cannot be written is reported once
      Given a Trace that cannot be written
      And a Provider that streams the answer "hi" and finishes normally
      When the user submits "hello"
      Then a warning starting with "the trace is no longer written" is shown

    @S-146
    Scenario: Tracing does not change the requests
      Given the Trace is recorded
      And a file "a.txt" with 3 lines
      And a Provider that reads "a.txt" and answers "done", and then answers "again"
      When the user submits "read"
      Then the requests are identical to the requests of a run with tracing off
