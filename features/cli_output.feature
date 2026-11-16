Feature: The command-line output rules and exit codes
  Every client command prints compact plain text: one ok line for a write, one
  stderr line ending with the next action for an error, at most 20 items in a
  plain list, and full objects with --json. The exit code tells the caller what
  kind of outcome it was: 0 success, 1 error, 2 usage, 4 not found, 5
  authentication failed.

  Background:
    Given a new database for the board commands
    When I run "board admin migrate"
    And I run "board admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port

  Scenario: exit 0, a write prints one ok line and --json prints the full object
    When I run the client 'board init add phone-onboard "Onboard the new phone"' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is exactly "ok initiative phone-onboard created"
    And stderr is empty
    When I run the client 'board init add other-one "Another" --json' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is a JSON object with a ULID id
    When I run the client 'board init ls --json' as actor "claude@laptop"
    Then stdout is a JSON array of 3 objects with ULID ids

  Scenario: exit 1, a server that cannot be reached is one error line with the next action
    When I run the client 'board init ls' as actor "claude@laptop" against the address "http://127.0.0.1:1"
    Then the exit code is 1
    And stdout is empty
    And stderr is one line ending with "→ check VH_URL and that board serve is running"

  Scenario: exit 2, a bad flag is a usage error with one usage line
    When I run the client 'board init ls --frobnicate' as actor "claude@laptop"
    Then the exit code is 2
    And stdout is empty
    And stderr is exactly one line containing "usage: board init"

  Scenario: exit 4, an unknown ref is one error line with the next action
    When I run the client 'board init show nope-nope' as actor "claude@laptop"
    Then the exit code is 4
    And stdout is empty
    And stderr is one line ending with "→ board init ls"

  Scenario: exit 5, a missing or revoked key is one error line with the next action
    When I run the client 'board init ls' as actor "claude@laptop" with no BOARD_API_KEY
    Then the exit code is 5
    And stderr is exactly "VH_API_KEY is unset (BOARD_API_KEY is also accepted) → export VH_API_KEY (this environment's key)"
    When I run "board admin keys revoke workstation"
    And I run the client 'board init ls' as actor "claude@laptop"
    Then the exit code is 5
    And stdout is empty
    And stderr is one line ending with "→ check VH_API_KEY (board admin keys list shows which keys are active)"
    And neither stdout nor stderr contains the remembered key

  Scenario: a list of exactly 20 items shows all of them and no more line
    Given 19 initiatives exist as actor "claude@laptop", which with the inbox make 20
    When I run the client 'board init ls' as actor "claude@laptop"
    Then the exit code is 0
    And stdout has 20 lines
    And stdout has no more line

  Scenario: a list of 21 items shows 20 and a +1 more line that names the command
    Given 20 initiatives exist as actor "claude@laptop", which with the inbox make 21
    When I run the client 'board init ls' as actor "claude@laptop"
    Then the exit code is 0
    And stdout has 21 lines
    And stdout line 21 matches "^\+1 more → board init ls --all$"
    When I run the client 'board init ls --all' as actor "claude@laptop"
    Then stdout has 21 lines
    And stdout has no more line
