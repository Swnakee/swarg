Feature: Skills
  A Skill is a folder with a SKILL.md: a name and a description in the front matter, instructions
  in the body, optional files beside it. The model sees the enabled Skills as a short list and
  applies one with the skill Tool; the user can apply one directly or switch one off.

  Rule: The enabled Skills are listed in the SystemPrompt (R-80, R-81)

    @S-600
    Scenario: Two enabled Skills are listed with their descriptions
      Given a Skill "deploy" described as "ship a release" with the instructions "Run the release script."
      And a Skill "lint" described as "check the code style" with the instructions "Run the linter."
      When the user submits "hi"
      Then the SystemPrompt sent to the Provider contains "- deploy: ship a release"

    @S-604
    Scenario: A project Skill replaces a global Skill of the same name
      Given a global Skill "deploy" described as "the global one" with the instructions "global"
      And a Skill "deploy" described as "the project one" with the instructions "project"
      When the user submits "hi"
      Then the SystemPrompt sent to the Provider contains "- deploy: the project one"

    @S-607
    Scenario: Without Skills there is no list and no skill Tool
      Given a file "notes.txt" with the text "alpha"
      And a Provider that calls "read_file" for "notes.txt" and then answers "ok"
      When the user submits "hi"
      Then the request to the Provider offers no Tool named "skill"

    @S-608
    Scenario: A SKILL.md without a description is skipped with a warning
      Given a Skill "deploy" described as "ship a release" with the instructions "Run it."
      And a SKILL.md of "broken" without a description
      When the user submits "hi"
      Then a Skill warning mentioning "broken" is given

  Rule: Applying a Skill returns its instructions (R-82)

    @S-601
    Scenario: The skill Tool returns the body without the front matter
      Given a Skill "deploy" described as "ship a release" with the instructions "Run the release script."
      And a Provider that applies the Skill "deploy" and then answers "done"
      When the user submits "deploy it"
      Then the second request contains a ToolResult "Run the release script."

    @S-606
    Scenario: A file of the Skill folder is returned on request
      Given a Skill "deploy" described as "ship a release" with the instructions "See the checklist."
      And the Skill "deploy" has a file "checklist.md" with the text "- tag the release"
      And a Provider that asks the Skill "deploy" for the file "checklist.md" and then answers "done"
      When the user submits "deploy it"
      Then the second request contains a ToolResult "- tag the release"

    @S-602
    Scenario: An unknown Skill names the enabled ones
      Given a Skill "deploy" described as "ship a release" with the instructions "Run it."
      And a Provider that applies the Skill "teleport" and then answers "ok"
      When the user submits "go"
      Then the second request contains a ToolResult mentioning "enabled skills: deploy"

  Rule: A Skill can be applied by the user and switched off (R-83, R-84)

    @S-605
    Scenario: The user applies a Skill with a text
      Given a Skill "deploy" described as "ship a release" with the instructions "Run the release script."
      And a Provider that answers "shipped"
      When the user applies the Skill "deploy" with the text "ship it"
      Then the Session history roles are user, assistant, tool, assistant

    @S-603
    Scenario: A Skill disabled in the config is neither listed nor applicable
      Given a Skill "deploy" described as "ship a release" with the instructions "Run it."
      And the Skill "deploy" is disabled in the config
      And a Provider that applies the Skill "deploy" and then answers "ok"
      When the user submits "go"
      Then the second request contains a ToolResult mentioning "is disabled"

    @S-609
    Scenario: A Skill switched off by the user leaves the list
      Given a Skill "deploy" described as "ship a release" with the instructions "Run it."
      And a Skill "lint" described as "check the code style" with the instructions "Lint it."
      And the user switches the Skill "deploy" off
      When the user submits "hi"
      Then the SystemPrompt sent to the Provider does not contain "- deploy:"
