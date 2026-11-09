Feature: The store and board admin migrate
  The binary embeds the goose migrations for the Phase 0 tables. `board admin
  migrate` applies them, and a database whose schema is newer than the binary is
  refused instead of served.

  Scenario: board admin migrate applies the embedded migrations to an empty database
    Given a new database for the board commands
    When I run "board admin migrate"
    Then the exit code is 0
    And stdout is exactly "ok migrated, schema version 6"
    And stderr is empty

  Scenario: board admin migrate is idempotent
    Given a new database for the board commands
    When I run "board admin migrate"
    And I run "board admin migrate"
    Then the exit code is 0
    And stdout is exactly "ok migrated, schema version 6"

  Scenario: a database migrated by board admin migrate is ready without BOARD_MIGRATE
    Given a new database for the board commands
    When I run "board admin migrate"
    And I run "board admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port with BOARD_MIGRATE=off
    And I request GET "/readyz" with the remembered key
    Then the response status is 200

  Scenario: board admin migrate refuses a schema newer than the binary
    Given a new database for the board commands
    And I run "board admin migrate"
    And the database records schema version 9999 as applied
    When I run "board admin migrate"
    Then the exit code is 1
    And stdout is empty
    And stderr is exactly one line containing "newer than this binary"

  Scenario: board serve exits non-zero with one stderr line on a schema newer than the binary
    Given a new database for the board commands
    And I run "board admin migrate"
    And the database records schema version 9999 as applied
    When I run "board serve"
    Then the exit code is 1
    And stdout is empty
    And stderr is exactly one line containing "newer than this binary"

  Scenario: board serve refuses a newer schema even with BOARD_MIGRATE=off
    Given a new database for the board commands
    And I run "board admin migrate"
    And the database records schema version 9999 as applied
    When I run "board serve" with BOARD_MIGRATE=off
    Then the exit code is 1
    And stderr is exactly one line containing "newer than this binary"

  Scenario: board admin rejects an unknown subcommand with the usage line
    When I run "board admin frobnicate"
    Then the exit code is 2
    And stderr is exactly the usage line
