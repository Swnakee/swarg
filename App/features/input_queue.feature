Feature: The InputQueue
  Messages submitted during a Turn never interrupt it; they are injected at the next Step of the running Turn.

  Rule: Queued input is injected at the next Step of the running Turn

    @S-014 @R-1
    Scenario: A message sent during an answer is sent after it
      Given a Provider that is answering "A" and then will answer "B"
      And the user submitted "first" and the Turn is running
      When the user submits "second"
      Then the Session history is first, "A", second, "B" in that order

    @S-015 @R-1
    Scenario: A queued message does not cut the running answer
      Given a Provider that is answering "A" and then will answer "B"
      And the user submitted "first" and the Turn is running
      When the user submits "second"
      Then the running answer is delivered in full

    @S-016
    Scenario: Several queued messages are sent in submission order
      Given a Provider that answers "A", "B" and "C" in turn
      And the user submitted "one" and the Turn is running
      And the user submitted "two"
      When the user submits "three"
      Then the Provider received the user Messages in the order one, two, three

  Rule: Blank input is ignored

    @S-017
    Scenario: Whitespace-only input starts no Turn
      Given an idle Session
      When the user submits "   "
      Then no request is sent to the Provider
