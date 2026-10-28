Feature: A Turn streams an answer into the Session
  A Turn sends the Session history to the Provider and stores the answer.

  Rule: A finished answer is stored

    @S-001
    Scenario: A streamed answer is stored
      Given a Provider that streams the answer "Hello there" and finishes normally
      When the user submits "Hi"
      Then the Session history is the user Message "Hi" followed by the assistant Message "Hello there"

    @S-002
    Scenario: An answer with no text is not stored
      Given a Provider that streams no text and finishes normally
      When the user submits "Hi"
      Then the Session history is only the user Message "Hi"

    @S-003
    Scenario: A Provider that drops the connection ends the Turn with an error
      Given a Provider that drops the connection after the text "Hel"
      When the user submits "Hi"
      Then the Turn ends with an error and the Session history has no assistant Message

  Rule: An answer cut by the token limit is marked

    @S-004
    Scenario: An answer cut by length is marked truncated
      Given a Provider that streams the answer "Long text" and is cut by the token limit
      When the user submits "Tell me a lot"
      Then the last assistant Message is marked truncated

    @S-005
    Scenario: An answer that fits the limit is not marked
      Given a Provider that streams the answer "Short" and finishes normally
      When the user submits "Hi"
      Then the last assistant Message is not marked truncated

  Rule: A server error does not end the Session

    @S-006
    Scenario: The next question is answered after a server error
      Given a Provider that fails with a server error and then streams the answer "Recovered"
      And the user submitted "First" and the Turn ended with an error
      When the user submits "Second"
      Then the last assistant Message is "Recovered"

    @S-007
    Scenario: A server error keeps the user Message and stores no assistant Message
      Given a Provider that fails with a server error
      When the user submits "Hi"
      Then the Session history is only the user Message "Hi"
