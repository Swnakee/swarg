Feature: SystemPrompt Blocks
  The SystemPrompt is assembled from Blocks that can be switched on and off.

  Rule: A disabled Block is absent from the next request

    @S-026
    Scenario: A disabled Block is absent from the next request
      Given a global Block "10-style" with the text "Be terse"
      And the Block "style" is off
      When the user submits "Hi"
      Then the SystemPrompt sent to the Provider does not contain "Be terse"

    @S-027
    Scenario: An enabled Block is present in the request
      Given a global Block "10-style" with the text "Be terse"
      When the user submits "Hi"
      Then the SystemPrompt sent to the Provider contains "Be terse"

    @S-028
    Scenario: A Block switched off between Turns applies to the next Turn
      Given a global Block "10-style" with the text "Be terse"
      And the user submitted "Hi"
      And the Block "style" is switched off
      When the user submits "Again"
      Then the SystemPrompt sent to the Provider does not contain "Be terse"

    @S-029
    Scenario: Switching an unknown Block is refused with the known names
      Given a global Block "10-style" with the text "Be terse"
      When the user switches the Block "nope" off
      Then the switch is refused and the known Block names are listed

  Rule: SWARG.md is read from the working directory only

    @S-030
    Scenario: SWARG.md from the working directory is in the SystemPrompt
      Given a SWARG.md with the text "Project rule" in the working directory
      When the user submits "Hi"
      Then the SystemPrompt sent to the Provider contains "Project rule"

    @S-031
    Scenario: SWARG.md from the parent folder is not in the SystemPrompt
      Given a SWARG.md with the text "Parent rule" in the parent of the working directory
      When the user submits "Hi"
      Then the SystemPrompt sent to the Provider does not contain "Parent rule"

    @S-032
    Scenario: A working directory without SWARG.md still gets the base SystemPrompt
      Given a working directory without a SWARG.md
      When the user submits "Hi"
      Then the SystemPrompt sent to the Provider contains the base Block

  Rule: A file named after a built-in Block in the blocks folder replaces that Block's text

    @S-362
    Scenario: A base override file replaces the base Block
      Given a global Block "base" with the text "Custom base rules"
      When the user submits "Hi"
      Then the SystemPrompt sent to the Provider contains "Custom base rules" instead of the base Block

    @S-363
    Scenario: A tool override file replaces that tool's Block
      Given Task mode allows depth 1
      And a global Block "tool-task" with the text "task: custom description"
      When the user submits "Hi"
      Then the SystemPrompt sent to the Provider contains "task: custom description"

    @S-364
    Scenario: An override file for an unknown Block is ignored
      Given a global Block "tool-nope" with the text "Nope text"
      When the user submits "Hi"
      Then the SystemPrompt sent to the Provider does not contain "Nope text"
