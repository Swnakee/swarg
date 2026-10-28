Feature: Per-model context limit
  The ContextLimit follows the model: every model id may have its own limit.

  Rule: The limit of the chosen model is used (R-41)

    @S-331
    Scenario: Choosing a model with its own limit changes the meter
      Given a context limit of 1000 tokens
      And a context limit of 4000 tokens for the model "big"
      And a Provider that streams the answer "hi" and finishes normally
      When the user chooses the model "big"
      Then the meter limit is 4000

    @S-332
    Scenario: A model without its own limit uses the provider's
      Given a context limit of 1000 tokens
      And a context limit of 4000 tokens for the model "big"
      And a Provider that streams the answer "hi" and finishes normally
      And the user chose the model "other"
      When the user sends "hi"
      Then the meter limit is 1000

    @S-333
    Scenario: A resumed Session uses the limit of its last chosen model
      Given a context limit of 1000 tokens
      And a context limit of 4000 tokens for the model "big"
      And a saved Session whose last chosen model is "big"
      When the user resumes it
      Then the meter limit is 4000
