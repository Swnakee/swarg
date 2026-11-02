Feature: SystemPrompt Blocks
  The SystemPrompt is assembled from Blocks that can be switched on and off.

  Rule: A disabled Block is absent from the next request

    @S-026 @R-4
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

    @S-028 @R-4
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

  Rule: Without SWARG.md the working directory's AGENTS.md, then CLAUDE.md, is the project rules file (R-135)

    @S-744 @R-135
    Scenario: AGENTS.md is used when there is no SWARG.md
      Given a rules file "AGENTS.md" with the text "Agents rule" in the working directory
      When the user submits "Hi"
      Then the SystemPrompt sent to the Provider contains "Agents rule"

    @S-745 @R-135
    Scenario: AGENTS.md wins over CLAUDE.md and the two do not stack
      Given a rules file "CLAUDE.md" with the text "Claude rule" in the working directory
      And a rules file "AGENTS.md" with the text "Agents rule" in the working directory
      When the user submits "Hi"
      Then the SystemPrompt sent to the Provider does not contain "Claude rule"

    @S-749 @R-135
    Scenario: SWARG.md wins over AGENTS.md
      Given a rules file "SWARG.md" with the text "Swarg rule" in the working directory
      And a rules file "AGENTS.md" with the text "Agents rule" in the working directory
      When the user submits "Hi"
      Then the SystemPrompt sent to the Provider does not contain "Agents rule"

    @S-746 @R-135
    Scenario: CLAUDE.md is used when only it exists
      Given a rules file "CLAUDE.md" with the text "Claude rule" in the working directory
      When the user submits "Hi"
      Then the SystemPrompt sent to the Provider contains "Claude rule"

  Rule: The env block says whether the working directory is in a git repository (R-136)

    @S-747 @R-136
    Scenario: Inside a git work tree
      Given the working directory is inside a git work tree
      When the user submits "Hi"
      Then the SystemPrompt sent to the Provider contains "Git repo: yes"

    @S-748 @R-136
    Scenario: Outside a git work tree
      Given a working directory without a SWARG.md
      When the user submits "Hi"
      Then the SystemPrompt sent to the Provider contains "Git repo: no"

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
