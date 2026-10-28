Feature: Sessions
  Every Message of a Session is saved in a SessionFile as it happens, and a saved Session can be
  resumed with exactly the history it had (Domain I-6, R-20, R-21). Compaction records are covered
  by compaction.feature.

  Rule: The SessionFile replays to the live history (I-6)

    @S-121
    Scenario: A finished Turn is in the SessionFile
      Given the Session is saved in a SessionFile
      And a Provider that streams the answer "hi" and finishes normally
      When the user submits "hello"
      Then the SessionFile holds the user Message "hello" followed by the assistant Message "hi"

    @S-122
    Scenario: Stubs are replayed
      Given the Session is saved in a SessionFile
      And a file "big.txt" with 150 lines
      And a Provider that reads "big.txt" and answers "one", and then answers "two"
      When the user submits "read it"
      Then replaying the SessionFile gives the Session history

    @S-123
    Scenario: Shrunk arguments are replayed
      Given the Session is saved in a SessionFile
      And a Provider that writes "out.txt" with 150 lines and answers "one", and then answers "two"
      When the user submits "write it"
      Then replaying the SessionFile gives the Session history

    @S-124
    Scenario: An answer cut by the loop guard is replayed
      Given the Session is saved in a SessionFile
      And a Provider that streams a numbered list of 60 identical items and keeps answering
      When the user submits "what can you do"
      Then replaying the SessionFile gives the Session history

    @S-125
    Scenario: Clearing is recorded
      Given the Session is saved in a SessionFile
      And a Session with one finished Turn
      And the user cleared the Session
      And a Provider that streams the answer "again" and finishes normally
      When the user submits "next"
      Then replaying the SessionFile gives the Session history

    @S-126
    Scenario: A failing SessionFile does not end the Turn
      Given a SessionFile that cannot be written
      And a Provider that streams the answer "hi" and finishes normally
      When the user submits "hello"
      Then a warning starting with "the session is no longer saved" is shown

    @S-127
    Scenario: The Session history survives a failing SessionFile
      Given a SessionFile that cannot be written
      And a Provider that streams the answer "hi" and finishes normally
      When the user submits "hello"
      Then the Session history is the user Message "hello" followed by the assistant Message "hi"

    @S-164
    Scenario: Compaction is recorded
      Given the Session is saved in a SessionFile
      And a Session with one finished Turn that used a read_file Tool
      And a Provider that summarizes each WorkSpan as "ok"
      When the user compacts the Session
      Then replaying the SessionFile gives the compacted history

  Rule: A resumed Session has the saved history (R-20, R-21)

    @S-128
    Scenario: Resume restores the history
      Given a saved Session with two finished Turns
      When the user resumes it
      Then the Session history equals the saved one

    @S-129
    Scenario: A resumed Session continues with the saved Messages as the prefix
      Given a saved Session with two finished Turns
      And the user resumed it
      And a Provider that streams the answer "again" and finishes normally
      When the user submits "next"
      Then the request starts with the saved Messages followed by the user Message "next"

    @S-130
    Scenario: A torn last line is skipped
      Given a saved Session whose last line was cut in the middle
      When the user resumes it
      Then the Session history equals the saved one

    @S-131
    Scenario: A ToolCall without a ToolResult is cancelled on resume
      Given a saved Session whose last assistant Message has a ToolCall without a ToolResult
      When the user resumes it
      Then every ToolCall in the Session history has a ToolResult

    @S-132
    Scenario: The cancelled ToolResult is saved too
      Given a saved Session whose last assistant Message has a ToolCall without a ToolResult
      When the user resumes it
      Then the SessionFile ends with the ToolResult "cancelled by the user"

    @S-133
    Scenario: A resume waits for the running Turn
      Given a saved Session with two finished Turns
      And a Provider that is answering "slow"
      And the user submitted "go" and the Turn is running
      When the user resumes it
      Then the Turn ends first and the Session history then equals the saved one

    @S-134
    Scenario: Resuming does not save the loaded Messages again
      Given a saved Session with two finished Turns
      When the user resumes it
      Then the SessionFile gains no Message

    @S-135
    Scenario: A Session file with a damaged middle is refused
      Given a saved Session whose second line is not valid JSON
      When the user resumes it
      Then the resume fails naming line 2 and the Session history is empty

    @S-136
    Scenario: New Messages of a resumed Session go to the same file
      Given a saved Session with two finished Turns
      And the user resumed it
      And a Provider that streams the answer "again" and finishes normally
      When the user submits "next"
      Then replaying the SessionFile gives the Session history

  Rule: Saving can be switched off

    @S-137
    Scenario: No SessionFile without saving
      Given saving is switched off
      And a Provider that streams the answer "hi" and finishes normally
      When the user submits "hello"
      Then no SessionFile exists
