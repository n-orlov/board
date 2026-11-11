Feature: Leases over HTTP
  A lease is one actor's claim on one task for a time. POST /api/v1/leases starts it (201), or renews the
  caller's live lease on the task (200). Renew pushes the expiry, end finishes a live lease, and ack records
  that the operator has seen an expired one. A lease past its expiry is 410 on renew, a lease that is not
  live cannot be ended, and a refusal changes nothing. Expiry is made by backdating the row, never by waiting.

  Background:
    Given a new database for the board commands
    When I run "board admin migrate"
    And I run "board admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port
    And I create the initiative "aa" titled "First" with no tags as actor "claude@laptop"
    And I create a task in "aa" titled "one" as actor "claude@laptop"
    And I create a task in "aa" titled "two" as actor "claude@laptop"

  Scenario: starting a lease answers 201 with the lease and no conflicts
    When I start a lease with body "{'task':'aa-1','intent':'wire the thing'}" as actor "claude@laptop"
    Then the response status is 201
    And the response path "lease.ref" is "L1"
    And the response path "lease.task" is "aa-1"
    And the response path "lease.actor" is "claude@laptop"
    And the response path "lease.intent" is "wire the thing"
    And the response path "lease.state" is "live"
    And the response path "lease.ttl_seconds" is "7200"
    And the response path "lease.resources" has 0 items
    And the response path "lease.ended_at" is absent
    And the response path "conflicts" has 0 items
    And exactly 1 lease_started event exists for the lease "L1"
    And exactly 1 lease_started event exists for the task "aa-1"
    And the latest lease_started event of "aa-1" has data.ref "L1"

  Scenario: resources are sorted and repeats dropped
    When I start a lease with body "{'task':'aa-1','intent':'x','resources':['repo:y','db:x','repo:y']}" as actor "claude@laptop"
    Then the response status is 201
    And the response path "lease.resources" is "db:x,repo:y"

  Scenario: starting again on the same task renews the lease and answers 200
    Given I start a lease with body "{'task':'aa-1','intent':'first','resources':['db:x']}" as actor "claude@laptop"
    When I start a lease with body "{'task':'aa-1','intent':'second'}" as actor "claude@laptop"
    Then the response status is 200
    And the response path "lease.ref" is "L1"
    And the response path "lease.intent" is "second"
    And the response path "lease.resources" is "db:x"
    And the response path "lease.expires_at" is the request time plus 7200 seconds
    And there are 1 leases in the database
    And exactly 1 lease_started event exists for the lease "L1"
    And exactly 1 lease_renewed event exists for the lease "L1"

  Scenario: another actor on the same task gets its own lease
    Given I start a lease with body "{'task':'aa-1','intent':'first'}" as actor "claude@laptop"
    When I start a lease with body "{'task':'aa-1','intent':'second'}" as actor "codex@laptop"
    Then the response status is 201
    And the response path "lease.ref" is "L2"
    And the response path "lease.actor" is "codex@laptop"

  Scenario: starting on a task that is done is 409 and names the reopen
    Given I mark the task "aa-1" done as actor "claude@laptop"
    When I start a lease with body "{'task':'aa-1','intent':'x'}" as actor "claude@laptop"
    Then the response status is 409
    And the response path "detail" is "cannot start a lease on task aa-1: it is done"
    And there are 0 leases in the database

  Scenario: starting on a closed task is 409 and writes nothing
    Given I close the task "aa-1" with body "{'resolution':'dropped'}" as actor "claude@laptop"
    And I note the counts of tasks, dependencies, events and actors
    When I start a lease with body "{'task':'aa-1','intent':'x'}" as actor "codex@laptop"
    Then the response status is 409
    And the response path "detail" is "cannot start a lease on task aa-1: it is closed"
    And the response path "next" is present
    And there are 0 leases in the database
    And the counts of tasks, dependencies, events and actors are unchanged

  Scenario Outline: the ttl is clamped to 60 to 43200 and defaults to 7200
    When I start a lease with body "<body>" as actor "claude@laptop"
    Then the response status is 201
    And the response path "lease.ttl_seconds" is "<ttl>"
    And the response path "lease.expires_at" is the request time plus <ttl> seconds

    Examples:
      | body                                       | ttl   |
      | {'task':'aa-1','intent':'x'}               | 7200  |
      | {'task':'aa-1','intent':'x','ttl_seconds':1}     | 60    |
      | {'task':'aa-1','intent':'x','ttl_seconds':60}    | 60    |
      | {'task':'aa-1','intent':'x','ttl_seconds':3600}  | 3600  |
      | {'task':'aa-1','intent':'x','ttl_seconds':43200} | 43200 |
      | {'task':'aa-1','intent':'x','ttl_seconds':99999} | 43200 |

  Scenario Outline: a start the server cannot take is refused and writes nothing
    Given I note the counts of tasks, dependencies, events and actors
    When I start a lease with body "<body>" as actor "codex@laptop"
    Then the response status is <status>
    And the response path "next" is present
    And there are 0 leases in the database
    And the counts of tasks, dependencies, events and actors are unchanged

    Examples:
      | body                                                          | status |
      | {'task':'aa-9','intent':'x'}                                  | 404    |
      | {'task':'aa-1','intent':''}                                   | 400    |
      | {'task':'aa-1','intent':'<<x*201>>'}                          | 400    |
      | {'task':'aa-1','intent':'x','resources':['']}                 | 400    |
      | {'task':'aa-1','intent':'x','resources':['<<y*101>>']}        | 400    |
      | {'task':'aa-1','intent':'x','resources':['r1','r2','r3','r4','r5','r6','r7','r8','r9','r10','r11','r12','r13','r14','r15','r16','r17','r18','r19','r20','r21']} | 400 |
      | {'intent':'x'}                                                | 400    |

  Scenario: a start with no body is 400
    When I start a lease without a body as actor "claude@laptop"
    Then the response status is 400
    And the response path "next" is present

  Scenario: renew pushes the expiry and may change the ttl, intent and resources
    Given I start a lease with body "{'task':'aa-1','intent':'first','ttl_seconds':600}" as actor "claude@laptop"
    When I renew the lease "L1" with body "{'ttl_seconds':3600,'intent':'later','resources':['db:x']}" as actor "claude@laptop"
    Then the response status is 200
    And the response path "lease.ref" is "L1"
    And the response path "lease.ttl_seconds" is "3600"
    And the response path "lease.intent" is "later"
    And the response path "lease.resources" is "db:x"
    And the response path "lease.expires_at" is the request time plus 3600 seconds
    And the response path "conflicts" has 0 items
    And exactly 1 lease_renewed event exists for the lease "L1"

  Scenario: renew with no body keeps the stored ttl and clamps a sent one
    Given I start a lease with body "{'task':'aa-1','intent':'first','ttl_seconds':600}" as actor "claude@laptop"
    When I renew the lease "L1" without a body as actor "claude@laptop"
    Then the response status is 200
    And the response path "lease.ttl_seconds" is "600"
    When I renew the lease "L1" with body "{'ttl_seconds':5}" as actor "claude@laptop"
    Then the response path "lease.ttl_seconds" is "60"
    When I renew the lease "L1" with body "{'ttl_seconds':500000}" as actor "claude@laptop"
    Then the response path "lease.ttl_seconds" is "43200"

  Scenario: any actor may renew a live lease
    Given I start a lease with body "{'task':'aa-1','intent':'first'}" as actor "claude@laptop"
    When I renew the lease "l1" without a body as actor "codex@laptop"
    Then the response status is 200
    And the response path "lease.actor" is "claude@laptop"

  Scenario: renewing an expired lease is 410 and carries expired_at
    Given I start a lease with body "{'task':'aa-1','intent':'first'}" as actor "claude@laptop"
    And the lease "L1" ran out 30 seconds ago
    When I renew the lease "L1" without a body as actor "claude@laptop"
    Then the response status is 410
    And the response path "expired_at" is present
    And the response expired_at is the expiry stored for the lease "L1"
    And the response path "next" is present
    And the lease "L1" is "live" in the database
    And exactly 0 lease_renewed events exist for the lease "L1"
    And exactly 0 lease_expired events exist for the lease "L1"
    When I request the lease "L1" as actor "claude@laptop"
    Then the response path "state" is "expired"
    And the lease "L1" is "expired" in the database
    And exactly 1 lease_expired event exists for the lease "L1"

  Scenario: renewing an ended lease is 409
    Given I start a lease with body "{'task':'aa-1','intent':'first'}" as actor "claude@laptop"
    And I end the lease "L1" without a body as actor "claude@laptop"
    When I renew the lease "L1" without a body as actor "claude@laptop"
    Then the response status is 409
    And the response path "detail" is "cannot renew lease L1: it is ended"

  Scenario: renewing with a bad intent is 400
    Given I start a lease with body "{'task':'aa-1','intent':'first'}" as actor "claude@laptop"
    When I renew the lease "L1" with body "{'intent':''}" as actor "claude@laptop"
    Then the response status is 400

  Scenario: ending a lease by its holder sets the outcome and no ended_by
    Given I start a lease with body "{'task':'aa-1','intent':'first'}" as actor "claude@laptop"
    When I end the lease "L1" with body "{'outcome':'merged'}" as actor "claude@laptop"
    Then the response status is 200
    And the response path "state" is "ended"
    And the response path "outcome" is "merged"
    And the response path "ended_at" is present
    And the response path "ended_by" is absent
    And exactly 1 lease_ended event exists for the lease "L1"
    And the latest lease_ended event of "aa-1" has data.outcome "merged"

  Scenario: ending a lease by another actor sets ended_by
    Given I start a lease with body "{'task':'aa-1','intent':'first'}" as actor "claude@laptop"
    When I end the lease "L1" without a body as actor "codex@laptop"
    Then the response status is 200
    And the response path "state" is "ended"
    And the response path "ended_by" is "codex@laptop"
    And the response path "actor" is "claude@laptop"
    And the latest lease_ended event of "aa-1" has data.ended_by "codex@laptop"

  Scenario: ending a lease twice is 409 and writes one event
    Given I start a lease with body "{'task':'aa-1','intent':'first'}" as actor "claude@laptop"
    And I end the lease "L1" without a body as actor "claude@laptop"
    When I end the lease "L1" without a body as actor "claude@laptop"
    Then the response status is 409
    And the response path "detail" is "cannot end lease L1: it is ended"
    And exactly 1 lease_ended event exists for the lease "L1"

  Scenario: ending an expired lease is 409
    Given I start a lease with body "{'task':'aa-1','intent':'first'}" as actor "claude@laptop"
    And the lease "L1" ran out 30 seconds ago
    When I end the lease "L1" without a body as actor "claude@laptop"
    Then the response status is 409
    And the response path "detail" is "cannot end lease L1: it is expired"

  Scenario: an outcome over 200 characters is 400
    Given I start a lease with body "{'task':'aa-1','intent':'first'}" as actor "claude@laptop"
    When I end the lease "L1" with body "{'outcome':'<<x*201>>'}" as actor "claude@laptop"
    Then the response status is 400
    And the lease "L1" is "live" in the database

  Scenario: acknowledging an expired lease records who and when
    Given I start a lease with body "{'task':'aa-1','intent':'first'}" as actor "claude@laptop"
    And the lease "L1" ran out 30 seconds ago
    When I acknowledge the lease "L1" as actor "operator@laptop"
    Then the response status is 200
    And the response path "state" is "expired"
    And the response path "acked_by" is "operator@laptop"
    And the response path "acked_at" is present
    And exactly 1 lease_acked event exists for the lease "L1"
    And exactly 1 lease_expired event exists for the lease "L1"

  Scenario: acknowledging twice changes nothing
    Given I start a lease with body "{'task':'aa-1','intent':'first'}" as actor "claude@laptop"
    And the lease "L1" ran out 30 seconds ago
    And I acknowledge the lease "L1" as actor "operator@laptop"
    When I acknowledge the lease "L1" as actor "codex@laptop"
    Then the response status is 200
    And the response path "acked_by" is "operator@laptop"
    And exactly 1 lease_acked event exists for the lease "L1"

  Scenario: acknowledging a live lease is 409
    Given I start a lease with body "{'task':'aa-1','intent':'first'}" as actor "claude@laptop"
    When I acknowledge the lease "L1" as actor "operator@laptop"
    Then the response status is 409
    And the response path "detail" is "cannot acknowledge lease L1: it is live"
    And exactly 0 lease_acked events exist for the lease "L1"

  Scenario: acknowledging an ended lease is 409
    Given I start a lease with body "{'task':'aa-1','intent':'first'}" as actor "claude@laptop"
    And I end the lease "L1" without a body as actor "claude@laptop"
    When I acknowledge the lease "L1" as actor "operator@laptop"
    Then the response status is 409
    And the response path "detail" is "cannot acknowledge lease L1: it is ended"

  Scenario Outline: a lease that does not exist is 404 on every operation
    When I <do> as actor "claude@laptop"
    Then the response status is 404
    And the response path "detail" is "there is no lease L9"

    Examples:
      | do                                        |
      | request the lease "L9"                    |
      | renew the lease "L9" without a body       |
      | end the lease "L9" without a body         |
      | acknowledge the lease "L9"                |

  Scenario: a lease is read by ref in either case and by its id
    Given I start a lease with body "{'task':'aa-1','intent':'first'}" as actor "claude@laptop"
    When I request the lease "l1" as actor "codex@laptop"
    Then the response status is 200
    And the response path "ref" is "L1"
    And the response path "intent" is "first"

  Scenario: a lease past its expiry is read as expired and the expiry is written once
    Given I start a lease with body "{'task':'aa-1','intent':'first'}" as actor "claude@laptop"
    And the lease "L1" ran out 30 seconds ago
    When I request the lease "L1" as actor "codex@laptop"
    Then the response status is 200
    And the response path "state" is "expired"
    When I request the lease "L1" as actor "codex@laptop"
    Then exactly 1 lease_expired event exists for the lease "L1"

  Scenario: the task lists its live lease, which ends with the task when it is closed
    Given I start a lease with body "{'task':'aa-1','intent':'first','resources':['db:x']}" as actor "claude@laptop"
    When I request the task "aa-1" as actor "claude@laptop"
    Then the response status is 200
    And the response path "active" is "true"
    And the response path "live_leases" has 1 items
    And the response path "live_leases.0.ref" is "L1"
    And the response path "live_leases.0.actor" is "claude@laptop"
    And the response path "live_leases.0.state" is "live"
    And the response path "live_leases.0.resources" is "db:x"
    When I close the task "aa-1" with body "{'resolution':'completed'}" as actor "claude@laptop"
    Then the response path "status" is "closed"
    And the response path "active" is "false"
    And the response path "live_leases" has 0 items
    And the lease "L1" is "ended" in the database

  Scenario: the task lists its live lease, which ends with the task when it is done
    Given I start a lease with body "{'task':'aa-1','intent':'first'}" as actor "claude@laptop"
    When I mark the task "aa-1" done as actor "claude@laptop"
    Then the response path "status" is "done"
    And the response path "active" is "false"
    And the response path "live_leases" has 0 items
    And the lease "L1" is "ended" in the database

  Scenario: the task stops being active when its lease ends or runs out
    Given I start a lease with body "{'task':'aa-1','intent':'first'}" as actor "claude@laptop"
    And I start a lease with body "{'task':'aa-2','intent':'second'}" as actor "claude@laptop"
    And I end the lease "L1" without a body as actor "claude@laptop"
    And the lease "L2" ran out 30 seconds ago
    When I request the task "aa-1" as actor "claude@laptop"
    Then the response path "active" is "false"
    And the response path "live_leases" has 0 items
    When I request the task "aa-2" as actor "claude@laptop"
    Then the response path "active" is "false"
    And the response path "live_leases" has 0 items
