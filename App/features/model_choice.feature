Feature: Model choice
  The user chooses the model in the app. Requests use it from the next one on, also in the
  middle of a Turn; the SessionFile records every choice, so a resumed Session continues with
  its last chosen model.

  Rule: The chosen model is used from the next request on (R-30, I-9)

    @S-200
    Scenario: The next Turn uses the chosen model
      Given the user chose the model "big"
      When the user submits "hi"
      Then the request names the model "big"

    @S-201
    Scenario: A model chosen during a Turn is used from its next Step
      Given a Provider that calls a held Tool and then answers "done"
      And the user submitted "go" and the Tool is running
      When the user chooses the model "big"
      Then the first request names the model "m" and the second the model "big"

    @S-203
    Scenario: Choosing the model in use changes nothing
      Given the Session is saved in a SessionFile
      When the user chooses the model "m"
      Then the SessionFile holds no model change

    @S-204
    Scenario: A blank model id is refused
      Given the user chose the model "big"
      When the user chooses the model "   "
      Then the choice is refused and the model stays "big"

  Rule: The SessionFile records every Model choice (R-30, I-9)

    @S-202
    Scenario: Replaying the SessionFile gives the last chosen model
      Given the Session is saved in a SessionFile
      And the user chose the model "big"
      And the user chose the model "small"
      When the SessionFile is replayed
      Then the replayed model is "small"

    @S-205
    Scenario: A resumed Session continues with its last chosen model
      Given a saved Session whose last chosen model is "big"
      And the user resumed it
      When the user submits "again"
      Then the request names the model "big"

    @S-206
    Scenario: Resuming a Session records no model change
      Given a saved Session whose last chosen model is "big"
      When the user resumes it
      Then the SessionFile holds exactly one model change

    @S-207
    Scenario: A SessionFile from before model choices resumes with its starting model
      Given the app starts with the model "other"
      And a saved Session with two finished Turns
      And the user resumed it
      When the user submits "again"
      Then the request names the model "m"
