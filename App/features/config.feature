Feature: Global and project settings
  swarg reads the global config and, inside a project, the project's own `.swarg/config.json`
  over it. A project may add Skills and MCP Servers and change settings, but it can never lower
  safety, and an MCP Server it brings runs only after the user approved it once.

  Rule: The project config is layered over the global one (R-95)

    @S-650
    Scenario: A project setting wins over the global one for the same key
      Given the global config sets the max steps to 30
      And the project config sets the max steps to 7
      When swarg loads the configuration
      Then the effective max steps are 7

    @S-651
    Scenario: Project MCP Servers join the global ones by name
      Given the global config defines the MCP Server "g"
      And the project config defines the MCP Server "p"
      When swarg loads the configuration
      Then the configured MCP Servers are "g, p"

    @S-652
    Scenario: A project without its own config loads the global one unchanged
      Given the global config sets the max steps to 30
      And the project has no config of its own
      When swarg loads the configuration
      Then the effective max steps are 30

  Rule: A project file never lowers safety (R-96, I-33)

    @S-653
    Scenario: A project file cannot switch auto-approve on
      Given the project config switches auto-approve on
      When swarg loads the configuration
      Then auto-approve stays off and a warning names "tools.auto_approve"

    @S-654
    Scenario: A project file cannot trust an MCP Server
      Given the project config defines the MCP Server "p" as trusted
      When swarg loads the configuration
      Then the MCP Server "p" is loaded untrusted and a warning names "mcp.servers.p.trust"

    @S-658
    Scenario: Switching a Skill off in the project scope writes the project file
      Given the project has a config of its own
      When the user switches the Skill "deploy" off in the project scope
      Then the project config lists the Skill "deploy" as disabled

  Rule: A project MCP Server runs only after the user's approval (R-97, I-34)

    @S-655
    Scenario: A project MCP Server is not started before its approval
      Given the project config defines the MCP Server "p"
      When the MCP Servers start
      Then the MCP Server "p" starts as "needs approval"

    @S-656
    Scenario: An approved project MCP Server starts and offers its Tools
      Given the project config defines the MCP Server "p"
      And the user approved the MCP Server "p" for this project
      When the MCP Servers start
      Then the MCP Server "p" starts as "running"

    @S-657
    Scenario: A changed project MCP Server entry needs a new approval
      Given the project config defines the MCP Server "p"
      And the user approved the MCP Server "p" for this project
      And the project config changes the arguments of the MCP Server "p"
      When the MCP Servers start
      Then the MCP Server "p" starts as "needs approval"

    @S-659
    Scenario: A global MCP Server runs without approval
      Given the global config defines the MCP Server "g"
      When the MCP Servers start
      Then the MCP Server "g" starts as "running"
