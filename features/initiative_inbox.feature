Feature: The built-in inbox initiative
  `vh serve` creates the initiative inbox (title Inbox, description "Work
  that fits no other initiative yet.", no tags) when none exists, with one
  initiative_created event made by the operator actor under the principal
  system:bootstrap. An inbox that exists is never changed, whatever its title,
  tags or status. Each scenario runs on the leg BOARD_DB_LEG names.

  Scenario: a fresh database gets the inbox with exactly one creation event
    When the board server is running on a free port
    Then the initiative "inbox" is open, titled "Inbox", described "Work that fits no other initiative yet." and has no tags
    And the initiative "inbox" has exactly 1 initiative_created event, made by operator "operator" under principal "system:bootstrap"
    When the board server is stopped and started again
    Then the initiative "inbox" has exactly 1 initiative_created event, made by operator "operator" under principal "system:bootstrap"

  Scenario: the operator actor of the event is BOARD_OPERATOR_HANDLE
    When the board server is running on a free port with BOARD_OPERATOR_HANDLE=boss
    Then the initiative "inbox" has exactly 1 initiative_created event, made by operator "boss" under principal "system:bootstrap"

  Scenario: an existing inbox with another title and tags keeps them
    Given a migrated database holding the initiative "inbox" titled "My pile" with the tags "mine, later"
    When the board server is running on a free port
    Then the initiative "inbox" is open, titled "My pile", described "" and has the tags "later, mine"
    And the initiative "inbox" has exactly 1 initiative_created event, made by agent "claude@laptop" under principal "key:seed"

  Scenario: a closed inbox stays closed
    Given a migrated database holding the closed initiative "inbox" titled "Inbox"
    When the board server is running on a free port
    Then the initiative "inbox" is closed, titled "Inbox", described "" and has no tags
    And the initiative "inbox" has exactly 1 initiative_created event, made by agent "claude@laptop" under principal "key:seed"
