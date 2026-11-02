Feature: Background jobs

  Rule: A Tool can start a command as a BackgroundJob and the Turn goes on

    @S-076 @R-13
    Scenario: Starting a command in the background returns at once
      Given a Provider that starts a long-running command as a BackgroundJob and then answers "started"
      When the user submits "run it"
      Then the ToolResult of the shell ToolCall says the command started in the background as "bg1"

    @S-077 @R-14
    Scenario: The output of a BackgroundJob can be read in the same Turn
      Given a Provider that starts a command printing "ready" as a BackgroundJob, reads "bg1" and then answers "done"
      And the BackgroundJob is given time to finish before the Tool reads it
      When the user submits "run and read"
      Then the ToolResult of the read contains "ready"

    @S-078 @R-14
    Scenario: A BackgroundJob already read has no new output
      Given a Provider that starts a command printing "ready" as a BackgroundJob, reads "bg1" twice and then answers "done"
      And the BackgroundJob is given time to finish before the Tool reads it
      When the user submits "read twice"
      Then the ToolResult of the second read does not contain "ready"

    @S-079
    Scenario: Reading an unknown BackgroundJob is an error for the model
      Given a Provider that reads the BackgroundJob "bg9" and then answers "ok"
      When the user submits "read it"
      Then the ToolResult of the read is an error naming "bg9"

  Rule: A BackgroundJob outlives the Tool call and the Turn

    @S-080 @R-13
    Scenario: A BackgroundJob keeps running after the Turn ends
      Given a Provider that starts a long-running command as a BackgroundJob and then answers "started"
      When the user submits "run it"
      Then the BackgroundJob "bg1" is still running

    @S-081
    Scenario: The Tool kills a BackgroundJob after the user approves
      Given a Provider that starts a long-running command as a BackgroundJob, kills "bg1" and then answers "stopped"
      And the user approves every mutating ToolCall
      When the user submits "run and stop"
      Then the BackgroundJob "bg1" is not running

    @S-082
    Scenario: A denied kill leaves the BackgroundJob running
      Given a Provider that starts a long-running command as a BackgroundJob, kills "bg1" and then answers "ok"
      And the user denies the kill
      When the user submits "run and stop"
      Then the BackgroundJob "bg1" is still running

  Rule: The user can move the running Tool to the background

    @S-083 @I-5
    Scenario: The running Tool is moved to the background
      Given a Provider that calls the blocked detachable Tool "build" and then answers "carried on"
      And the user submitted "go" and the Tool "build" is running
      When the user moves the running Tool to the background
      Then the ToolResult of "build" says it moved to the background

    @S-084
    Scenario: The Turn continues after the Tool moved to the background
      Given a Provider that calls the blocked detachable Tool "build" and then answers "carried on"
      And the user submitted "go" and the Tool "build" is running
      When the user moves the running Tool to the background
      Then the Provider receives a second request containing the ToolResult of "build"

    @S-085 @I-5
    Scenario: Nothing is moved when no Tool is running
      Given an idle Agent
      When the user moves the running Tool to the background
      Then no Tool was moved to the background

    @S-086 @I-5
    Scenario: A read-only Tool is not moved to the background
      Given a Provider that calls the blocked read-only Tool "scan" and then answers "ok"
      And the user submitted "go" and the Tool "scan" is running
      When the user moves the running Tool to the background
      Then no Tool was moved to the background
