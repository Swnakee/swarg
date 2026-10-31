Feature: Global agent types
  The agent types the owner chooses and their start prompts live in swarg's global config folder.
  A file there replaces the built-in of the same name, a project file replaces both. A built-in is
  edited by copying its full definition into the global folder; an existing file is never
  overwritten. A changed file applies without a restart. The save scope of /agents (global by
  default) and the menu are covered by unit tests of the ui and main packages, because they need
  the terminal. Each Source has a default prompt Profile (R-128, R-129).

  Rule: Where an agent type comes from (R-129)

    @S-723
    Scenario: A global file replaces the built-in of the same name
      Given the global agent file "explore" with the description "my explorer"
      When the agent types are loaded
      Then the agent type "explore" comes from the "global" file and is described "my explorer"

    @S-724
    Scenario: A project file replaces the global one
      Given the global agent file "explore" with the description "my explorer"
      And the project agent file "explore" with the description "project explorer"
      When the agent types are loaded
      Then the agent type "explore" comes from the "project" file and is described "project explorer"

  Rule: Editing a built-in copies it to the global folder (R-129)

    @S-725
    Scenario: The copy holds the full definition of the built-in
      When the user edits the built-in agent type "review"
      Then the global file "review" exists and loads back as the same description, tools and body

    @S-726
    Scenario: An existing file is never overwritten
      Given the global agent file "plan" with the description "my plan"
      When the user edits the built-in agent type "plan"
      Then the global file "plan" still has the description "my plan"

    @S-727
    Scenario: A name that is no built-in is refused
      When the user edits the built-in agent type "nonesuch"
      Then the edit is refused

    @S-728
    Scenario: A changed file applies without a restart
      Given the agent types are loaded
      And the global agent file "explore" with the description "tuned explorer"
      When the agent types are reloaded
      Then the agent type "explore" comes from the "global" file and is described "tuned explorer"

  Rule: Each Source has a default prompt Profile (R-129)

    @S-729
    Scenario: The OpenRouter preset defaults to standard and local presets stay compact
      When the default config is read
      Then the Profile of the Source "openrouter" is "standard" and of the Source "local" is "compact"
