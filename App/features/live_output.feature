Feature: Live Tool output

  Rule: The output of a running Tool is reported while it runs

    @S-087
    Scenario: Output appears before the Tool ends
      Given a Provider that calls the Tool "build", which reports "compiling" while it runs, and then answers "ok"
      When the user submits "build it"
      Then the output "compiling" of the ToolCall was reported before the ToolCall ended

    @S-088
    Scenario: Reported output does not enter the history
      Given a Provider that calls the Tool "build", which reports "compiling" and returns "built", and then answers "ok"
      When the user submits "build it"
      Then the ToolResult of "build" is "built"

    @S-089
    Scenario: A Tool that reports nothing still ends normally
      Given a Provider that calls the Tool "quiet", which reports nothing, and then answers "ok"
      When the user submits "go"
      Then the Turn ended without reported output

    @S-090
    Scenario: A failing Tool keeps the output it reported
      Given a Provider that calls the Tool "build", which reports "compiling" and then fails, and then answers "ok"
      When the user submits "build it"
      Then the ToolResult of "build" is an error
