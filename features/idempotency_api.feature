Feature: Safe retries with an Idempotency-Key, on every POST under /api/v1
  A POST under /api/v1 that carries an Idempotency-Key is answered once. The same request sent again with the key
  within 24 hours gets the first answer back with Idempotent-Replayed: true and writes nothing. The same key with a
  different request is 422, a key that is not 1 to 255 printable ASCII characters is 400, and another principal's
  key of the same text is a new request.

  Background:
    Given a new database for the board commands
    When I run "vh admin migrate"
    And I run "vh admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port
    And I create the initiative "aa" titled "First" with no tags as actor "claude@laptop"
    And I create a task in "aa" titled "one" as actor "claude@laptop"

  Scenario Outline: a write sent twice with one key is answered twice and written once
    Given I note the counts of events, notes and live leases
    When I send POST "<path>" with body "<body>" as actor "claude@laptop" with the idempotency key "retry-1"
    Then the response status is <status>
    And the response has no header "Idempotent-Replayed"
    When I remember the response
    And I send POST "<path>" with body "<body>" as actor "claude@laptop" with the idempotency key "retry-1"
    Then the response status is <status>
    And the response header "Idempotent-Replayed" is "true"
    And the response body is the remembered response
    And the counts of events, notes and live leases changed by <events>, <notes> and <leases>

    Examples:
      | path           | body                                  | status | events | notes | leases |
      | /api/v1/tasks  | {'initiative':'aa','title':'again'}   | 201    | 1      | 0     | 0      |
      | /api/v1/notes  | {'summary':'once'}                    | 201    | 1      | 1     | 0      |
      | /api/v1/leases | {'task':'aa-1','intent':'mine'}       | 201    | 1      | 0     | 1      |
      | /api/v1/start  | {'task':'aa-1','intent':'mine'}       | 201    | 1      | 0     | 1      |
      | /api/v1/ping   | {'progress':'half way'}               | 409    | 0      | 0     | 0      |

  Scenario: a refused answer is replayed too
    Given I note the counts of events, notes and live leases
    When I send POST "/api/v1/ping" with body "{}" as actor "claude@laptop" with the idempotency key "refused"
    Then the response status is 409
    When I remember the response
    And I start with body "{'task':'aa-1','intent':'mine'}" as actor "claude@laptop"
    And I send POST "/api/v1/ping" with body "{}" as actor "claude@laptop" with the idempotency key "refused"
    Then the response status is 409
    And the response header "Idempotent-Replayed" is "true"
    And the response body is the remembered response

  Scenario Outline: the same key with a different request is 422 with a next and writes nothing
    Given I send POST "/api/v1/notes" with body "{'summary':'first'}" as actor "claude@laptop" with the idempotency key "mixed"
    And I note the counts of events, notes and live leases
    When I send POST "<path>" with body "<body>" as actor "<actor>" with the idempotency key "mixed"
    Then the response status is 422
    And the response path "next" is present
    And the response has no header "Idempotent-Replayed"
    And the counts of events, notes and live leases changed by 0, 0 and 0

    Examples:
      | path          | body                | actor          |
      | /api/v1/notes | {'summary':'other'} | claude@laptop  |
      | /api/v1/notes | {'summary':'first'} | codex@laptop   |
      | /api/v1/tasks | {'initiative':'aa','title':'two'} | claude@laptop |

  Scenario Outline: a key that is not 1 to 255 printable ASCII characters is 400 and writes nothing
    Given I note the counts of events, notes and live leases
    When I send POST "/api/v1/notes" with body "{'summary':'x'}" as actor "claude@laptop" with the idempotency key "<key>"
    Then the response status is 400
    And the response path "next" is present
    And the counts of events, notes and live leases changed by 0, 0 and 0

    Examples:
      | key          |
      |              |
      | <<k*256>>    |
      | café         |

  Scenario Outline: a key of 1 and of 255 characters is accepted
    When I send POST "/api/v1/notes" with body "{'summary':'x'}" as actor "claude@laptop" with the idempotency key "<key>"
    Then the response status is 201

    Examples:
      | key       |
      | k         |
      | <<k*255>> |

  Scenario: a request without the header is not remembered
    Given I note the counts of events, notes and live leases
    When I create a task in "aa" titled "two" as actor "claude@laptop"
    And I create a task in "aa" titled "two" as actor "claude@laptop"
    Then the response status is 201
    And the response has no header "Idempotent-Replayed"
    And the counts of events, notes and live leases changed by 2, 0 and 0

  Scenario: another principal sending the same key makes a new request
    Given I send POST "/api/v1/notes" with body "{'summary':'same'}" as actor "claude@laptop" with the idempotency key "shared"
    And I note the counts of events, notes and live leases
    When I run "vh admin keys create second"
    And I remember the key stdout printed
    And I send POST "/api/v1/notes" with body "{'summary':'same'}" as actor "claude@laptop" with the idempotency key "shared"
    Then the response status is 201
    And the response has no header "Idempotent-Replayed"
    And the counts of events, notes and live leases changed by 1, 1 and 0
    When I send POST "/api/v1/notes" with body "{'summary':'same'}" as actor "claude@laptop" with the idempotency key "shared"
    Then the response header "Idempotent-Replayed" is "true"
    And the counts of events, notes and live leases changed by 1, 1 and 0
