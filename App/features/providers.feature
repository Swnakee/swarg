Feature: Model source
  The user switches the model source (server) while the app runs. Requests use the new source
  from the next one on, never in the middle of a request; each source has its own model list and
  remembers the model last used on it; the SessionFile records the source, so a resumed Session
  continues on it. Errors of a source are clear and never carry the key.

  Rule: A switched source is used from the next request on (R-100, I-36)

    @S-660
    Scenario: Switching the source sends the next request to the new server with its model
      Given a second source "cloud" with the model "m2"
      And the second source answers "hello from the cloud"
      And the user switched to the second source
      When the user submits "hi"
      Then the request goes to the second source with the model "m2"

    @S-661
    Scenario: A switch during a Turn takes effect at the next Step, never mid-request
      Given a Provider that calls a held Tool
      And a second source "cloud" with the model "m2"
      And the second source answers "done"
      And the user submitted "go" and the Tool is running
      When the user switches to the second source
      Then the first request went to the first source and the second to the second source

    @S-662
    Scenario: The switched source starts with its last used model, else its configured one
      Given a second source "cloud" with the model "m2"
      And the model "m-used" was used before on the source "cloud"
      And the second source answers "ok"
      And the user switched to the second source
      When the user submits "hi"
      Then the request goes to the second source with the model "m-used"

    @S-663
    Scenario: A history larger than the new limit is compacted before the first request
      Given a Session with two finished Turns that used Tools
      And a second source "small" with the model "m2"
      And the second source has a context limit of 600 tokens
      And the second source answers "log" and then "ok"
      And the user switched to the second source
      When the user submits "more"
      Then the first request to the second source is a summary request and the Turn request follows

  Rule: Each source has its own model list and the Session remembers its source (R-101)

    @S-664
    Scenario: The model list shows only the active source's models
      Given a second source "cloud" with the model "m2"
      And the first source lists the models "a1, a2" and the second the models "b1"
      When the user switches to the second source
      Then the model list shows only "b1"

    @S-665
    Scenario: A resumed Session continues on its last source
      Given a second source "cloud" with the model "m2"
      And the second source answers "back"
      And a saved Session whose last source is the second one
      And the user resumed it on its recorded source
      When the user submits "again"
      Then the request goes to the second source with the model "m2"

    @S-666
    Scenario: An old SessionFile without a source resumes on the configured one
      Given a second source "cloud" with the model "m2"
      And a Provider that streams the answer "here" and finishes normally
      And a saved Session with two finished Turns
      And the user resumed it on its recorded source
      When the user submits "again"
      Then the request names the model "m"

  Rule: Source errors are clear and never carry the key (R-103, R-104, I-35)

    @S-667
    Scenario: A source without its key cannot be switched to, with the variable named
      Given a second source "cloud" with the model "m2"
      And the second source needs the key variable "T11_TEST_KEY"
      When the user switches to the second source
      Then the switch is refused naming "no key: set T11_TEST_KEY"

    @S-668
    Scenario: A rate-limited request waits once for a short Retry-After and succeeds
      Given a second source "cloud" with the model "m2"
      And the second source is rate limited once for 1 second and then answers "ok now"
      And the user switched to the second source
      When the user submits "hi"
      Then the last assistant Message is "ok now"

    @S-669
    Scenario: A bad key is a clear error without the key in it
      Given a second source "cloud" with the model "m2"
      And the second source has the key "sk-test-secret"
      And the second source refuses the key
      And the user switched to the second source
      When the user submits "hi"
      Then the Turn ends with an error naming "refused the API key" and not "sk-test-secret"
