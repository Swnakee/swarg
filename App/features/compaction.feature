Feature: Compaction
  A Compaction replaces finished WorkSpans by one WorkSummary each, between Turns (Domain R-16,
  R-18). It is asked by the user or starts by itself when the context is nearly full.

  Rule: User Messages and final answers are never compacted (R-16)

    @S-147
    Scenario: Compaction keeps the main context verbatim
      Given a Session with two finished Turns that used Tools
      And a Provider that summarizes each WorkSpan as "read a.go, edited it"
      When the user compacts the Session
      Then every user Message and every final answer in the history is unchanged

    @S-148
    Scenario: A Turn without Tools has nothing to compact
      Given a Session with one finished Turn
      When the user compacts the Session
      Then no summary request was sent

    @S-149
    Scenario: A Compaction asked during a Turn waits for the Turn
      Given a Session with one finished Turn that used a read_file Tool
      And a Provider that is answering "slow"
      And the user submitted "go" and its answer is streaming
      When the user compacts the Session
      Then the summary request is sent only after the Turn request was fully read

  Rule: A WorkSpan is replaced by one WorkSummary

    @S-150
    Scenario: A WorkSpan becomes one summary Message
      Given a Session with one finished Turn that used three Tools
      And a Provider that summarizes each WorkSpan as "read a.go, edited it"
      When the user compacts the Session
      Then the WorkSpan is replaced by one WorkSummary containing "read a.go, edited it"

    @S-151
    Scenario: An empty summary falls back to the generated list
      Given a Session with one finished Turn that used a read_file and an edit_file Tool
      And a Provider that answers the summary request with nothing
      When the user compacts the Session
      Then the WorkSummary lists "read_file" and "edit_file"

    @S-152
    Scenario: A failing summary request falls back to the generated list
      Given a Session with one finished Turn that used a read_file Tool
      And a Provider whose summary requests fail
      When the user compacts the Session
      Then the WorkSummary lists "read_file" and a warning is shown

    @S-153
    Scenario: A summary longer than the WorkSpan is not used
      Given a Session with one finished Turn that used a read_file Tool
      And a Provider that summarizes each WorkSpan with a very long text
      When the user compacts the Session
      Then the WorkSummary is the generated list

    @S-154
    Scenario: An oversized WorkSpan is summarized in chunks
      Given a context limit of 2000 tokens
      And a Session with one finished Turn whose Tool results are larger than half the limit
      And a Provider that summarizes every chunk
      When the user compacts the Session
      Then more than one summary request was sent and the history holds one WorkSummary for the WorkSpan

    @S-155
    Scenario: A small WorkSpan is listed without a model call
      Given a Session with one finished Turn that used three small edits
      When the user compacts the Session
      Then the WorkSpan is replaced by a generated list and no summary request was sent

    @S-156
    Scenario: A WorkSpan that would not shrink is left alone
      Given a Session with one finished Turn that used a small read_file Tool
      When the user compacts the Session
      Then the Session history is unchanged

  Rule: Compaction never runs while a request is in flight (R-18)

    @S-157
    Scenario: Text submitted during a Compaction waits for it
      Given a Session with one finished Turn that used a read_file Tool
      And a Provider whose summary request is still running
      And the user started a Compaction
      When the user submits "next"
      Then the Turn request is sent only after the summary request was fully read

    @S-158
    Scenario: Interrupting a Compaction leaves the history unchanged
      Given a Session with one finished Turn that used a read_file Tool
      And a Provider whose summary request is still running
      And the user started a Compaction
      When the user interrupts the Turn
      Then the Session history is unchanged

  Rule: Auto-compaction runs between Turns when the context is nearly full

    @S-159 @R-23
    Scenario: Auto-compaction at 70 percent
      Given a context limit of 10000 tokens
      And auto-compaction at 70 percent
      And a Session with one finished Turn that used a read_file Tool
      And a Provider that summarizes each WorkSpan as "ok"
      And a Provider whose answers report 7000 tokens used
      When the user submits "next"
      Then the history holds one WorkSummary

    @S-160 @R-23
    Scenario: Just below the threshold nothing is compacted
      Given a context limit of 10000 tokens
      And auto-compaction at 70 percent
      And a Session with one finished Turn that used a read_file Tool
      And a Provider whose answers report 6999 tokens used
      When the user submits "next"
      Then no summary request was sent

    @S-161 @R-23
    Scenario: Auto-compaction stops after two failures in a row
      Given a context limit of 10000 tokens
      And auto-compaction at 70 percent
      And a Provider whose summary requests fail
      And a Provider whose next 3 Turns each read "a.go" and report 7000 tokens used
      When the user submits 3 Turns in a row
      Then exactly 2 summary requests were sent and a warning says auto-compaction is off

    @S-162 @R-23
    Scenario: A manual Compaction re-enables auto-compaction
      Given a context limit of 10000 tokens
      And auto-compaction at 70 percent
      And a Provider whose first 2 summary requests fail and the rest answer "ok"
      And a Provider whose next 4 Turns each read "a.go" and report 7000 tokens used
      And auto-compaction was switched off by two failures
      When the user compacts the Session
      Then the next Turn at 70 percent compacts again

    @S-163 @R-23
    Scenario: Auto-compaction runs before the next queued text
      Given a context limit of 10000 tokens
      And auto-compaction at 70 percent
      And a Provider that summarizes each WorkSpan as "ok"
      And a Provider whose next Turn reads "a.go" and then streams an answer reporting 7000 tokens used and "second answer" after it
      And the user submitted "first" and "second" while the answer was streaming
      When the Provider finishes the rest of its answer
      Then the Turn request is sent only after the summary request was fully read
