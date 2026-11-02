Feature: Abort

  Rule: The first interrupt is soft

    @S-067 @R-6
    Scenario: A soft interrupt cancels a running Tool
      Given a Provider that calls a blocked Tool
      And the user submitted "go" and the Tool is running
      When the user interrupts the Turn
      Then the ToolResult is "cancelled by the user"

    @S-068
    Scenario: A soft interrupt does not disconnect the Provider
      Given a Provider that has streamed "Partial" and is still answering
      When the user interrupts the Turn
      Then the Provider connection stays open

  Rule: The second interrupt aborts

    @S-069
    Scenario: A second interrupt disconnects the Provider at once
      Given a Provider that has streamed "Partial" and is still answering
      And the user interrupted the Turn
      When the user interrupts the Turn again
      Then the Provider connection is closed

    @S-070
    Scenario: An abort frees the agent without waiting for the Provider
      Given a Provider that has streamed "Partial" and is still answering and then will answer "B"
      And the user submitted "Next"
      And the user interrupted the Turn
      When the user interrupts the Turn again
      Then the next request is sent before the first response is read to its end

    @S-071
    Scenario: A dead server is reported after an abort
      Given a health check that fails
      And a Provider that has streamed "Partial" and is still answering
      And the user interrupted the Turn
      When the user interrupts the Turn again
      Then a warning starting with "model server busy or not responding" is shown

    @S-072
    Scenario: A healthy server gives no warning after an abort
      Given a health check that succeeds
      And a Provider that has streamed "Partial" and is still answering
      And the user interrupted the Turn
      When the user interrupts the Turn again
      Then no warning is shown

    @S-073
    Scenario: An abort while idle changes nothing
      Given a Session with one finished Turn
      When the user aborts the Turn
      Then the Session history is unchanged

    @S-074
    Scenario: Queued messages run after an abort
      Given a Provider that has streamed "Partial" and is still answering and then will answer "B"
      And the user submitted "Next"
      And the user interrupted the Turn
      When the user interrupts the Turn again
      Then the last assistant Message is "B"
