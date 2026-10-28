Feature: Session commands
  Clearing the history.

  Rule: Clear empties the history

    @S-018
    Scenario: Clear empties the history
      Given a Session with one finished Turn
      When the user clears the Session
      Then the Session history is empty

    @S-019
    Scenario: Clear on an empty Session is harmless
      Given an idle Session
      When the user clears the Session
      Then the Session history is empty

    @S-020
    Scenario: Clear is refused while a Turn runs
      Given a Provider that is answering "A"
      And the user submitted "first" and the Turn is running
      When the user clears the Session
      Then the clear is refused as busy
