Feature: Session usage
  Besides the money, the Session adds up the tokens its requests used: prompt, cached and
  completion, over every request (Turn steps, Tasks, Agents, Compaction summaries). The sums are
  kept in the SessionFile and a resumed Session continues with them.

  Rule: The tokens of each response are added to the Session usage (R-131)

    @S-730
    Scenario: A response adds its tokens to the Session usage
      Given a Provider that streams the answer "ok" and finishes normally
      And the reply used 120 prompt tokens, 80 of them cached, and 15 completion tokens
      When the user submits "hi"
      Then the Session usage is 120 prompt, 80 cached and 15 completion tokens

    @S-731
    Scenario: Tokens of a free response count though it costs nothing
      Given a Provider that streams the answer "ok" and finishes normally
      And the reply used 40 prompt tokens, 0 of them cached, and 5 completion tokens
      When the user submits "hi"
      Then the Session spend is 0 USD and the Session usage is 40 prompt, 0 cached and 5 completion tokens

    @S-732
    Scenario: A response without a usage report leaves the Session usage at zero
      Given the Session is saved in a SessionFile
      And a Provider that streams the answer "ok" and finishes normally
      When the user submits "hi"
      Then the Session usage is zero and the SessionFile holds no usage line

    @S-733
    Scenario: The tokens of every step of a Turn add up
      Given a Provider that streams the answer "one" and finishes normally
      And the reply used 100 prompt tokens, 0 of them cached, and 10 completion tokens
      And a Provider that streams the answer "two" and finishes normally
      And the reply used 150 prompt tokens, 100 of them cached, and 20 completion tokens
      And the user submitted "a"
      When the user submits "b"
      Then the Session usage is 250 prompt, 100 cached and 30 completion tokens

    @S-734
    Scenario: The tokens of a Task's requests count in the Session usage
      Given Task mode allows depth 2
      And the Provider starts a Task "look"
      And the reply used 10 prompt tokens, 0 of them cached, and 1 completion tokens
      And the helper reports "found"
      And the reply used 20 prompt tokens, 0 of them cached, and 2 completion tokens
      And the Provider then answers "ok"
      And the reply used 40 prompt tokens, 0 of them cached, and 4 completion tokens
      When the user sends "go"
      Then the Session usage is 70 prompt, 0 cached and 7 completion tokens

  Rule: A resumed Session continues with its usage (R-131)

    @S-735
    Scenario: Resuming restores the usage
      Given a saved Session with a usage of 300 prompt, 200 cached and 30 completion tokens
      And the user resumed it
      When the Agent has loaded it
      Then the Session usage is 300 prompt, 200 cached and 30 completion tokens

    @S-736
    Scenario: A resumed Session adds its new tokens to the saved usage
      Given a saved Session with a usage of 300 prompt, 200 cached and 30 completion tokens
      And a Provider that streams the answer "ok" and finishes normally
      And the reply used 50 prompt tokens, 0 of them cached, and 5 completion tokens
      And the user resumed it
      When the user submits "again"
      Then the Session usage and the replayed SessionFile usage are 350 prompt, 200 cached and 35 completion tokens

    @S-737
    Scenario: A SessionFile from before the usage lines resumes with zero usage
      Given a saved Session with a spend of 0.25 USD on the model "m"
      And the user resumed it
      When the Agent has loaded it
      Then the Session usage is zero
