Feature: Prompt profiles
  The SystemPrompt comes in three Profiles: compact (the base prompt alone), standard and full,
  each adding instruction layers to the one before. The config picks the Profile; the user can
  choose another, and the choice belongs to the Session.

  Rule: A Profile adds instruction layers to the SystemPrompt (R-115, I-40)

    @S-700
    Scenario: Without a Profile the SystemPrompt is the compact base prompt
      When the user submits "Hi"
      Then the SystemPrompt sent to the Provider has no Profile layer

    @S-701
    Scenario: The standard Profile adds the standard layer
      Given the prompt Profile is "standard"
      When the user submits "Hi"
      Then the SystemPrompt sent to the Provider contains "Scope discipline"

    @S-702
    Scenario: The full Profile carries the standard and the full layers
      Given the prompt Profile is "full"
      When the user submits "Hi"
      Then the SystemPrompt sent to the Provider carries the standard and the full layers

    @S-703
    Scenario: A Profile switched between Turns applies to the next Turn
      Given the prompt Profile is "standard"
      And the user submitted "Hi"
      And the prompt Profile is "compact"
      When the user submits "Again"
      Then the SystemPrompt sent to the Provider has no Profile layer

    @S-704
    Scenario: An unknown Profile is refused with the known names
      When the user switches the prompt Profile to "huge"
      Then the switch is refused and the known Profiles are listed

  Rule: The config picks the Profile (R-116)

    @S-705
    Scenario: A model pattern wins over the Source and the global default
      Given the global config names the Profile "standard"
      And the global config names the Profile "full" for the Source "local"
      And the global config names the Profile "compact" for the models "*9b*"
      When swarg loads the configuration
      Then the config picks the Profile "compact" for the model "qwen-9b" on the Source "local"

    @S-706
    Scenario: The Source's Profile wins over the global default
      Given the global config names the Profile "standard"
      And the global config names the Profile "full" for the Source "local"
      When swarg loads the configuration
      Then the config picks the Profile "full" for the model "other" on the Source "local"

    @S-707
    Scenario: A config that names no Profile gives compact
      Given the project has no config of its own
      When swarg loads the configuration
      Then the config picks the Profile "compact" for the model "other" on the Source "local"

  Rule: The choice belongs to the Session (R-118)

    @S-708
    Scenario: The SessionFile replays the last chosen Profile
      Given the Session is saved in a SessionFile
      And the user chose the prompt Profile "standard"
      And the user chose the prompt Profile "full"
      When the SessionFile is replayed
      Then the replayed prompt Profile is "full"

    @S-709
    Scenario: A SessionFile without a choice leaves the Profile to the config
      Given the Session is saved in a SessionFile
      When the SessionFile is replayed
      Then the replayed prompt Profile is none
