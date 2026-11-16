Feature: A refused lease write changes nothing
  A lease request the board refuses (a closed or unknown task, an ended, expired, live or unknown lease)
  leaves every row, actor and event as it was, even when an unrelated live lease is past its expiry: the
  expiry a lease write needs commits only with a request that succeeds. The sweeper and every read still
  expire a due lease and write exactly one lease_expired event for it. Expiry is made by backdating the
  row, never by waiting.

  Background:
    Given a new database for the board commands
    When I run "vh admin migrate"
    And I run "vh admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port with BOARD_SWEEP_INTERVAL=off
    And I create the initiative "aa" titled "First" with no tags as actor "claude@laptop"
    And I create a task in "aa" titled "one" as actor "claude@laptop"
    And I create a task in "aa" titled "two" as actor "claude@laptop"
    And I create a task in "aa" titled "three" as actor "claude@laptop"
    And I start a lease with body "{'task':'aa-1','intent':'due soon'}" as actor "claude@laptop"
    And I start a lease with body "{'task':'aa-2','intent':'second'}" as actor "claude@laptop"

  Scenario: a start on a closed task is refused and the unrelated due lease stays live
    Given I close the task "aa-3" with body "{'resolution':'dropped'}" as actor "claude@laptop"
    And the lease "L1" ran out 30 seconds ago
    And I note the counts of tasks, dependencies, events and actors
    And I note the lease rows
    When I start a lease with body "{'task':'aa-3','intent':'x'}" as actor "codex@laptop"
    Then the response status is 409
    And the counts of tasks, dependencies, events and actors are unchanged
    And the lease rows are unchanged
    And the lease "L1" is "live" in the database
    And exactly 0 lease_expired events exist for the lease "L1"

  Scenario: a start on an unknown task is refused and the unrelated due lease stays live
    Given the lease "L1" ran out 30 seconds ago
    And I note the counts of tasks, dependencies, events and actors
    And I note the lease rows
    When I start a lease with body "{'task':'aa-9','intent':'x'}" as actor "codex@laptop"
    Then the response status is 404
    And the counts of tasks, dependencies, events and actors are unchanged
    And the lease rows are unchanged
    And exactly 0 lease_expired events exist for the lease "L1"

  Scenario: a renewal of an ended lease is refused and the unrelated due lease stays live
    Given I end the lease "L2" without a body as actor "claude@laptop"
    And the lease "L1" ran out 30 seconds ago
    And I note the counts of tasks, dependencies, events and actors
    And I note the lease rows
    When I renew the lease "L2" without a body as actor "codex@laptop"
    Then the response status is 409
    And the counts of tasks, dependencies, events and actors are unchanged
    And the lease rows are unchanged
    And exactly 0 lease_expired events exist for the lease "L1"

  Scenario: a renewal of an unknown lease is refused and the unrelated due lease stays live
    Given the lease "L1" ran out 30 seconds ago
    And I note the counts of tasks, dependencies, events and actors
    And I note the lease rows
    When I renew the lease "L9" without a body as actor "codex@laptop"
    Then the response status is 404
    And the counts of tasks, dependencies, events and actors are unchanged
    And the lease rows are unchanged
    And exactly 0 lease_expired events exist for the lease "L1"

  Scenario: a renewal of a lease that ran out is 410 and leaves its own row and its events alone
    Given the lease "L1" ran out 30 seconds ago
    And I note the counts of tasks, dependencies, events and actors
    And I note the lease rows
    When I renew the lease "L1" without a body as actor "claude@laptop"
    Then the response status is 410
    And the counts of tasks, dependencies, events and actors are unchanged
    And the lease rows are unchanged
    And the lease "L1" is "live" in the database

  Scenario: ending an ended lease is refused and the unrelated due lease stays live
    Given I end the lease "L2" without a body as actor "claude@laptop"
    And the lease "L1" ran out 30 seconds ago
    And I note the counts of tasks, dependencies, events and actors
    And I note the lease rows
    When I end the lease "L2" without a body as actor "codex@laptop"
    Then the response status is 409
    And the counts of tasks, dependencies, events and actors are unchanged
    And the lease rows are unchanged
    And exactly 0 lease_expired events exist for the lease "L1"

  Scenario: ending a lease that ran out is refused and leaves it untouched
    Given the lease "L1" ran out 30 seconds ago
    And I note the counts of tasks, dependencies, events and actors
    And I note the lease rows
    When I end the lease "L1" without a body as actor "codex@laptop"
    Then the response status is 409
    And the response path "detail" is "cannot end lease L1: it is expired"
    And the counts of tasks, dependencies, events and actors are unchanged
    And the lease rows are unchanged

  Scenario: acknowledging a live lease is refused and the unrelated due lease stays live
    Given the lease "L1" ran out 30 seconds ago
    And I note the counts of tasks, dependencies, events and actors
    And I note the lease rows
    When I acknowledge the lease "L2" as actor "operator@laptop"
    Then the response status is 409
    And the counts of tasks, dependencies, events and actors are unchanged
    And the lease rows are unchanged
    And exactly 0 lease_expired events exist for the lease "L1"

  Scenario: acknowledging an ended lease is refused and the unrelated due lease stays live
    Given I end the lease "L2" without a body as actor "claude@laptop"
    And the lease "L1" ran out 30 seconds ago
    And I note the counts of tasks, dependencies, events and actors
    And I note the lease rows
    When I acknowledge the lease "L2" as actor "operator@laptop"
    Then the response status is 409
    And the counts of tasks, dependencies, events and actors are unchanged
    And the lease rows are unchanged

  Scenario: acknowledging an unknown lease is refused and the unrelated due lease stays live
    Given the lease "L1" ran out 30 seconds ago
    And I note the counts of tasks, dependencies, events and actors
    And I note the lease rows
    When I acknowledge the lease "L9" as actor "operator@laptop"
    Then the response status is 404
    And the counts of tasks, dependencies, events and actors are unchanged
    And the lease rows are unchanged

  Scenario: a successful start expires the unrelated due lease with exactly one event
    Given the lease "L1" ran out 30 seconds ago
    When I start a lease with body "{'task':'aa-2','intent':'second'}" as actor "codex@laptop"
    Then the response status is 201
    And the lease "L1" is "expired" in the database
    And exactly 1 lease_expired event exists for the lease "L1"

  Scenario: a successful renewal expires the unrelated due lease with exactly one event
    Given the lease "L1" ran out 30 seconds ago
    When I renew the lease "L2" without a body as actor "claude@laptop"
    Then the response status is 200
    And the lease "L1" is "expired" in the database
    And exactly 1 lease_expired event exists for the lease "L1"

  Scenario: a successful end expires the unrelated due lease with exactly one event
    Given the lease "L1" ran out 30 seconds ago
    When I end the lease "L2" without a body as actor "claude@laptop"
    Then the response status is 200
    And the lease "L1" is "expired" in the database
    And exactly 1 lease_expired event exists for the lease "L1"

  Scenario: a successful acknowledgement of the due lease writes one lease_expired and one lease_acked
    Given the lease "L1" ran out 30 seconds ago
    When I acknowledge the lease "L1" as actor "operator@laptop"
    Then the response status is 200
    And the lease "L1" is "expired" in the database
    And exactly 1 lease_expired event exists for the lease "L1"
    And exactly 1 lease_acked event exists for the lease "L1"

  Scenario: a read after a refused write still expires the lease once
    Given I end the lease "L2" without a body as actor "claude@laptop"
    And the lease "L1" ran out 30 seconds ago
    When I end the lease "L2" without a body as actor "codex@laptop"
    Then the response status is 409
    And the lease "L1" is "live" in the database
    When I request the lease "L1" as actor "claude@laptop"
    Then the response path "state" is "expired"
    And exactly 1 lease_expired event exists for the lease "L1"

  # The key a refused request authenticates with is never used before it: the usage metadata
  # authentication keeps (api_keys.last_used_at) commits only with a request that succeeds.

  Scenario Outline: a refused <what> with a never-used key changes no row in any table
    Given I end the lease "L2" without a body as actor "claude@laptop"
    And the lease "L1" ran out 30 seconds ago
    And I run "vh admin keys create fresh"
    And I remember the key stdout printed
    And the key "fresh" has never been used
    And I note every row of every table
    When <request>
    Then the response status is <status>
    And every row of every table is unchanged
    And the key "fresh" has never been used
    And the lease "L1" is "live" in the database
    And exactly 0 lease_expired events exist for the lease "L1"

    Examples:
      | what                              | request                                                                | status |
      | start on an unknown task          | I start a lease with body "{'task':'aa-9','intent':'x'}" as actor "codex@laptop" | 404    |
      | renewal of an ended lease         | I renew the lease "L2" without a body as actor "codex@laptop"          | 409    |
      | renewal of an unknown lease       | I renew the lease "L9" without a body as actor "codex@laptop"          | 404    |
      | end of an ended lease             | I end the lease "L2" without a body as actor "codex@laptop"            | 409    |
      | end of an unknown lease           | I end the lease "L9" without a body as actor "codex@laptop"            | 404    |
      | acknowledgement of an ended lease | I acknowledge the lease "L2" as actor "operator@laptop"                | 409    |
      | acknowledgement of an unknown lease | I acknowledge the lease "L9" as actor "operator@laptop"              | 404    |

  Scenario: a refused start on a closed task with a never-used key changes no row
    Given I close the task "aa-3" with body "{'resolution':'dropped'}" as actor "claude@laptop"
    And the lease "L1" ran out 30 seconds ago
    And I run "vh admin keys create fresh"
    And I remember the key stdout printed
    And I note every row of every table
    When I start a lease with body "{'task':'aa-3','intent':'x'}" as actor "codex@laptop"
    Then the response status is 409
    And every row of every table is unchanged

  Scenario: a refusal does not suppress the key's usage bump on its next successful request
    Given the lease "L1" ran out 30 seconds ago
    And I run "vh admin keys create fresh"
    And I remember the key stdout printed
    When I renew the lease "L9" without a body as actor "codex@laptop"
    Then the response status is 404
    And the key "fresh" has never been used
    When I renew the lease "L2" without a body as actor "claude@laptop"
    Then the response status is 200
    And the key "fresh" has been used
    And the lease "L1" is "expired" in the database
    And exactly 1 lease_expired event exists for the lease "L1"
