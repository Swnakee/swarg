Feature: API keys
  The user writes API keys into one global Keys file, never into the terminal. A Key is read by
  its name: the environment variable wins, then the Keys file, then the old Key directory. The
  file is private to the user and a Key is never copied into another file.

  Rule: A Key is read from the Keys file by its name (R-106, R-107)

    @S-670
    Scenario: A Key in the Keys file is the key of its name
      Given the Keys file holds "T12_KEY=test-key-value-1"
      When the key of the name "T12_KEY" is read
      Then the key is "test-key-value-1" from the "keys.env"

    @S-671
    Scenario: The environment variable wins over the Keys file
      Given the Keys file holds "T12_KEY=test-key-value-1"
      And the environment variable "T12_KEY" is "test-key-env"
      When the key of the name "T12_KEY" is read
      Then the key is "test-key-env" from the "env"

    @S-672
    Scenario: An edit of the Keys file is used at once
      Given the Keys file holds "T12_KEY=test-key-value-1"
      And the key of the name "T12_KEY" is read
      When the Keys file is changed to "T12_KEY=test-key-value-2"
      Then the key of the name "T12_KEY" is now "test-key-value-2"

    @S-673
    Scenario: A bad line is skipped and the warning names only its number
      Given the Keys file holds "lower=test-key-secret"
      When the Keys file is checked
      Then the warning names "line 1" and shows no part of the line

    @S-674
    Scenario: A Keys file that others can read is ignored with a warning
      Given the Keys file readable by others holds "T12_KEY=test-key-value-1"
      When the key of the name "T12_KEY" is read
      Then no key is found for the name "T12_KEY" and the warning tells to chmod 600 the file

  Rule: The old Key directory is still read until the user moves its Keys (R-106)

    @S-678
    Scenario: A Key of the old Key directory is used when the Keys file lacks it
      Given the old Key directory holds "T12_KEY" as "test-key-old"
      When the key of the name "T12_KEY" is read
      Then the key is "test-key-old" from the "old keys dir"

    @S-679
    Scenario: The Keys file wins over the old Key directory
      Given the old Key directory holds "T12_KEY" as "test-key-old"
      And the Keys file holds "T12_KEY=test-key-value-1"
      When the key of the name "T12_KEY" is read
      Then the key is "test-key-value-1" from the "keys.env"

  Rule: The Keys file template is made on request and never replaces a file (R-110, I-37)

    @S-680
    Scenario: The template lists the Key names of the config and is private
      Given the config has the provider "p" needing the key variable "T12_KEY"
      When the Keys file is initialised
      Then the Keys file is private to the user and lists "T12_KEY" as a comment

    @S-681
    Scenario: An existing Keys file is not overwritten
      Given the Keys file holds "T12_KEY=test-key-value-1"
      When the Keys file is initialised
      Then the initialisation is refused and the Keys file is unchanged

    @S-677
    Scenario: Initialising the Keys file copies no Key anywhere
      Given the environment variable "T12_KEY" is "test-key-env"
      And a config file exists
      When the Keys file is initialised
      Then no file of the config dir holds "test-key-env"

  Rule: Other settings refer to a Key by name (R-106, R-107)

    @S-675
    Scenario: An MCP header resolves from the Keys file when the variable is unset
      Given the Keys file holds "T12_KEY=test-key-value-1"
      When the value "Bearer ${T12_KEY}" is expanded
      Then the expanded value is "Bearer test-key-value-1"

    @S-676
    Scenario: The users of a Key are the providers and MCP Servers that name it
      Given the config has the provider "p" needing the key variable "T12_KEY"
      And the config has the MCP Server "web" with the header "Authorization" set to "Bearer ${T12_KEY}"
      When the users of the Keys are listed
      Then the Key "T12_KEY" is used by "mcp web header Authorization, provider p"
