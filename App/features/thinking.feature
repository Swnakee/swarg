Feature: Thinking mode
  The user switches the thinking mode of the model in the app. With it on, every request asks the
  model to think and uses the thinking sampling and a larger output limit; the SessionFile
  records every switch, so a resumed Session continues in its last mode.

  Rule: Requests follow the thinking mode from the next request on (R-60)

    @S-370
    Scenario: Thinking is off by default
      Given a Provider with the thinking presets
      When the user submits "hi"
      Then the request has thinking off with temperature 0.7 and at most 2048 tokens

    @S-371
    Scenario: The next Turn thinks after thinking was switched on
      Given a Provider with the thinking presets
      And the user switched thinking on
      When the user submits "hi"
      Then the request has thinking on with temperature 0.6 and at most 8192 tokens

    @S-372
    Scenario: A switch during a Turn is used from its next Step
      Given a Provider with the thinking presets
      And a Provider that calls a held Tool and then answers "done"
      And the user submitted "go" and the Tool is running
      When the user switches thinking on
      Then the first request has thinking off and the second thinking on

    @S-373
    Scenario: Switching to the mode in use changes nothing
      Given a Provider with the thinking presets
      And the Session is saved in a SessionFile
      When the user switches thinking off
      Then the SessionFile holds no thinking change

  Rule: A resumed Session continues in its last thinking mode (I-30)

    @S-374
    Scenario: Replaying the SessionFile gives the last thinking mode
      Given the Session is saved in a SessionFile
      And the user switched thinking on
      And the user switched thinking off
      And the user switched thinking on
      When the SessionFile is replayed
      Then the replayed thinking is on

    @S-375
    Scenario: A resumed Session thinks when it was thinking
      Given a Provider with the thinking presets
      And a saved Session whose thinking is on
      And the user resumed it
      When the user submits "again"
      Then the request has thinking on with temperature 0.6 and at most 8192 tokens

    @S-376
    Scenario: A SessionFile from before thinking resumes with thinking off
      Given a Provider with the thinking presets
      And a saved Session with two finished Turns
      And the user resumed it
      When the user submits "again"
      Then the request has thinking off with temperature 0.7 and at most 2048 tokens
