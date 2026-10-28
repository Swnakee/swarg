Feature: Session Name
  A Session can be given a Name; the Name is stored in its SessionFile and lists show it instead
  of the first user Message (Domain R-32). A Name never changes the history (I-11).

  Rule: The Name is stored in the SessionFile (R-32)

    @S-250
    Scenario: Naming the Session is recorded
      Given the Session is saved in a SessionFile
      And a Session with one finished Turn
      When the user names the Session "api work"
      Then the SessionFile's Name is "api work"

    @S-251
    Scenario: Renaming keeps the last Name
      Given the Session is saved in a SessionFile
      And the user named the Session "first name"
      When the user names the Session "second name"
      Then the SessionFile's Name is "second name"

    @S-256
    Scenario: A blank Name is refused
      Given the Session is saved in a SessionFile
      And the user named the Session "kept"
      When the user names the Session "   "
      Then the naming is refused and the SessionFile's Name is "kept"

    @S-257
    Scenario: The Name survives a Clear
      Given the Session is saved in a SessionFile
      And a Session with one finished Turn
      And the user named the Session "kept"
      When the user clears the Session
      Then the SessionFile's Name is "kept"

  Rule: Lists show the Name, else the first user Message (R-32)

    @S-252
    Scenario: A Session without a Name is listed by its first user Message
      Given a saved Session with two finished Turns
      When the user lists the saved Sessions
      Then the Session is listed as "first"

    @S-253
    Scenario: A named Session is listed by its Name
      Given a saved Session with two finished Turns
      And that Session was named "api work"
      When the user lists the saved Sessions
      Then the Session is listed as "api work"

  Rule: A Name never changes the history (I-11)

    @S-254
    Scenario: A SessionFile with a Name and a record of an unknown kind loads with its history
      Given a saved Session with two finished Turns
      And that Session was named "api work"
      And its file holds a record of an unknown kind
      When the user resumes it
      Then the Session history equals the saved one

    @S-255
    Scenario: A Name record does not change the replayed history
      Given the Session is saved in a SessionFile
      And a Session with one finished Turn
      When the user names the Session "api work"
      Then replaying the SessionFile gives the Session history
