Feature: Task mode
  A Task is the agent loop on a goal in its own history; its parent gets only the TaskResult and
  the Session continues as if only the result had been there. Tasks nest up to a depth (default 8).

  Background:
    Given Task mode allows depth 8

  Rule: A Task's Messages never enter its parent's history (R-37)

    @S-300
    Scenario: The helper's reads stay out of the Session
      Given a file "notes.txt" with the text "port 8081"
      And the Provider starts a Task "collect facts"
      And the helper calls "read_file" for "notes.txt" and then reports "port 8081"
      And the Provider then answers "done, 8081"
      When the user sends "find the port"
      Then the final request carries no ToolResult of "read_file"

    @S-301
    Scenario: A Task inside a Task leaves one TaskResult in the Session
      Given the Provider starts a Task "outer"
      And the helper starts a Task "inner"
      And the helper reports "inner done"
      And the helper reports "outer done"
      And the Provider then answers "ok"
      When the user sends "go"
      Then the Session history has exactly 1 TaskResult

    @S-302
    Scenario: A failed helper request is an error result and the Turn goes on
      Given the Provider starts a Task "x"
      And the helper's request fails
      And the Provider then answers "ok"
      When the user sends "go"
      Then the TaskResult starts with "error: failed:" and the Turn ends with the answer "ok"

    @S-303
    Scenario: A Task between two reads keeps the call order
      Given the Provider calls "list_dir", starts a Task "x" and calls "list_dir" again
      And the helper reports "r"
      And the Provider then answers "ok"
      When the user sends "go"
      Then the ToolResults of the Session are list_dir, task, list_dir

  Rule: A Task always ends with a TaskResult whose first line carries the status and the files (R-38)

    @S-304
    Scenario: The files a Task changed are on the first line
      Given a file "notes.txt" with the text "port 8081"
      And the Provider starts a Task "fix the port"
      And the helper edits "notes.txt" replacing "8081" with "9090" and then reports "fixed"
      And the Provider then answers "ok"
      When the user sends "go"
      Then the TaskResult's first line is "done; files: notes.txt"

    @S-305
    Scenario: A Task that runs out of Steps reports what it did
      Given Task mode allows depth 1 with 2 steps per Task
      And a file "notes.txt" with the text "port 8081"
      And the Provider starts a Task "x"
      And the helper calls "read_file" for "notes.txt" twice
      And the Provider then answers "ok"
      When the user sends "go"
      Then the TaskResult starts with "stopped: step limit" and lists "read_file"

    @S-306
    Scenario: A Compaction of the parent keeps the status and the files
      Given a file "notes.txt" with the text "port 8081"
      And the Provider starts a Task "fix the port"
      And the helper edits "notes.txt" replacing "8081" with "9090" and then reports "changed the port to 9090 in notes.txt, checked that nothing else names 8081, the service config and the readme are untouched"
      And the Provider then answers "ok"
      And the user sent "go"
      When the user compacts the Session
      Then the WorkSpan is replaced by one WorkSummary containing "done; files: notes.txt"

    @S-307
    Scenario: An empty report is replaced by the list of steps
      Given a file "notes.txt" with the text "port 8081"
      And the Provider starts a Task "x"
      And the helper calls "read_file" for "notes.txt" and then reports ""
      And the Provider then answers "ok"
      When the user sends "go"
      Then the TaskResult is the step list under the status "done"

    @S-308
    Scenario: A Task cut by the repeat guard reports it
      Given the Provider starts a Task "x"
      And the helper calls "read_file" for "missing.txt" three times
      And the Provider then answers "ok"
      When the user sends "go"
      Then the TaskResult starts with "stopped: the same tool call kept failing"

    @S-309
    Scenario: The files of a Task inside a Task bubble up
      Given a file "notes.txt" with the text "port 8081"
      And the Provider starts a Task "outer"
      And the helper starts a Task "inner"
      And the helper edits "notes.txt" replacing "8081" with "9090" and then reports "fixed"
      And the helper reports "outer done"
      And the Provider then answers "ok"
      When the user sends "go"
      Then the TaskResult's first line is "done; files: notes.txt"

  Rule: The task Tool is guarded by depth, a per-Turn cap and the repeated goal (R-39)

    @S-359
    Scenario: A chain of 8 nested Tasks returns its results up to the Session
      Given Task mode allows depth 8
      And the Provider starts a chain of 8 nested Tasks
      And the 8 levels report in turn
      And the Provider then answers "ok"
      When the user sends "go"
      Then the Session history has exactly 1 TaskResult

    @S-360
    Scenario: The 9th level is refused without a request
      Given Task mode allows depth 8
      And the Trace is recorded
      And the Provider starts a chain of 8 nested Tasks
      And the helper starts a Task "level 9"
      And the 8 levels report in turn
      And the Provider then answers "ok"
      When the user sends "go"
      Then the Trace holds 8 task_start records

    @S-310
    Scenario: At the deepest level the task Tool is not offered
      Given Task mode allows depth 1
      And the Provider starts a Task "x"
      And the helper reports "r"
      And the Provider then answers "ok"
      When the user sends "go"
      Then the second request offers no Tool named "task"

    @S-311
    Scenario: The per-Turn cap refuses the next Task without a request
      Given Task mode allows depth 1 and 2 Tasks per Turn
      And the Provider starts a Task "a"
      And the helper reports "r"
      And the Provider starts a Task "b"
      And the helper reports "r"
      And the Provider starts a Task "c"
      And the Provider then answers "ok"
      When the user sends "go"
      Then the task call "c" is refused with "task: too many tasks in this turn, do it yourself" and only 2 helper requests were made

    @S-312
    Scenario: A Task cannot start a Task with its own goal
      Given the Provider starts a Task "collect facts"
      And the helper starts a Task "Collect  Facts"
      And the helper reports "r"
      And the Provider then answers "ok"
      When the user sends "go"
      Then request 3 carries a ToolResult starting with "error: task: this is the task you are already doing"

    @S-313
    Scenario: The Session cannot delegate the user's own request
      Given the Provider starts a Task "find the port"
      And the Provider then answers "ok"
      When the user sends "find the port"
      Then request 2 carries a ToolResult starting with "error: task: this is the task you are already doing"

    @S-314
    Scenario: An empty goal is an error
      Given the Provider starts a Task ""
      And the Provider then answers "ok"
      When the user sends "go"
      Then request 2 carries a ToolResult starting with "error: task: goal is empty"

    @S-315
    Scenario: With Tasks off the Tool does not exist
      Given Task mode is off
      And the Provider starts a Task "x"
      And the Provider then answers "ok"
      When the user sends "go"
      Then request 2 carries a ToolResult starting with "error: unknown tool task"

  Rule: One Esc interrupts the whole Chain (R-40)

    @S-316
    Scenario: Every level ends with a result when the user interrupts
      Given the Trace is recorded
      And the Provider starts a Task "outer"
      And the helper starts a Task "inner"
      And a Provider that has streamed "partial" and is still answering
      When the user interrupts the Turn
      Then the Trace has a task_end for every task_start

    @S-317
    Scenario: The interrupted levels report it, innermost first
      Given the Trace is recorded
      And the Provider starts a Task "outer"
      And the helper starts a Task "inner"
      And a Provider that has streamed "partial" and is still answering
      When the user interrupts the Turn
      Then the Trace holds 2 task_end records with the status "interrupted"

    @S-361
    Scenario: Esc stops a chain of 8 Tasks, every level reports it
      Given Task mode allows depth 8
      And the Trace is recorded
      And the Provider starts a chain of 8 nested Tasks
      And a Provider that has streamed "partial" and is still answering
      When the user interrupts the Turn
      Then the Trace holds 8 task_end records with the status "interrupted"

    @S-318
    Scenario: A second Esc aborts the Chain
      Given the Trace is recorded
      And the Provider starts a Task "outer"
      And the helper starts a Task "inner"
      And a Provider that has streamed "partial" and is still answering
      And the user interrupted the Turn
      When the user interrupts the Turn again
      Then the Trace holds 2 task_end records with the status "aborted"

    @S-319
    Scenario: The server is checked once after an aborted Chain
      Given a health check that fails
      And the Provider starts a Task "outer"
      And a Provider that has streamed "partial" and is still answering
      And the user interrupted the Turn
      When the user interrupts the Turn again
      Then the server-busy warning is shown once

    @S-322
    Scenario: Text sent while a Task runs reaches the Session after the Task, never the helper
      Given the Provider starts a Task "x"
      And a Provider that has streamed "partial" and is still answering and then will answer "ok"
      And the user submits "later"
      When the Provider finishes the rest of its answer
      Then the text "later" joined the Session after the TaskResult and never reached the helper

  Rule: Denials inside a Task end the Task and count once for its parent (R-9)

    @S-323
    Scenario: Two denied calls end the Task, the Session goes on
      Given approval is denied
      And the Provider starts a Task "x"
      And the helper calls "shell" for "echo hi" twice
      And the Provider then answers "ok"
      When the user sends "go"
      Then the TaskResult starts with "stopped: tool calls were denied" and the Turn ends with the answer "ok"

  Rule: The SessionFile and the Trace see a Task as designed (I-12, I-14)

    @S-325
    Scenario: The SessionFile replays to the Session history without the helper's Messages
      Given the Session is saved in a SessionFile
      And a file "notes.txt" with the text "port 8081"
      And the Provider starts a Task "x"
      And the helper calls "read_file" for "notes.txt" and then reports "r"
      And the Provider then answers "ok"
      When the user sends "go"
      Then replaying the SessionFile gives the Session history

    @S-326
    Scenario: The Trace tags the helper's records with the Task id
      Given the Trace is recorded
      And a file "notes.txt" with the text "port 8081"
      And the Provider starts a Task "x"
      And the helper calls "read_file" for "notes.txt" and then reports "r"
      And the Provider then answers "ok"
      When the user sends "go"
      Then the Trace records of the helper carry the Task id and the Session's do not

  Rule: /task runs the goal as a Task first (R-40)

    @S-327
    Scenario: The Session answers from the TaskResult
      Given the helper reports "r"
      And the Provider then answers "ok"
      When the user runs /task "collect facts"
      Then the Session history roles are user, assistant, tool, assistant

    @S-328
    Scenario: /task sent during a Turn waits and starts its own Turn
      Given a Provider that has streamed "partial" and is still answering
      And the helper reports "r"
      And the Provider then answers "ok"
      And the user runs /task "collect facts" while the Turn runs
      When the Provider finishes the rest of its answer
      Then the text "collect facts" started its own Turn and nothing was injected

    @S-330
    Scenario: /task with Tasks off is refused
      Given Task mode is off
      When the user runs /task "x"
      Then the command is refused because tasks are off
