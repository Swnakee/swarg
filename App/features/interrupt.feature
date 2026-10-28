Feature: Interrupting a Turn
  An interrupt stops the output at once, keeps what was shown, and never leaves the server stream unread.

  Rule: An interrupt keeps the partial answer

    @S-008
    Scenario: An interrupt keeps the partial answer marked interrupted
      Given a Provider that has streamed "Partial" and is still answering
      When the user interrupts the Turn
      Then the last assistant Message is "Partial" and is marked interrupted

    @S-009
    Scenario: An interrupt before any text stores no assistant Message
      Given a Provider that is answering but has streamed no text yet
      When the user interrupts the Turn
      Then the Session history is only the user Message

    @S-010
    Scenario: An interrupt while idle changes nothing
      Given a Session with one finished Turn
      When the user interrupts the Turn
      Then the Session history is unchanged

  Rule: The interrupted stream is drained

    @S-011
    Scenario: The Provider stream is read to its end after an interrupt
      Given a Provider that has streamed "Partial" and is still answering
      And the user interrupted the Turn
      When the Provider finishes the rest of its answer
      Then the Provider response is fully read

    @S-012
    Scenario: Text arriving after an interrupt is not shown
      Given a Provider that has streamed "Partial" and is still answering
      And the user interrupted the Turn
      When the Provider finishes the rest of its answer " and more"
      Then no output contains " and more"

    @S-013
    Scenario: A new Turn waits until the interrupted stream is drained
      Given a Provider that has streamed "Partial" and is still answering
      And the user interrupted the Turn
      And the user submitted "Next"
      When the Provider finishes the rest of its answer
      Then the second request is sent only after the first response was fully read

    @S-036
    Scenario: Queued messages still run after an interrupt
      Given a Provider that has streamed "Partial" and is still answering and then will answer "B"
      And the user submitted "Next"
      And the user interrupted the Turn
      When the Provider finishes the rest of its answer
      Then the last assistant Message is "B"
