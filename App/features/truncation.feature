Feature: Truncated prompt
  A server whose context window is smaller than the configured ContextLimit cuts the prompt
  silently (Ollama: OLLAMA_CONTEXT_LENGTH). The usage it reports then shows far fewer prompt
  tokens than were sent; the user is told once, with the setting to change.

  Rule: A server that reads less than half of the prompt is reported once (R-65)

    @S-379
    Scenario: A server that reads only part of the prompt is reported
      Given a Provider that reports 1000 prompt tokens
      When the user submits a message of 30000 characters
      Then a warning starting with "the server read" is shown

    @S-380
    Scenario: A full prompt reading is not reported
      Given a Provider that reports 9000 prompt tokens
      When the user submits a message of 30000 characters
      Then no warning is shown
