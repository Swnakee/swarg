Feature: Generation speed
  Every Step reports how fast the model generated its answer: completion tokens per second,
  counted from the first generated token (text or reasoning) to the end of the stream (prompt
  reading is not generation).

  Rule: The speed of a Step is its completion tokens over its generation time (R-29)

    @S-210
    Scenario: A streamed answer reports its speed
      Given a Provider that answers "Hi" with 40 completion tokens, the first text at 0 s and the end at 2 s
      When the user submits "q"
      Then the Generation speed of the Step is 20 tokens per second

    @S-211
    Scenario: Prompt reading is not counted
      Given a Provider that answers "Hi" with 40 completion tokens, the first text at 3 s and the end at 5 s
      When the user submits "q"
      Then the Generation speed of the Step is 20 tokens per second

    @S-212
    Scenario: A Step with only ToolCalls counts from the end of prompt reading
      Given a Provider that only calls a Tool with 30 completion tokens, reading the prompt until 2 s and ending at 4 s, and then answers "done"
      When the user submits "q"
      Then the Generation speed of the first Step is 15 tokens per second

    @S-213
    Scenario: A Step without a usage report has no speed
      Given a Provider that answers "Hi" without a usage report
      When the user submits "q"
      Then no Generation speed is reported

    @S-214
    Scenario: A failed Step has no speed
      Given a Provider that fails with a server error
      When the user submits "q"
      Then no Generation speed is reported

    @S-215
    Scenario: Every Step reports its own speed
      Given a Provider that only calls a Tool with 30 completion tokens, reading the prompt until 2 s and ending at 4 s, and then answers "done" with 20 completion tokens, the first text at 1 s and the end at 3 s
      When the user submits "q"
      Then the Steps report the speeds 15 and 10 tokens per second
