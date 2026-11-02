Feature: Approval

  Rule: A mutating Tool runs only with Approval

    @S-061
    Scenario: An approved ToolCall runs
      Given approval is granted
      And a Provider that calls "write_file" for "n.txt" with "hi" and then answers "ok"
      When the user submits "write"
      Then the file "n.txt" contains "hi"

    @S-062
    Scenario: A denied ToolCall does not run
      Given approval is denied
      And a Provider that calls "write_file" for "n.txt" with "hi" and then answers "ok"
      When the user submits "write"
      Then the file "n.txt" does not exist

    @S-063 @R-6
    Scenario: A denial note reaches the Provider
      Given approval is denied with the note "use another name"
      And a Provider that calls "write_file" for "n.txt" with "hi" and then answers "ok"
      When the user submits "write"
      Then the second request contains a ToolResult "denied by the user: use another name"

    @S-064
    Scenario: A read-only ToolCall asks for no Approval
      Given approval is denied
      And a file "notes.txt" with the text "alpha"
      And a Provider that calls "read_file" for "notes.txt" and then answers "ok"
      When the user submits "read"
      Then approval was asked 0 times

    @S-065
    Scenario: Without an approval function every ToolCall is allowed
      Given no approval function is set
      And a Provider that calls "write_file" for "n.txt" with "hi" and then answers "ok"
      When the user submits "write"
      Then the file "n.txt" contains "hi"

    @S-066
    Scenario: A denied ToolCall does not end the Turn
      Given approval is denied
      And a Provider that calls "shell" for "echo hi" and then answers "understood"
      When the user submits "run"
      Then the last assistant Message is "understood"

    @S-075
    Scenario: Repeated denials end the Turn
      Given approval is denied
      And a Provider that keeps calling "write_file" for "n.txt" with "hi"
      When the user submits "write"
      Then a warning "stopped: tool calls were denied" is shown
