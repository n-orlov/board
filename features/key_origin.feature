Feature: Key ownership and origin
  Every API key has an owner, the operator actor that owns it, and every event
  an origin, the operator actor behind the write: a session's own operator, or
  the owner of the key the write came through. An agent actor carries the
  origin of its latest keyed write, while an event keeps the origin it was
  written with. Migration 00013 backfills all three on a Phase 4 database.
  Nothing checks any of it. Each scenario runs on the leg BOARD_DB_LEG names.

  Scenario: migrating a Phase 4 database gives its keys, events and agents the operator as origin
    Given a Phase 4 database at schema version 11 holding one operator, two agents, one API key and events
    When I run "vh admin migrate"
    Then the exit code is 0
    And the api_keys row "laptop" has owner "operator"
    And the event "EV1" has origin "operator"
    And the event "EV2" has origin "operator"
    And the actor "claude@laptop" has origin "operator"
    And the actor "codex@laptop" has no origin
    And the actor "operator" has no origin

  Scenario: migrating a Phase 4 database with no operator row leaves every owner and origin null
    Given a Phase 4 database at schema version 11 holding one agent, one API key and a keyed event but no operator
    When I run "vh admin migrate"
    Then the exit code is 0
    And the api_keys row "laptop" has no owner
    And the event "EV1" has no origin
    And the actor "claude@laptop" has no origin

  Scenario: a key made by operator A writes as an agent and the event and the agent carry origin A
    Given a new database for the board commands
    When I run "vh admin migrate"
    And the operators "alice-op,bob-op" exist
    And I create the admin key "alice-key" owned by "alice-op"
    And the board server is running on a free port
    And the key "alice-key" as actor "claude@laptop" creates the initiative "by-alice"
    Then the response status is 201
    And the key "alice-key" reads the event of the initiative "by-alice" with origin "alice-op"
    And the key "alice-key" reads the actor "claude@laptop" with origin "alice-op"
    And the key "alice-key" reads the key "alice-key" owned by "alice-op"

  Scenario: keys of two operators give two origins
    Given a new database for the board commands
    When I run "vh admin migrate"
    And the operators "alice-op,bob-op" exist
    And I create the admin key "alice-key" owned by "alice-op"
    And I create the admin key "bob-key" owned by "bob-op"
    And the board server is running on a free port
    And the key "alice-key" as actor "claude@laptop" creates the initiative "by-alice"
    And the key "bob-key" as actor "codex@laptop" creates the initiative "by-bob"
    Then the key "alice-key" reads the event of the initiative "by-alice" with origin "alice-op"
    And the key "alice-key" reads the event of the initiative "by-bob" with origin "bob-op"
    And the key "alice-key" reads the actor "claude@laptop" with origin "alice-op"
    And the key "alice-key" reads the actor "codex@laptop" with origin "bob-op"

  Scenario: an earlier event keeps its origin after the agent's origin changes
    Given a new database for the board commands
    When I run "vh admin migrate"
    And the operators "alice-op,bob-op" exist
    And I create the admin key "alice-key" owned by "alice-op"
    And I create the admin key "bob-key" owned by "bob-op"
    And the board server is running on a free port
    And the key "alice-key" as actor "claude@laptop" creates the initiative "first"
    And the key "bob-key" as actor "claude@laptop" creates the initiative "second"
    Then the key "alice-key" reads the actor "claude@laptop" with origin "bob-op"
    And the key "alice-key" reads the event of the initiative "first" with origin "alice-op"
    And the key "alice-key" reads the event of the initiative "second" with origin "bob-op"

  Scenario: a write through a key with no owner clears the agent's origin and keeps the earlier event's
    Given a new database for the board commands
    When I run "vh admin migrate"
    And I create the admin key "loose-key" with no owner
    And the api_keys row "loose-key" has no owner
    And the operators "alice-op" exist
    And I create the admin key "alice-key" owned by "alice-op"
    And the board server is running on a free port
    And the key "alice-key" as actor "claude@laptop" creates the initiative "by-alice"
    And the key "alice-key" reads the actor "claude@laptop" with origin "alice-op"
    And the key "loose-key" as actor "claude@laptop" creates the initiative "by-loose"
    Then the response status is 201
    And the key "alice-key" reads the actor "claude@laptop" with no origin
    And the key "alice-key" reads the event of the initiative "by-loose" with no origin
    And the key "alice-key" reads the event of the initiative "by-alice" with origin "alice-op"

  Scenario: a key made through a key is owned by that key's owner
    Given a new database for the board commands
    When I run "vh admin migrate"
    And the operators "alice-op,bob-op" exist
    And I create the admin key "alice-key" owned by "alice-op"
    And the board server is running on a free port
    And the key "alice-key" as actor "claude@laptop" creates the API key "child"
    Then the response status is 201
    And the key "alice-key" reads the key "child" owned by "alice-op"
    And the key "child" as actor "other@laptop" creates the initiative "by-child"
    And the key "child" reads the event of the initiative "by-child" with origin "alice-op"

  Scenario: an admin-made key carries the owner --owner names, and VH_OPERATOR_HANDLE's operator by default
    Given a new database for the board commands
    When I run "vh admin migrate"
    And the operators "operator,bob-op" exist
    And I run "vh admin keys create explicit --owner bob-op"
    Then the exit code is 0
    And the api_keys row "explicit" has owner "bob-op"
    When I run "vh admin keys create default"
    Then the exit code is 0
    And the api_keys row "default" has owner "operator"

  Scenario Outline: a default owner that is no operator is refused on a board that has one, and nothing is written
    Given a new database for the board commands
    When I run "vh admin migrate"
    And the operators "boss" exist
    And the agent "worker" exists
    And I run "vh admin keys create refused" with BOARD_OPERATOR_HANDLE=<handle>
    Then the exit code is 2
    And stdout is empty
    And stderr is exactly one line containing "--owner"
    And the api_keys table holds 0 rows

    Examples:
      | handle |
      | worker |
      | ghost  |

  Scenario Outline: --owner naming no operator is refused and changes nothing
    Given a new database for the board commands
    When I run "vh admin migrate"
    And the operators "operator" exist
    And the agent "claude@laptop" exists
    And I run "vh admin keys create refused --owner <owner>"
    Then the exit code is 2
    And stdout is empty
    And stderr is exactly one line containing "--owner"
    And the api_keys table holds 0 rows

    Examples:
      | owner         |
      | nobody        |
      | claude@laptop |

  Scenario: a key made through a session is owned by the session's operator, and its writes carry that origin
    Given the mock OIDC provider signs in "alice@example.com" with a verified email
    And the board server is running with OIDC login allowing "alice@example.com" and BOARD_OIDC_OPERATORS set to "alice@example.com=alice-op"
    When the browser opens "/"
    And the browser follows the redirects
    Then the response status is 200
    When the browser reads its CSRF token from "/auth/session"
    And the browser sends POST "/api/v1/keys" with body "{'name':'by-session'}", the session's CSRF token and the board's own Origin
    Then the response status is 201
    When the browser sends POST "/api/v1/initiatives" with body "{'slug':'by-alice','title':'By alice'}", the session's CSRF token and the board's own Origin
    Then the response status is 201
    And the api_keys row "by-session" has owner "alice-op"
    And the event "initiative_created" of the initiative "by-alice" has origin "alice-op"
    And the event "key_created" of the key "by-session" has origin "alice-op"
