Feature: The board binary basics
  The one board binary prints its version, rejects unknown subcommands with a
  one-line usage error, and serves HTTP on the configured listen address.

  Scenario: board version prints the build-info version
    When I run "board version"
    Then the exit code is 0
    And stdout is a single line holding a version
    And stderr is empty

  Scenario: an unknown subcommand is a usage error
    When I run "board frobnicate"
    Then the exit code is 2
    And stdout is empty
    And stderr is exactly the usage line

  Scenario: no subcommand is a usage error
    When I run "board"
    Then the exit code is 2
    And stderr is exactly the usage line

  Scenario: board serve listens on the configured address
    Given the board server is running on a free port
    Then the server accepts connections on its listen address
