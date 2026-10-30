Feature: MCP Servers
  An MCP Server is an external program that offers Tools over the MCP protocol. swarg starts the
  configured servers, offers their Tools as mcp__<server>__<tool> and runs the calls; a failing
  server never stops swarg.

  Rule: The Tools of a running MCP Server are offered and run (R-85, R-86)

    @S-620
    Scenario: The Tools of a running server are offered under the server's name
      Given an MCP Server "demo" offering the tools "a" and "b"
      When the user submits "hi"
      Then the request to the Provider offers the Tools "mcp__demo__a, mcp__demo__b"

    @S-621
    Scenario: The text answer of an MCP Tool is the ToolResult
      Given an MCP Server "demo" offering the tool "a"
      And the tool "a" of the MCP Server answers "found it"
      And approval is granted
      And a Provider that calls "mcp__demo__a" and then answers "ok"
      When the user submits "go"
      Then the second request contains a ToolResult "found it"

    @S-622
    Scenario: A tool error of the server is an error ToolResult
      Given an MCP Server "demo" offering the tool "a"
      And the tool "a" of the MCP Server answers with an error "no such thing"
      And approval is granted
      And a Provider that calls "mcp__demo__a" and then answers "ok"
      When the user submits "go"
      Then the second request contains a ToolResult starting with "error: no such thing"

    @S-630
    Scenario: A tool list in several pages is complete
      Given an MCP Server "demo" offering the tools "a" and "b"
      And the MCP Server lists its tools one per page
      When the user submits "hi"
      Then the request to the Provider offers the Tools "mcp__demo__a, mcp__demo__b"

    @S-628
    Scenario: A long tool description is offered cut
      Given an MCP Server "demo" offering the tool "a"
      And the tool "a" of the MCP Server is described in 400 characters
      When the user submits "hi"
      Then the Tool "mcp__demo__a" is offered with a description of 203 characters

    @S-631
    Scenario: Only the allowlisted tools are offered
      Given an MCP Server "demo" offering the tools "a" and "b"
      And the MCP Server offers only the tool "a" to the model
      When the user submits "hi"
      Then the request to the Provider offers no Tool named "mcp__demo__b"

  Rule: A failing MCP Server never stops swarg (I-31)

    @S-623
    Scenario: A server that does not start is a warning and the built-in Tools work
      Given an MCP Server "demo" offering the tool "a"
      And the MCP Server refuses to start
      And a file "notes.txt" with the text "alpha"
      And a Provider that calls "read_file" for "notes.txt" and then answers "ok"
      When the user submits "show notes"
      Then a start warning names the MCP Server "demo"

    @S-629
    Scenario: A server speaking an unknown protocol version does not start
      Given an MCP Server "demo" offering the tool "a"
      And the MCP Server speaks protocol version "1999-01-01"
      When the user submits "hi"
      Then the MCP Server "demo" is reported failed

    @S-624
    Scenario: A server that exits answers its calls with the state
      Given an MCP Server "demo" offering the tool "a"
      And approval is granted
      And a Provider that calls "mcp__demo__a" and then answers "ok"
      And the MCP Server exits
      When the user submits "go"
      Then the second request contains a ToolResult mentioning "/mcp restart demo"

    @S-633
    Scenario: A restart brings the Tools of an exited server back
      Given an MCP Server "demo" offering the tool "a"
      And the tool "a" of the MCP Server answers "found it"
      And approval is granted
      And a Provider that calls "mcp__demo__a" and then answers "ok"
      And the MCP Server exits
      And the user restarts the MCP Server "demo"
      When the user submits "go"
      Then the second request contains a ToolResult "found it"

    @S-625
    Scenario: A call without an answer ends with the timeout
      Given an MCP Server "demo" offering the tool "a"
      And the MCP Server never answers a call
      And approval is granted
      And a Provider that calls "mcp__demo__a" and then answers "ok"
      When the user submits "go"
      Then the second request contains a ToolResult mentioning "no answer in 1s"

  Rule: Approval and visibility of MCP Tools follow the config (R-87, R-88)

    @S-627
    Scenario: A read-only hint of an untrusted server still asks for approval
      Given an MCP Server "demo" offering the tool "a"
      And the tool "a" of the MCP Server is marked read-only
      And approval is granted
      And a Provider that calls "mcp__demo__a" and then answers "ok"
      When the user submits "go"
      Then approval was asked 1 time

    @S-634
    Scenario: A read-only tool of a trusted server runs without approval
      Given an MCP Server "demo" offering the tool "a"
      And the tool "a" of the MCP Server is marked read-only
      And the MCP Server is trusted
      And approval is granted
      And a Provider that calls "mcp__demo__a" and then answers "ok"
      When the user submits "go"
      Then approval was asked 0 times

    @S-626
    Scenario: A server switched off by the user offers nothing
      Given an MCP Server "demo" offering the tool "a"
      And a file "notes.txt" with the text "alpha"
      And a Provider that calls "read_file" for "notes.txt" and then answers "ok"
      And the user switches the MCP Server "demo" off
      When the user submits "go"
      Then the request to the Provider offers no Tool named "mcp__demo__a"

  Rule: An MCP Server reached by URL works like a local one (R-91, R-92)

    @S-640
    Scenario: The Tools of a server reached by URL are offered under the server's name
      Given an MCP Server "docs" reached by URL offering the tools "a" and "b"
      When the user submits "hi"
      Then the request to the Provider offers the Tools "mcp__docs__a, mcp__docs__b"

    @S-641
    Scenario: An answer streamed by the server is the ToolResult
      Given an MCP Server "docs" reached by URL offering the tool "a"
      And the MCP Server streams its answers
      And the tool "a" of the MCP Server answers "found it"
      And approval is granted
      And a Provider that calls "mcp__docs__a" and then answers "ok"
      When the user submits "go"
      Then the second request contains a ToolResult "found it"

    @S-646
    Scenario: A header named in the config reaches the server with its value from the environment
      Given an MCP Server "docs" reached by URL offering the tool "a"
      And the MCP Server needs the bearer token "secret-value"
      And the environment variable "T9_TOKEN" holds "secret-value"
      And the MCP Server is configured with the header "Authorization" set to "Bearer ${T9_TOKEN}"
      And the tool "a" of the MCP Server answers "found it"
      And approval is granted
      And a Provider that calls "mcp__docs__a" and then answers "ok"
      When the user submits "go"
      Then the second request contains a ToolResult "found it"

  Rule: Failures of a server reached by URL are reported and never stop swarg (R-93, I-31)

    @S-642
    Scenario: A server that needs authentication is reported failed with the auth hint
      Given an MCP Server "docs" reached by URL offering the tool "a"
      And the MCP Server needs the bearer token "secret-value"
      When the user submits "hi"
      Then the MCP Server "docs" is reported failed because of "needs authentication"

    @S-643
    Scenario: A URL nobody answers is a start warning and the built-in Tools work
      Given an MCP Server "docs" reached by URL offering the tool "a"
      And nobody answers at the MCP Server's URL
      And a file "notes.txt" with the text "alpha"
      And a Provider that calls "read_file" for "notes.txt" and then answers "ok"
      When the user submits "show notes"
      Then a start warning names the MCP Server "docs"

    @S-644
    Scenario: A session the server forgot is renewed and the call answered
      Given an MCP Server "docs" reached by URL offering the tool "a"
      And the tool "a" of the MCP Server answers "found it"
      And approval is granted
      And a Provider that calls "mcp__docs__a" and then answers "ok"
      And the MCP Server forgets the session
      When the user submits "go"
      Then the second request contains a ToolResult "found it"

  Rule: A server reached by URL is reached safely (R-94, I-32)

    @S-645
    Scenario: A plain http URL to a public host is refused at start
      Given an MCP Server "docs" reached by URL offering the tool "a"
      And the MCP Server's URL is plain http to a public host
      When the user submits "hi"
      Then the MCP Server "docs" is reported failed because of "use https"

    @S-647
    Scenario: A header value never appears in the reported state of the server
      Given an MCP Server "docs" reached by URL offering the tool "a"
      And the MCP Server needs the bearer token "secret-value"
      And the MCP Server is configured with the header "Authorization" set to "Bearer wrong-value"
      When the user submits "hi"
      Then the reported state of the MCP Server "docs" does not mention "wrong-value"
