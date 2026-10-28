Feature: Loop guard
  A small model can fall into endless repetition of a list item. The agent stops the answer when
  it starts repeating itself (Domain R-22).

  Rule: An answer that starts repeating itself is stopped

    @S-115
    Scenario: A list that repeats one item is cut after the first copy
      Given a Provider that streams a numbered list of 60 identical items and keeps answering
      When the user submits "what can you do"
      Then the last assistant Message keeps one copy of the item and is marked truncated

    @S-116
    Scenario: The repeating request is disconnected
      Given a Provider that streams a numbered list of 60 identical items and keeps answering
      When the user submits "what can you do"
      Then the Provider connection is closed

    @S-117
    Scenario: The user is told why the answer stopped
      Given a Provider that streams a numbered list of 60 identical items and keeps answering
      When the user submits "what can you do"
      Then a warning "stopped: the answer started repeating itself" is shown

    @S-118
    Scenario: The next Turn works after a stop
      Given a Provider that streams a numbered list of 60 identical items and keeps answering
      And a Provider that streams the answer "fine" and finishes normally
      And the user submitted "what can you do"
      When the user submits "again"
      Then the last assistant Message is "fine"

    @S-119
    Scenario: Three copies of an item are not a loop
      Given a Provider that streams a numbered list of 3 identical items and finishes normally
      When the user submits "what can you do"
      Then no loop warning is shown

    @S-120
    Scenario: A long list of different items is not stopped
      Given a Provider that streams a numbered list of 60 different items and finishes normally
      When the user submits "what can you do"
      Then the last assistant Message is not marked truncated

  Rule: A phrase that repeats inside one line is stopped early

    @S-174
    Scenario: The user sees only a few copies of a phrase repeating inside one line
      Given a Provider that streams one line repeating a phrase 40 times and keeps answering
      When the user submits "what tools do you have"
      Then the user saw at most 4 copies of the phrase

    @S-175
    Scenario: The answer is stored without the repetition inside one line
      Given a Provider that streams one line repeating a phrase 40 times and keeps answering
      When the user submits "what tools do you have"
      Then the last assistant Message keeps at most one copy of the phrase and is marked truncated

    @S-176
    Scenario: A stopped answer is not reported as cut off by a limit
      Given a Provider that streams one line repeating a phrase 40 times and keeps answering
      When the user submits "what tools do you have"
      Then the Turn does not end as cut off by a limit

    @S-177
    Scenario: Two copies of a phrase in one line are not a loop
      Given a Provider that streams one line repeating a phrase 2 times and finishes normally
      When the user submits "what tools do you have"
      Then no loop warning is shown

    @S-178
    Scenario: A long line of different items is not stopped
      Given a Provider that streams one line of 80 different items and finishes normally
      When the user submits "what tools do you have"
      Then no loop warning is shown
