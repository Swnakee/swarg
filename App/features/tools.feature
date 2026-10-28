Feature: Tools
  The Provider can call Tools; their ToolResults go back into the Session.

  Rule: A ToolCall result returns to the Provider

    @S-037
    Scenario: A read-only Tool result is sent back
      Given a file "notes.txt" with the text "alpha"
      And a Provider that calls the Tool "read_file" for "notes.txt" and then answers "done"
      When the user submits "show notes"
      Then the second request to the Provider contains a ToolResult with the text "alpha"

    @S-038
    Scenario: A Turn with several Steps stores every Message in order
      Given a Provider that calls "list_dir" and then "read_file" and then answers "done"
      When the user submits "explore"
      Then the Session history roles are user, assistant, tool, assistant, tool, assistant

    @S-039
    Scenario: A Tool that is disabled in the config is not offered
      Given the Tool "shell" is disabled
      When the user submits "hi"
      Then the request to the Provider offers no Tool named "shell"

  Rule: Wrong ToolCalls return an error to the Provider

    @S-040
    Scenario: An unknown Tool name returns an error ToolResult
      Given a Provider that calls the Tool "teleport" and then answers "ok"
      When the user submits "go"
      Then the second request contains a ToolResult starting with "error: unknown tool"

    @S-041
    Scenario: Invalid JSON arguments return an error ToolResult
      Given a Provider that calls "read_file" with the arguments "{not json" and then answers "ok"
      When the user submits "go"
      Then the second request contains a ToolResult starting with "error:"

    @S-042
    Scenario: A missing required argument names the argument
      Given a Provider that calls "read_file" with the arguments "{}" and then answers "ok"
      When the user submits "go"
      Then the second request contains a ToolResult mentioning "path"

    @S-043
    Scenario: A wrong ToolCall does not end the Turn
      Given a Provider that calls the Tool "teleport" and then answers "ok"
      When the user submits "go"
      Then the last assistant Message is "ok"

  Rule: Tools stay inside the project folder

    @S-044
    Scenario: A path outside the project folder is refused
      Given a Provider that calls "read_file" for "../secret.txt" and then answers "ok"
      When the user submits "go"
      Then the second request contains a ToolResult mentioning "outside"

    @S-045
    Scenario: A path inside the project folder is allowed
      Given a file "sub/a.txt" with the text "inner"
      And a Provider that calls "read_file" for "sub/a.txt" and then answers "ok"
      When the user submits "go"
      Then the second request contains a ToolResult with the text "inner"

    @S-046
    Scenario: A symlink leading outside the project folder is refused
      Given a symlink "link" to a file outside the project folder
      And a Provider that calls "read_file" for "link" and then answers "ok"
      When the user submits "go"
      Then the second request contains a ToolResult mentioning "outside"

  Rule: A path written from the root means the project folder (R-25)

    @S-166
    Scenario: A path with a leading separator names a file of the project folder
      Given a file "words.txt" with the text "alpha"
      And a Provider that calls "read_file" for "/words.txt" and then answers "ok"
      When the user submits "go"
      Then the second request contains a ToolResult with the text "alpha"

    @S-167
    Scenario: A nested path from the root names a file in a subfolder of the project
      Given a file "config/settings.yaml" with the text "debug: true"
      And a Provider that calls "read_file" for "/config/settings.yaml" and then answers "ok"
      When the user submits "go"
      Then the second request contains a ToolResult with the text "debug: true"

    @S-168
    Scenario: A root-style path climbing out of the project folder is refused
      Given a Provider that calls "read_file" for "/../etc/passwd" and then answers "ok"
      When the user submits "go"
      Then the second request contains a ToolResult mentioning "outside"

  Rule: edit_file replaces one exact fragment

    @S-047
    Scenario: A unique fragment is replaced
      Given a file "a.txt" with the text "one two"
      And approval is granted
      And a Provider that calls "edit_file" replacing "two" with "2" in "a.txt" and then answers "ok"
      When the user submits "edit"
      Then the file "a.txt" contains "one 2"

    @S-048
    Scenario: A fragment that occurs twice is refused
      Given a file "a.txt" with the text "x x"
      And approval is granted
      And a Provider that calls "edit_file" replacing "x" with "y" in "a.txt" and then answers "ok"
      When the user submits "edit"
      Then the file "a.txt" is unchanged

    @S-049
    Scenario: Line endings of a CRLF file are kept
      Given a file "w.txt" with Windows line endings and the lines "a" and "b"
      And approval is granted
      And a Provider that calls "edit_file" replacing "b" with "c" in "w.txt" and then answers "ok"
      When the user submits "edit"
      Then the file "w.txt" still has Windows line endings
