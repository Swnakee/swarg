Feature: Reasoning round trip
  The model's reasoning is kept and sent back with the requests of the running Turn, so a chat
  template that renders it keeps the server's prefix cache.

  Rule: Reasoning goes back with the running Turn's requests only (R-42)

    @S-334
    Scenario: The next Step of the Turn carries the reasoning
      Given a Provider that thinks "think" and then calls "list_dir" and then answers "ok"
      When the user sends "go"
      Then request 2 carries the reasoning "think" on its assistant message

    @S-335
    Scenario: Without reasoning nothing is sent
      Given a Provider that calls "list_dir" and then answers "ok"
      When the user sends "go"
      Then no request carries reasoning

    @S-336
    Scenario: An earlier Turn's reasoning is not sent
      Given a Provider that thinks "think" and then calls "list_dir" and then answers "ok"
      And a Provider that streams the answer "bye" and finishes normally
      And the user sent "go"
      When the user sends "next"
      Then request 3 carries no reasoning

    @S-338
    Scenario: The reasoning survives a resume
      Given the Session is saved in a SessionFile
      And a Provider that thinks "think" and then calls "list_dir" and then answers "ok"
      When the user sends "go"
      Then replaying the SessionFile gives the Session history
