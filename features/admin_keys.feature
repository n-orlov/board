Feature: Bootstrapping API keys without a server
  `board admin keys create|list|revoke` work directly on the database named by
  BOARD_DB_URL, with no server running, on SQLite and on Postgres alike. A key
  is shown once at creation and never stored: the database keeps only its
  SHA-256 hash and an 8-character display prefix.

  Scenario: creating a key prints a brd_ key once
    Given a new database for the board commands
    When I run "board admin migrate"
    And I run "board admin keys create workstation"
    Then the exit code is 0
    And stderr is empty
    And stdout is one line that ends with a brd_ key
    And the key appears in stdout exactly once

  Scenario: listing keys does not show the key
    Given a new database for the board commands
    When I run "board admin migrate"
    And I run "board admin keys create workstation"
    And I remember the key stdout printed
    And I run "board admin keys list"
    Then the exit code is 0
    And stdout is one line starting with "workstation brd_"
    And stdout does not contain the remembered key

  Scenario: the stored row holds no plaintext key
    Given a new database for the board commands
    When I run "board admin migrate"
    And I run "board admin keys create workstation"
    And I remember the key stdout printed
    Then the api_keys row named "workstation" stores the SHA-256 hash of the key and an 8-character prefix
    And no column of that row contains the remembered key

  Scenario: a key made by board admin keys create is accepted by the server
    Given a new database for the board commands
    When I run "board admin migrate"
    And I run "board admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port
    And I request GET "/readyz" with the remembered key
    Then the response status is 200

  Scenario: after board admin keys revoke the same key is refused
    Given a new database for the board commands
    When I run "board admin migrate"
    And I run "board admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port
    And I request GET "/readyz" with the remembered key
    Then the response status is 200
    When I run "board admin keys revoke workstation"
    And I request GET "/readyz" with the remembered key
    Then the response status is 401
