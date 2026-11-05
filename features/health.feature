Feature: Liveness and readiness
  GET /healthz answers while the process is up. GET /readyz answers 200 only
  when the database is reachable and migrated, and 503 otherwise. These
  scenarios send no credentials because the binary has no authentication layer
  yet: task 015 puts /readyz behind credentials and rewrites them to
  authenticated access.

  Scenario: /healthz answers 200 without credentials
    Given the board server is running on a free port
    When I request GET "/healthz" without credentials
    Then the response status is 200

  Scenario: /readyz answers 200 on a migrated store
    Given the board server is running on a free port
    When I request GET "/readyz" without credentials
    Then the response status is 200

  Scenario: /readyz answers 503 on an unmigrated store with BOARD_MIGRATE=off
    Given the board server is running on a free port with BOARD_MIGRATE=off
    When I request GET "/readyz" without credentials
    Then the response status is 503

  Scenario: /healthz still answers 200 on an unmigrated store
    Given the board server is running on a free port with BOARD_MIGRATE=off
    When I request GET "/healthz" without credentials
    Then the response status is 200
