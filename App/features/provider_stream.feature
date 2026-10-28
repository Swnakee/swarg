Feature: The Provider hides stream details
  The core never sees SSE keepalives or the usage chunk.

  Rule: Server stream details do not leak into the Session

    @S-033
    Scenario: Prefill keepalive lines are not part of the answer
      Given a Provider whose stream starts with 3 keepalive lines and then the answer "Hello"
      When the user submits "Hi"
      Then the last assistant Message is "Hello"

    @S-034
    Scenario: The usage chunk is not part of the answer
      Given a Provider whose stream ends with a usage chunk after the answer "Hello"
      When the user submits "Hi"
      Then the last assistant Message is "Hello"

    @S-035
    Scenario: A malformed chunk ends the Turn with an error but not the Session
      Given a Provider whose stream contains a malformed chunk
      When the user submits "Hi"
      Then the Turn ends with an error and the Session history has no assistant Message
