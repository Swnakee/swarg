Feature: Prompt profiles
  The SystemPrompt comes in three Profiles: low (the base prompt alone), medium and high,
  each adding instruction layers to the one before. The config picks the Profile; the user can
  choose another, and the choice belongs to the Session.

  Rule: A Profile adds instruction layers to the SystemPrompt (R-115, I-40)

    @S-700
    Scenario: Without a Profile the SystemPrompt is the low base prompt
      When the user submits "Hi"
      Then the SystemPrompt sent to the Provider has no Profile layer

    @S-701
    Scenario: The medium Profile adds the medium layer
      Given the prompt Profile is "medium"
      When the user submits "Hi"
      Then the SystemPrompt sent to the Provider contains "Scope discipline"

    @S-702
    Scenario: The high Profile carries the medium and the high layers
      Given the prompt Profile is "high"
      When the user submits "Hi"
      Then the SystemPrompt sent to the Provider carries the medium and the high layers

    @S-703
    Scenario: A Profile switched between Turns applies to the next Turn
      Given the prompt Profile is "medium"
      And the user submitted "Hi"
      And the prompt Profile is "low"
      When the user submits "Again"
      Then the SystemPrompt sent to the Provider has no Profile layer

    @S-704
    Scenario: An unknown Profile is refused with the known names
      When the user switches the prompt Profile to "huge"
      Then the switch is refused and the known Profiles are listed

  Rule: The config picks the Profile (R-116)

    @S-705
    Scenario: A model pattern wins over the Source and the global default
      Given the global config names the Profile "medium"
      And the global config names the Profile "high" for the Source "local"
      And the global config names the Profile "low" for the models "*9b*"
      When swarg loads the configuration
      Then the config picks the Profile "low" for the model "qwen-9b" on the Source "local"

    @S-706
    Scenario: The Source's Profile wins over the global default
      Given the global config names the Profile "medium"
      And the global config names the Profile "high" for the Source "local"
      When swarg loads the configuration
      Then the config picks the Profile "high" for the model "other" on the Source "local"

    @S-707
    Scenario: A config that names no Profile gives low
      Given the project has no config of its own
      When swarg loads the configuration
      Then the config picks the Profile "low" for the model "other" on the Source "local"

  Rule: The choice belongs to the Session (R-118)

    @S-708
    Scenario: The SessionFile replays the last chosen Profile
      Given the Session is saved in a SessionFile
      And the user chose the prompt Profile "medium"
      And the user chose the prompt Profile "high"
      When the SessionFile is replayed
      Then the replayed prompt Profile is "high"

    @S-709
    Scenario: A SessionFile without a choice leaves the Profile to the config
      Given the Session is saved in a SessionFile
      When the SessionFile is replayed
      Then the replayed prompt Profile is none

  Rule: The old Profile names still work (R-134)

    @S-741
    Scenario: A config that names an old Profile picks the new one
      Given the global config names the Profile "standard"
      When swarg loads the configuration
      Then the config picks the Profile "medium" for the model "other" on the Source "local"

    @S-742
    Scenario: The user switches to an old Profile name
      When the user switches the prompt Profile to "full"
      Then the prompt Profile in force is "high"

    @S-743
    Scenario: A name that was never a Profile is still refused
      When the user switches the prompt Profile to "mega"
      Then the switch is refused and the known Profiles are listed
