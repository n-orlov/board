Feature: The agent protocol, board help agent
  An agent reads the rules of working with the board from the binary itself:
  board help agent prints a short cheat sheet headed with its version, records
  that the actor has read it, and prints even when no server can be reached.

  Background:
    Given a new database for the board commands
    When I run "board admin migrate"
    And I run "board admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port

  Scenario: every command and flag the text names exists in the CLI command tree
    When I run the client 'board help agent' as actor "claude@laptop"
    Then the exit code is 0
    And stderr is empty
    And stdout line 1 matches "^board protocol v2$"
    And the stdout text names at least 5 commands
    And every board command and flag the stdout text names exists in the command tree
    When I run the client 'board help agent --version' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is exactly "2"

  Scenario: reading the text records protocol_seen through the actors endpoint
    Then there is no actor row for "claude@laptop"
    When I run the client 'board help agent --version' as actor "claude@laptop"
    Then there is no actor row for "claude@laptop"
    When I run the client 'board help agent' as actor "claude@laptop"
    Then the exit code is 0
    And the protocol_seen of the actor "claude@laptop" is 2
    When I record protocol version 3 as read as actor "claude@laptop" through the API
    Then the response status is 200
    And the response field "actor" is "claude@laptop"
    And the response field "protocol_seen" is "3"
    When I run the client 'board help agent' as actor "claude@laptop"
    Then the protocol_seen of the actor "claude@laptop" is 3
    When I record protocol version -1 as read as actor "claude@laptop" through the API
    Then the response status is 400
    And the problem has a next field containing "version"

  Scenario: the text still prints when the server cannot be reached
    When I run the client 'board help agent' as actor "claude@laptop" against the address "http://127.0.0.1:1"
    Then the exit code is 0
    And stderr is empty
    And stdout line 1 matches "^board protocol v2$"
    And every board command and flag the stdout text names exists in the command tree
