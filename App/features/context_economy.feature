Feature: Context economy
  The Provider's cache reuses a prompt only when it is an exact prefix of the next one, so the
  history changes in batches: at the end of a Turn, when text is injected, and once per Turn when
  the context is nearly full. Inside a Turn every request only appends (Domain R-16, R-17, R-19).

  Rule: Inside a Turn every request only appends to the previous one

    @S-091
    Scenario: Steps of a Turn append to the history
      Given a Provider that reads 3 small files in turn and then answers "done"
      When the user submits "read all"
      Then every request starts with the Messages of the previous request

    @S-092
    Scenario: Results of a Turn that fits in the context are sent as they are
      Given a Provider that reads 2 small files in turn and then answers "done"
      When the user submits "read both"
      Then the last request carries all ToolResults in full

    @S-093
    Scenario: A failed ToolCall does not change the earlier Messages
      Given a Provider that reads a missing file, then reads an existing one and then answers "done"
      When the user submits "read"
      Then every request starts with the Messages of the previous request

  Rule: An overfull Turn gets one emergency batch of Stubs

    @S-094
    Scenario: Old ToolResults of an overfull Turn become Stubs in one batch
      Given a context limit of 6000 tokens
      And a file "log.txt" with 150 lines
      And a Provider that searches "log.txt" 6 times in turn and then answers "done"
      When the user submits "search"
      Then the request after the batch carries a Stub for the oldest ToolResult and the last 2 ToolResults in full

    @S-095
    Scenario: Two ToolResults are never stubbed inside a Turn
      Given a context limit of 1500 tokens
      And a file "log.txt" with 150 lines
      And a Provider that searches "log.txt" 2 times in turn and then answers "done"
      When the user submits "search"
      Then the last request carries all ToolResults in full

    @S-096
    Scenario: After the batch the Turn appends again
      Given a context limit of 6000 tokens
      And a file "log.txt" with 150 lines
      And a Provider that searches "log.txt" 8 times in turn and then answers "done"
      When the user submits "search"
      Then the requests after the batch each start with the Messages of the request before them

    @S-097
    Scenario: A file read that is not yet edited is never stubbed
      Given a context limit of 6000 tokens
      And a Provider that reads 6 large files in turn and then answers "done"
      When the user submits "read all"
      Then no request carries a Stub

  Rule: A finished Turn's ToolResults are sent as Stubs

    @S-098
    Scenario: The next Turn sends the Stub of the last Turn's ToolResult
      Given a file "a.go" with 30 lines
      And a Provider that reads "a.go" and answers "ok", and then answers "again"
      And the user submitted "read it"
      When the user submits "next"
      Then the ToolResult of "read_file" in the last request is "[read_file a.go: 30 lines shown earlier; read it again if needed]"

    @S-099
    Scenario: A ToolResult that is already tiny is sent unchanged
      Given a Provider that writes "a.go" with 3 short lines and answers "ok", and then answers "again"
      And approval is granted
      And the user submitted "write it"
      When the user submits "next"
      Then the ToolResult of "write_file" in the last request is its real output

    @S-100
    Scenario: The history keeps the full ToolResult
      Given a file "a.go" with 30 lines
      And a Provider that reads "a.go" and answers "ok", and then answers "again"
      And the user submitted "read it"
      When the user submits "next"
      Then the ToolResult of "read_file" in the Session history is longer than its Stub

    @S-101
    Scenario: A failed shell command keeps its last lines in the Stub
      Given a Provider that runs a shell command printing 40 lines and exiting with 1, and then answers "ok" and "again"
      And approval is granted
      And the user submitted "run it"
      When the user submits "next"
      Then the Stub of the shell ToolResult in the last request has the line "line 40" and "exit 1" and not the line "line 1"

    @S-102
    Scenario: User Messages and final answers are never stubbed
      Given a file "a.go" with 30 lines
      And a Provider that reads "a.go" and answers "final answer", and then answers "again"
      And the user submitted "read it"
      When the user submits "next"
      Then the last request contains the user Message "read it" and the assistant Message "final answer" unchanged

    @S-103
    Scenario: A denied ToolCall keeps its denial text
      Given a Provider that writes "a.go" with 3 short lines and answers "ok", and then answers "again"
      And approval is denied
      And the user submitted "write it"
      When the user submits "next"
      Then the ToolResult of "write_file" in the last request starts with "denied by the user"

  Rule: A superseded read is stale

    @S-104
    Scenario: A file read twice leaves the older read stale
      Given a file "a.go" with 30 lines
      And a Provider that reads "a.go" twice and answers "ok", and then answers "again"
      And the user submitted "read it twice"
      When the user submits "next"
      Then the older ToolResult of "read_file" in the last request says "stale"

    @S-105
    Scenario: A read followed by an edit of the same file is stale
      Given a file "a.go" with 30 lines
      And a Provider that reads "a.go", edits it and answers "ok", and then answers "again"
      And approval is granted
      And the user submitted "change it"
      When the user submits "next"
      Then the ToolResult of "read_file" in the last request is "[read_file a.go: stale, the file changed later]"

    @S-106
    Scenario: A read of another file is not stale
      Given a file "a.go" with 30 lines
      And a file "b.go" with the text "package b"
      And a Provider that reads "a.go", then edits "b.go" and answers "ok", and then answers "again"
      And approval is granted
      And the user submitted "change b"
      When the user submits "next"
      Then the ToolResult of "read_file" in the last request does not say "stale"

    @S-107
    Scenario: A failed edit does not make the read stale
      Given a file "a.go" with 30 lines
      And a Provider that reads "a.go", then edits it with a text that is not in the file and answers "ok", and then answers "again"
      And approval is granted
      And the user submitted "change it"
      When the user submits "next"
      Then the ToolResult of "read_file" in the last request does not say "stale"

  Rule: Arguments of written files shrink to valid JSON once the Turn is over

    @S-108
    Scenario: write_file arguments shrink at the Turn end
      Given a Provider that writes "a.go" with 40 lines and answers "ok", and then answers "again"
      And approval is granted
      And the user submitted "write it"
      When the user submits "next"
      Then the write_file ToolCall in the last request has valid JSON arguments for "a.go" omitted from history with 40 lines

    @S-109
    Scenario: edit_file arguments shrink at the Turn end
      Given a file "a.go" with 40 lines
      And a Provider that rewrites "a.go" with 40 other lines and answers "ok", and then answers "again"
      And approval is granted
      And the user submitted "edit it"
      When the user submits "next"
      Then the edit_file ToolCall in the last request has valid JSON arguments for "a.go" omitted from history with 40 lines

    @S-110
    Scenario: Small arguments are not shrunk
      Given a Provider that writes "a.go" with 3 short lines and answers "ok", and then answers "again"
      And approval is granted
      And the user submitted "write it"
      When the user submits "next"
      Then the write_file ToolCall in the last request has the arguments it was sent with

    @S-111
    Scenario: shell arguments are not shrunk
      Given a Provider that runs the shell command "echo hi" and answers "ok", and then answers "again"
      And approval is granted
      And the user submitted "run it"
      When the user submits "next"
      Then the shell ToolCall in the last request has the arguments it was sent with

  Rule: The meter shows how much of the prompt the server reused

    @S-112
    Scenario: Cache share from the server usage
      Given a Provider whose answer reports 1000 prompt tokens of which 800 were cached
      When the user submits "hi"
      Then the meter shows a cache share of 80 percent

    @S-113
    Scenario: No cached tokens reported
      Given a Provider whose answer reports 1000 prompt tokens and no cached tokens
      When the user submits "hi"
      Then the meter shows a cache share of 0 percent

    @S-114
    Scenario: A failed Turn keeps the last cache share
      Given a Provider whose answer reports 1000 prompt tokens of which 800 were cached and then fails with a server error
      And the user submitted "hi"
      When the user submits "again"
      Then the meter shows a cache share of 80 percent
