Feature: Context usage warnings
  The user is warned as the context fills up. Sending is never blocked.

  Rule: A warning appears at 80 percent and at 100 percent

    @S-021 @R-5
    Scenario: A warning appears when usage reaches 80 percent
      Given a context limit of 1000 tokens
      And a Provider whose answer reports 800 tokens used
      When the user submits "Hi"
      Then a context warning for 80 percent is shown

    @S-022 @R-5
    Scenario: No warning appears just below 80 percent
      Given a context limit of 1000 tokens
      And a Provider whose answer reports 799 tokens used
      When the user submits "Hi"
      Then no context warning is shown

    @S-023 @R-5
    Scenario: Sending still works at 100 percent
      Given a context limit of 1000 tokens
      And a Provider whose answers report 1000 tokens used and then answer "Still here"
      And the user submitted "Hi"
      When the user submits "Again"
      Then the last assistant Message is "Still here"

    @S-024 @R-5
    Scenario: The 80 percent warning is shown only once
      Given a context limit of 1000 tokens
      And a Provider whose answers report 800 and then 850 tokens used
      And the user submitted "Hi"
      When the user submits "Again"
      Then exactly one context warning is shown

    @S-025 @R-5
    Scenario: The warning is shown again after a clear
      Given a context limit of 1000 tokens
      And the context warning for 80 percent was shown
      And the user cleared the Session
      And a Provider whose answer reports 800 tokens used
      When the user submits "Hi"
      Then a context warning for 80 percent is shown again
