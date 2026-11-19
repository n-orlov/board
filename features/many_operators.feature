Feature: Several operators
  Migration 00012 drops the unique index that allowed one operator actor. An
  admitted email maps to an operator handle through VH_OIDC_OPERATORS
  (email=handle, comma separated, emails case-insensitive); an admitted email
  with no entry acts as VH_OPERATOR_HANDLE. A session acts as its email's handle
  and that row is kind operator. Each scenario runs on the leg BOARD_DB_LEG names.

  Scenario: a Phase 4 database with one operator, two agents, a key and events migrates and then takes a second operator
    Given a Phase 4 database at schema version 11 holding one operator, two agents, one API key and events
    Then a second operator actor cannot be inserted
    When I run "vh admin migrate"
    Then the exit code is 0
    And the database has migrated past schema version 11
    And the seeded actors, API key and events read back with identical column values
    And a second operator actor can be inserted
    And the actors table holds 2 operators and 2 agents

  Scenario: two mapped emails sign in as two operator actors whose rows coexist
    Given the mock OIDC provider signs in "alice@example.com" with a verified email
    And the board server is running with OIDC login allowing "alice@example.com,bob@example.com" and BOARD_OIDC_OPERATORS set to "Alice@Example.com=alice-op,bob@example.com=bob-op"
    When the browser opens "/"
    And the browser follows the redirects
    Then the response status is 200
    When the browser reads its CSRF token from "/auth/session"
    Then the session response is for "alice@example.com" acting as "alice-op"
    When the browser sends POST "/api/v1/initiatives" with body "{'slug':'by-alice','title':'By alice'}", the session's CSRF token and the board's own Origin
    Then the response status is 201
    When the browser forgets its cookies
    And the mock OIDC provider signs in "bob@example.com" with a verified email
    And the browser opens "/"
    And the browser follows the redirects
    Then the response status is 200
    When the browser reads its CSRF token from "/auth/session"
    Then the session response is for "bob@example.com" acting as "bob-op"
    When the browser sends POST "/api/v1/initiatives" with body "{'slug':'by-bob','title':'By bob'}", the session's CSRF token and the board's own Origin
    Then the response status is 201
    And the actors table holds the operator handles "alice-op,bob-op,operator"
    And the initiative "by-alice" was created by "alice-op"
    And the initiative "by-bob" was created by "bob-op"

  Scenario: an unmapped admitted email acts as VH_OPERATOR_HANDLE
    Given the mock OIDC provider signs in "carol@example.com" with a verified email
    And the board server is running with OIDC login allowing "alice@example.com,carol@example.com" and BOARD_OIDC_OPERATORS set to "alice@example.com=alice-op" and BOARD_OPERATOR_HANDLE set to "boss"
    When the browser opens "/"
    And the browser follows the redirects
    Then the response status is 200
    When the browser reads its CSRF token from "/auth/session"
    Then the session response is for "carol@example.com" acting as "boss"
    When the browser sends POST "/api/v1/initiatives" with body "{'slug':'by-carol','title':'By carol'}", the session's CSRF token and the board's own Origin
    Then the response status is 201
    And the initiative "by-carol" was created by "boss"
    And the actors table holds the operator handles "boss"

  Scenario Outline: a malformed VH_OIDC_OPERATORS makes vh serve exit 1
    Given the mock OIDC provider signs in "alice@example.com" with a verified email
    When I run "vh serve" with OIDC login allowing "alice@example.com" and BOARD_OIDC_OPERATORS set to "<value>"
    Then the exit code is 1
    And stderr is exactly one line containing "VH_OIDC_OPERATORS"

    Examples:
      | value                                   |
      | alice@example.com                       |
      | alice@example.com=two words             |
      | alice@example.com=a,alice@example.com=b |
