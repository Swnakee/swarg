Feature: Sub-agents
  An agent is a helper with its own history, system prompt, model and tools. The Session hands it a
  job with the `agent` Tool and gets one report back. Agents may edit files, cannot start agents
  or Tasks, and the Tool is offered only while agent types are on offer (never under the compact
  Profile). Parallel runs, the slots, the file lock and Esc are covered by unit tests, because they
  need a model that answers several requests at once.

  Rule: An agent works apart and returns one report (R-120, R-127)

    @S-710
    Scenario: The agent's reads stay out of the Session
      Given the agent type "explore" with the tools "readonly" is on offer
      And a file "notes.txt" with the text "port 8081"
      And the Provider starts an agent "explore" with the goal "find the port"
      And the helper calls "read_file" for "notes.txt" and then reports "port 8081"
      And the Provider then answers "done, 8081"
      When the user sends "find the port"
      Then the final request carries no ToolResult of "read_file"

    @S-711
    Scenario: The agent runs under the system prompt of its type
      Given the agent type "explore" with the tools "readonly" is on offer
      And the Provider starts an agent "explore" with the goal "look"
      And the helper reports "nothing"
      And the Provider then answers "ok"
      When the user sends "go"
      Then every agent request starts with the system prompt "AGENT:explore"

    @S-712
    Scenario: The agent's report lists the files it changed
      Given the agent type "general" with the tools "*" is on offer
      And a file "notes.txt" with the text "port 8081"
      And the Provider starts an agent "general" with the goal "fix the port"
      And the helper edits "notes.txt" replacing "8081" with "9090" and then reports "fixed"
      And the Provider then answers "ok"
      When the user sends "go"
      Then the agent's report starts with "done; files: notes.txt"

    @S-713
    Scenario: An agent that runs out of steps reports what it did
      Given the agent type "general" with the tools "*" and 2 steps is on offer
      And a file "notes.txt" with the text "port 8081"
      And the Provider starts an agent "general" with the goal "x"
      And the helper calls "read_file" for "notes.txt" twice
      And the Provider then answers "ok"
      When the user sends "go"
      Then the agent's report starts with "stopped: step limit"

    @S-714
    Scenario: The agent asks the model of its type
      Given the agent type "general" with the tools "*" and the model "other-model" is on offer
      And the Provider starts an agent "general" with the goal "x"
      And the helper reports "r"
      And the Provider then answers "ok"
      When the user sends "go"
      Then every agent request names the model "other-model"

    @S-715
    Scenario: The cost of an agent's requests counts in the Session spend
      Given the agent type "general" with the tools "*" is on offer
      And the Provider starts an agent "general" with the goal "x"
      And the reply costs 0.01 USD
      And the helper reports "r"
      And the reply costs 0.02 USD
      And the Provider then answers "ok"
      And the reply costs 0.04 USD
      When the user sends "go"
      Then the Session spend is 0.07 USD

  Rule: An agent gets only the tools of its type and cannot delegate (R-121, I-41)

    @S-716
    Scenario: A read-only agent is not offered the editing tools
      Given the agent type "explore" with the tools "readonly" is on offer
      And the Provider starts an agent "explore" with the goal "look"
      And the helper reports "nothing"
      And the Provider then answers "ok"
      When the user sends "go"
      Then no agent request offers the tool "edit_file" and every one offers "read_file"

    @S-717
    Scenario: An agent is offered neither agents nor Tasks
      Given Task mode allows depth 3
      And the agent type "general" with the tools "*" is on offer
      And the Provider starts an agent "general" with the goal "look"
      And the helper reports "nothing"
      And the Provider then answers "ok"
      When the user sends "go"
      Then no agent request offers the tool "agent" or "task" and the first request offers both

    @S-718
    Scenario: A call outside the agent's tools is refused and nothing changes
      Given the agent type "explore" with the tools "readonly" is on offer
      And a file "notes.txt" with the text "port 8081"
      And the Provider starts an agent "explore" with the goal "look"
      And the helper edits "notes.txt" replacing "8081" with "9090" and then reports "tried"
      And the Provider then answers "ok"
      When the user sends "go"
      Then the file "notes.txt" is unchanged

  Rule: The Tool is offered only with agent types on offer (R-123, I-40)

    @S-719
    Scenario: With no agent type on offer the Tool is not offered
      Given no agent type is on offer
      And a Provider that streams the answer "ok" and finishes normally
      When the user sends "hi"
      Then the first request does not offer the tool "agent"

    @S-720
    Scenario: With agent types on offer the Tool is offered
      Given the agent type "explore" with the tools "readonly" is on offer
      And a Provider that streams the answer "ok" and finishes normally
      When the user sends "hi"
      Then the first request offers the tool "agent"

    @S-721
    Scenario: An unknown type is refused with the known types
      Given the agent type "explore" with the tools "readonly" is on offer
      And the Provider starts an agent "nope" with the goal "x"
      And the Provider then answers "ok"
      When the user sends "go"
      Then the agent's report starts with "error: agent: unknown type"

    @S-722
    Scenario: An empty goal is refused without a model request
      Given the agent type "explore" with the tools "readonly" is on offer
      And the Provider starts an agent "explore" with the goal " "
      And the Provider then answers "ok"
      When the user sends "go"
      Then the agent's report starts with "error: agent: goal is empty" and no agent request was made
