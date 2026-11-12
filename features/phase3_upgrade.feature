Feature: Upgrading a populated Phase 2 database
  A database written by the launch commit (schema version 8) and holding data
  migrates to the current schema without losing or rewriting a row: every
  column of the seeded rows reads back with the value it was written with, and
  the migrations after version 8 (the notifications outbox) are applied.
  Each scenario runs on the leg BOARD_DB_LEG names.

  Scenario: a Phase 2 database with rows in nine tables keeps every row through the Phase 3 migrations
    Given a Phase 2 database at schema version 8 holding rows in initiatives, tasks, leases, asks, notes, cursors, API keys, sessions and events
    When I run "board admin migrate"
    Then the exit code is 0
    And the database has migrated past schema version 8
    And the notifications table exists
    And the seeded rows of those nine tables read back with identical column values
