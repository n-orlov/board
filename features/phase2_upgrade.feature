Feature: Upgrading a populated Phase 1 database
  A database written by the Phase 1 release (schema version 4) and holding data
  migrates to the current schema without losing or rewriting a row: every
  column of the seeded rows reads back with the value it was written with.
  Each scenario runs on the leg BOARD_DB_LEG names.

  Scenario: a Phase 1 database with rows in six tables keeps every row through the Phase 2 migrations
    Given a Phase 1 database at schema version 4 holding rows in initiatives, tasks, leases, API keys, sessions and events
    When I run "board admin migrate"
    Then the exit code is 0
    And the database has migrated past schema version 4
    And the seeded rows of those six tables read back with identical column values
