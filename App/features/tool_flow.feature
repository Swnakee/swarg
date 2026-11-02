Feature: Tool scheduling

  Rule: Read-only Tools run together, mutating Tools one at a time

    @S-050 @R-8
    Scenario: Read-only ToolCalls overlap
      Given a Provider that calls three read-only Tools in one answer
      When the user submits "read all"
      Then the three Tools were running at the same time

    @S-051 @R-8
    Scenario: Mutating ToolCalls never overlap
      Given a Provider that calls three mutating Tools in one answer
      When the user submits "change all"
      Then at most one Tool was running at any time

    @S-052 @R-8
    Scenario: Mutating ToolCalls run in call order
      Given a Provider that calls the mutating Tools "a", "b", "c" in one answer
      When the user submits "change all"
      Then the Tools finished in the order a, b, c

    @S-053 @R-8
    Scenario: ToolResults keep call order whatever finishes first
      Given a Provider that calls the read-only Tools "slow" and "fast" in one answer
      When the user submits "read both"
      Then the ToolResults in the history are in the order slow, fast

  Rule: Text sent during a Turn reaches the Provider at the next Step

    @S-054 @R-1
    Scenario: Text sent while a Tool runs is sent with its result
      Given a Provider that calls a blocked Tool and then answers "ok"
      And the user submitted "start" and the Tool is running
      When the user submits "also this"
      Then the second request ends with the ToolResult followed by the user Message "also this"

    @S-055 @R-6
    Scenario: A mutating ToolCall not yet started is skipped
      Given a Provider that calls the blocked mutating Tools "first" and "second" in one answer
      And the user submitted "go" and the Tool "first" is running
      When the user submits "stop, new plan"
      Then the ToolResult of "second" is "skipped: the user sent a new message"

    @S-056
    Scenario: A read-only ToolCall already started is not cancelled
      Given a Provider that calls the blocked read-only Tool "r" and then answers "ok"
      And the user submitted "go" and the Tool "r" is running
      When the user submits "extra"
      Then the ToolResult of "r" is its real output

    @S-057
    Scenario: Injected text does not start a second Turn
      Given a Provider that calls a blocked Tool and then answers "ok"
      And the user submitted "start" and the Tool is running
      When the user submits "also this"
      Then exactly one Turn ran

  Rule: A Turn ends after MaxSteps Steps

    @S-058 @R-12
    Scenario: The step limit ends the Turn with a warning
      Given the step limit is 2 and a Provider that always calls a Tool
      When the user submits "loop"
      Then a warning "step limit reached (2)" is shown

    @S-059 @R-12
    Scenario: A Turn within the step limit has no warning
      Given the step limit is 2 and a Provider that calls a Tool once and then answers "ok"
      When the user submits "once"
      Then no warning is shown

    @S-060 @R-6 @R-12
    Scenario: The history stays valid after the step limit
      Given the step limit is 2 and a Provider that always calls a Tool
      When the user submits "loop"
      Then every ToolCall in the Session history has a ToolResult

  Rule: A ToolCall that keeps failing the same way is cut short (R-24)

    @S-169
    Scenario: The second identical failure tells the model to stop repeating it
      Given a Provider that calls "shell_output" with the arguments "{}" 2 times in a row and then answers "ok"
      When the user submits "go"
      Then the last ToolResult in the Session history mentions "failed 2 times in a row"

    @S-170
    Scenario: A single failure carries no note
      Given a Provider that calls "shell_output" with the arguments "{}" 1 times in a row and then answers "ok"
      When the user submits "go"
      Then no ToolResult in the Session history mentions "failed 2 times in a row"

    @S-171
    Scenario: Two identical failures do not end the Turn
      Given a Provider that calls "shell_output" with the arguments "{}" 2 times in a row and then answers "ok"
      When the user submits "go"
      Then the last assistant Message is "ok"

    @S-172
    Scenario: The third identical failure ends the Turn
      Given a Provider that calls "shell_output" with the arguments "{}" 3 times in a row and then answers "ok"
      When the user submits "go"
      Then a warning "stopped: the same tool call kept failing" is shown

    @S-173
    Scenario: Alternating different failing calls are not stopped
      Given a Provider that calls "read_file" for the missing files "a.txt" and "b.txt" alternately 3 times and then answers "ok"
      When the user submits "go"
      Then the last assistant Message is "ok"

  Rule: A ToolCall that keeps returning the same result is cut short (R-24)

    @S-179
    Scenario: The second identical result tells the model to stop repeating the call
      Given a Provider that lists the project folder 2 times in a row and then answers "ok"
      When the user submits "go"
      Then the last ToolResult in the Session history mentions "returned the same result 2 times in a row"

    @S-180
    Scenario: A single result carries no note
      Given a Provider that lists the project folder 1 times in a row and then answers "ok"
      When the user submits "go"
      Then no ToolResult in the Session history mentions "the same result"

    @S-181
    Scenario: The third identical result ends the Turn
      Given a Provider that lists the project folder 3 times in a row and then answers "ok"
      When the user submits "go"
      Then a warning "stopped: the same tool call kept returning the same result" is shown

    @S-182
    Scenario: Waiting on a background job is not a repeat
      Given a Provider that polls a finished background job 6 times in a row and then answers "ok"
      When the user submits "go"
      Then the last assistant Message is "ok"

  Rule: Two calls that keep alternating with the same results are cut short (R-44)

    @S-339
    Scenario: Three full alternations end the Turn
      Given a Provider that lists the project folder and reads the missing file "a.txt" alternately 6 times and then answers "ok"
      When the user submits "go"
      Then a warning "stopped: the same tool calls kept repeating" is shown

    @S-340
    Scenario: Two full alternations only add a note
      Given a Provider that lists the project folder and reads the missing file "a.txt" alternately 5 times and then answers "ok"
      When the user submits "go"
      Then the last assistant Message is "ok"

    @S-341
    Scenario: A different call inside the alternation breaks the cycle
      Given a Provider that lists the project folder and reads the missing file "a.txt" alternately 4 times, then reads the missing file "b.txt" and does the pair once more and then answers "ok"
      When the user submits "go"
      Then the last assistant Message is "ok"

    @S-342
    Scenario: Eight identical polls of a background job are not a cycle
      Given a Provider that polls a finished background job 8 times in a row and then answers "ok"
      When the user submits "go"
      Then the last assistant Message is "ok"

    @S-343
    Scenario: Restarting the job under a new id each time is still a cycle
      Given a Provider that starts a background job and polls the first one alternately 6 times and then answers "ok"
      When the user submits "go"
      Then a warning "stopped: the same tool calls kept repeating" is shown
