Feature: A refused lease write from a browser session changes nothing
  A cookie-authenticated lease request the board refuses leaves every row as it was, the session's
  own row included: the last-seen time a session lookup is due to write, and the removal of a dead
  session, commit only with a request that succeeds (a status below 400), exactly like an API key's
  last_used_at. The session is made due for a last-seen write, or dead, by backdating its row, never
  by waiting, and an unrelated due lease shows that no expiry commits either.

  Background:
    Given a new database for the board commands
    When I run "board admin migrate"
    And I run "board admin keys create workstation"
    And I remember the key stdout printed
    And the mock OIDC provider signs in "alice@example.com" with a verified email
    And the board server is running with OIDC login allowing "alice@example.com" and BOARD_SWEEP_INTERVAL set to "off"
    And I create the initiative "aa" titled "First" with no tags as actor "claude@laptop"
    And I create a task in "aa" titled "one" as actor "claude@laptop"
    And I create a task in "aa" titled "two" as actor "claude@laptop"
    And I create a task in "aa" titled "three" as actor "claude@laptop"
    And I start a lease with body "{'task':'aa-1','intent':'due soon'}" as actor "claude@laptop"
    And I start a lease with body "{'task':'aa-2','intent':'second'}" as actor "claude@laptop"
    And I end the lease "L2" without a body as actor "claude@laptop"
    And I close the task "aa-3" with body "{'resolution':'dropped'}" as actor "claude@laptop"
    And the browser opens "/"
    And the browser follows the redirects
    And the browser reads its CSRF token from "/auth/session"

  Scenario Outline: a refused <what> from a session due for a last-seen write changes no row
    Given the lease "L1" ran out 30 seconds ago
    And the session was last seen 3 hours ago
    And I note every row of every table
    When <request>
    Then the response status is <status>
    And every row of every table is unchanged
    And the lease "L1" is "live" in the database
    And exactly 0 lease_expired events exist for the lease "L1"

    Examples:
      | what                                | request                                                                                                                     | status |
      | start on an unknown task            | the browser sends POST "/api/v1/leases" with body "{'task':'aa-9','intent':'x'}", the session's CSRF token and the board's own Origin | 404    |
      | start on a closed task              | the browser sends POST "/api/v1/leases" with body "{'task':'aa-3','intent':'x'}", the session's CSRF token and the board's own Origin | 409    |
      | renewal of an ended lease           | the browser sends POST "/api/v1/leases/L2/renew" with the session's CSRF token and the board's own Origin                  | 409    |
      | renewal of an unknown lease         | the browser sends POST "/api/v1/leases/L9/renew" with the session's CSRF token and the board's own Origin                  | 404    |
      | end of an ended lease               | the browser sends POST "/api/v1/leases/L2/end" with the session's CSRF token and the board's own Origin                    | 409    |
      | end of an unknown lease             | the browser sends POST "/api/v1/leases/L999/end" with the session's CSRF token and the board's own Origin                  | 404    |
      | end without the CSRF token          | the browser sends POST "/api/v1/leases/L999/end" with no CSRF token and the board's own Origin                             | 403    |
      | renewal from a foreign Origin       | the browser sends POST "/api/v1/leases/L1/renew" with the session's CSRF token and the Origin "https://evil.example"      | 403    |
      | acknowledgement of an ended lease   | the browser sends POST "/api/v1/leases/L2/ack" with the session's CSRF token and the board's own Origin                    | 409    |
      | acknowledgement of an unknown lease | the browser sends POST "/api/v1/leases/L9/ack" with the session's CSRF token and the board's own Origin                    | 404    |

  Scenario: a refused write from a dead session leaves the session's row and every other row alone
    Given the lease "L1" ran out 30 seconds ago
    And the session was last seen 25 hours ago
    And I note every row of every table
    When the browser sends POST "/api/v1/leases/L9/end" with the session's CSRF token and the board's own Origin
    Then the response status is 401
    And every row of every table is unchanged
    And the database holds 1 session
    When the browser opens "/"
    Then the response redirects to "/auth/login"
    And the database holds 0 sessions

  Scenario: a refusal does not suppress the session's last-seen write on its next successful request
    Given the lease "L1" ran out 30 seconds ago
    And the session was last seen 3 hours ago
    And I note every row of every table
    When the browser sends POST "/api/v1/leases/L9/renew" with the session's CSRF token and the board's own Origin
    Then the response status is 404
    And every row of every table is unchanged
    When the browser sends POST "/api/v1/leases" with body "{'task':'aa-2','intent':'from the browser'}", the session's CSRF token and the board's own Origin
    Then the response status is 201
    And the session was seen within the last minute
    And the lease "L1" is "expired" in the database
    And exactly 1 lease_expired event exists for the lease "L1"
