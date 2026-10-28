Feature: Web tools and tool call errors
  The model can search the web and read pages with approval; a call that misses its path says how to call.

  Rule: Web tools ask for Approval (R-45)

    @S-350
    Scenario: A denied web_fetch does not run
      Given approval is denied
      And a Provider that calls "web_fetch" for "https://example.org/" and then answers "ok"
      When the user submits "read the page"
      Then the second request contains a ToolResult starting with "denied by the user"

    @S-351
    Scenario: A web_fetch asks for Approval once
      Given approval is denied
      And a Provider that calls "web_fetch" for "https://example.org/" and then answers "ok"
      When the user submits "read the page"
      Then approval was asked 1 times

    @S-352
    Scenario: A denied web_search does not run
      Given approval is denied
      And a Provider that calls "web_search" for "swarg agent" and then answers "ok"
      When the user submits "search"
      Then the second request contains a ToolResult starting with "denied by the user"

  Rule: A missing path names the argument and shows the call (R-46)

    @S-353
    Scenario: write_file without a path
      Given approval is granted
      And a Provider that calls "write_file" with the arguments "{}" and then answers "ok"
      When the user submits "write"
      Then the second request contains a ToolResult mentioning "call write_file like"

    @S-354
    Scenario: edit_file without a path
      Given approval is granted
      And a Provider that calls "edit_file" with the arguments "{}" and then answers "ok"
      When the user submits "edit"
      Then the second request contains a ToolResult mentioning "call edit_file like"

    @S-344
    Scenario: read_file without a path
      Given a Provider that calls "read_file" with the arguments "{}" and then answers "ok"
      When the user submits "read"
      Then the second request contains a ToolResult mentioning "call read_file like"

    @S-345
    Scenario: grep without a pattern
      Given a Provider that calls "grep" and then answers "ok"
      When the user submits "search"
      Then the second request contains a ToolResult mentioning "call grep like"

    @S-346
    Scenario: shell with an empty command
      Given approval is granted
      And a Provider that calls "shell" for "" and then answers "ok"
      When the user submits "run"
      Then the second request contains a ToolResult mentioning "call shell like"

    @S-355
    Scenario: A call with a path is not affected
      Given approval is granted
      And a Provider that calls "write_file" for "n.txt" with "hi" and then answers "ok"
      When the user submits "write"
      Then the file "n.txt" contains "hi"

  Rule: web_fetch reaches only public http and https addresses (R-47)

    @S-356
    Scenario: A loopback address is refused
      Given approval is granted
      And a Provider that calls "web_fetch" for "http://127.0.0.1:9/" and then answers "ok"
      When the user submits "read it"
      Then the second request contains a ToolResult mentioning "refused"

    @S-357
    Scenario: A cloud metadata address is refused
      Given approval is granted
      And a Provider that calls "web_fetch" for "http://169.254.169.254/latest/meta-data/" and then answers "ok"
      When the user submits "read it"
      Then the second request contains a ToolResult mentioning "refused"

    @S-358
    Scenario: A file address is not a web page
      Given approval is granted
      And a Provider that calls "web_fetch" for "file:///etc/passwd" and then answers "ok"
      When the user submits "read it"
      Then the second request contains a ToolResult mentioning "http or https"
