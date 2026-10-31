Feature: Spend
  A paid model server reports what each response cost. The Session adds the costs up, per model,
  keeps the sum in the SessionFile and warns once when it passes a level the user set. It never
  stops a Turn: a limit is for the server to enforce, swarg only shows and warns.

  Rule: The cost of each response is added to the Session spend (R-111)

    @S-690
    Scenario: A response with a cost raises the Session spend
      Given a Provider that streams the answer "ok" and finishes normally
      And the reply costs 0.002 USD
      When the user submits "hi"
      Then the Session spend is 0.002 USD

    @S-691
    Scenario: A response without a cost leaves the Session spend at zero
      Given the Session is saved in a SessionFile
      And a Provider that streams the answer "ok" and finishes normally
      When the user submits "hi"
      Then the Session spend is 0 USD and the SessionFile holds no spend line

    @S-692
    Scenario: The spend is kept per model
      Given a Provider that streams the answer "one" and finishes normally
      And the reply costs 0.25 USD
      And a Provider that streams the answer "two" and finishes normally
      And the reply costs 0.5 USD
      And the user submitted "a"
      And the user chose the model "other"
      When the user submits "b"
      Then the Session spend is 0.25 USD on the model "m" and 0.5 USD on the model "other"

    @S-698
    Scenario: The cost of a Task's requests counts in the Session spend
      Given Task mode allows depth 2
      And the Provider starts a Task "look"
      And the reply costs 0.01 USD
      And the helper reports "found"
      And the reply costs 0.02 USD
      And the Provider then answers "ok"
      And the reply costs 0.04 USD
      When the user sends "go"
      Then the Session spend is 0.07 USD

  Rule: A resumed Session continues with its spend (R-111)

    @S-693
    Scenario: Resuming restores the spend
      Given a saved Session with a spend of 0.25 USD on the model "m"
      And the user resumed it
      When the Agent has loaded it
      Then the Session spend is 0.25 USD

    @S-694
    Scenario: A resumed Session adds its new cost to the saved spend
      Given a saved Session with a spend of 0.25 USD on the model "m"
      And a Provider that streams the answer "ok" and finishes normally
      And the reply costs 0.05 USD
      And the user resumed it
      When the user submits "again"
      Then the Session spend and the replayed SessionFile spend are 0.3 USD

  Rule: The user is warned once when the spend reaches the warning level, and the Turn goes on (R-114, I-39)

    @S-695
    Scenario: Crossing the level warns once
      Given the warning level is 0.03 USD
      And a Provider that streams the answer "one" and finishes normally
      And the reply costs 0.02 USD
      And a Provider that streams the answer "two" and finishes normally
      And the reply costs 0.02 USD
      And a Provider that streams the answer "three" and finishes normally
      And the reply costs 0.02 USD
      And the user submitted "a"
      And the user submitted "b"
      When the user submits "c"
      Then the user was warned about the spend once and the Turn answered "three"

    @S-696
    Scenario: Reaching exactly the level warns
      Given the warning level is 0.05 USD
      And a Provider that streams the answer "ok" and finishes normally
      And the reply costs 0.05 USD
      When the user submits "hi"
      Then the user was warned about the spend once

    @S-697
    Scenario: Without a warning level nothing is said however much is spent
      Given a Provider that streams the answer "ok" and finishes normally
      And the reply costs 500 USD
      When the user submits "hi"
      Then the user was not warned about the spend

    @S-699
    Scenario: A resumed Session already over the level is not warned again
      Given the warning level is 0.1 USD
      And a saved Session with a spend of 0.25 USD on the model "m"
      And a Provider that streams the answer "ok" and finishes normally
      And the reply costs 0.05 USD
      And the user resumed it
      When the user submits "again"
      Then the user was not warned about the spend
