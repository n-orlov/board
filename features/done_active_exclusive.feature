Feature: A task is never done or closed while it has a live lease
  Done and active are exclusive (R14). A lease starts only on an open task, so a done task and a closed task are
  409 and nothing changes. Marking a task done ends the caller's own live lease in the same transaction, with the
  outcome sent or `done`. Another actor's live lease makes done a 409 naming the holder, the lease and its intent,
  and nothing changes, unless the request sends force, which ends every live lease, the caller's own too, with
  ended_by the caller. Closing a task ends every live lease on it the same way.

  Background:
    Given a new database for the board commands
    When I run "board admin migrate"
    And I run "board admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port
    And I create the initiative "aa" titled "First" with no tags as actor "claude@laptop"
    And I create a task in "aa" titled "one" as actor "claude@laptop"

  Scenario: starting a lease on a done task is 409 and changes nothing
    Given I mark the task "aa-1" done as actor "claude@laptop"
    And I note the counts of tasks, dependencies, events and actors
    When I start a lease with body "{'task':'aa-1','intent':'x'}" as actor "codex@laptop"
    Then the response status is 409
    And the response path "detail" is "cannot start a lease on task aa-1: it is done"
    And the response path "next" is "Run board task reopen aa-1 first, or start the lease on an open task."
    And there are 0 leases in the database
    And the counts of tasks, dependencies, events and actors are unchanged

  Scenario: starting a lease on a closed task is 409 and changes nothing
    Given I close the task "aa-1" with body "{'resolution':'dropped'}" as actor "claude@laptop"
    And I note the counts of tasks, dependencies, events and actors
    When I start a lease with body "{'task':'aa-1','intent':'x'}" as actor "codex@laptop"
    Then the response status is 409
    And the response path "detail" is "cannot start a lease on task aa-1: it is closed"
    And there are 0 leases in the database
    And the counts of tasks, dependencies, events and actors are unchanged

  Scenario: done by the lease holder ends its lease with the outcome given
    Given I start a lease with body "{'task':'aa-1','intent':'build it'}" as actor "claude@laptop"
    When I mark the task "aa-1" done with body "{'outcome':'shipped the thing'}" as actor "claude@laptop"
    Then the response status is 200
    And the response path "status" is "done"
    And the response path "active" is "false"
    And the response path "live_leases" has 0 items
    When I request the lease "L1" as actor "claude@laptop"
    Then the response path "state" is "ended"
    And the response path "outcome" is "shipped the thing"
    And the response path "ended_by" is absent
    And the response path "ended_at" is present
    And exactly 1 lease_ended event exists for the lease "L1"
    And exactly 1 task_done event exists for the task "aa-1"

  Scenario: done by the lease holder with no outcome ends its lease with the outcome done
    Given I start a lease with body "{'task':'aa-1','intent':'build it'}" as actor "claude@laptop"
    When I mark the task "aa-1" done as actor "claude@laptop"
    Then the response status is 200
    And the response path "live_leases" has 0 items
    When I request the lease "L1" as actor "claude@laptop"
    Then the response path "state" is "ended"
    And the response path "outcome" is "done"
    And the response path "ended_by" is absent

  Scenario: done while another actor holds a live lease is 409 naming it and changes nothing
    Given I start a lease with body "{'task':'aa-1','intent':'refactor the parser'}" as actor "codex@laptop"
    And I note the counts of tasks, dependencies, events and actors
    And I note the lease rows
    When I mark the task "aa-1" done as actor "claude@laptop"
    Then the response status is 409
    And the problem detail contains "codex@laptop"
    And the problem detail contains "L1"
    And the problem detail contains "refactor the parser"
    And the response path "next" is "wait for L1 to end, or --force"
    And the counts of tasks, dependencies, events and actors are unchanged
    And the lease rows are unchanged
    And the lease "L1" is "live" in the database
    When I request the task "aa-1" as actor "claude@laptop"
    Then the response path "status" is "open"

  Scenario: done with force ends every live lease, the caller's own included, and marks the task done
    Given I start a lease with body "{'task':'aa-1','intent':'refactor the parser'}" as actor "codex@laptop"
    And I start a lease with body "{'task':'aa-1','intent':'build it'}" as actor "claude@laptop"
    When I mark the task "aa-1" done with body "{'outcome':'shipped','force':true}" as actor "claude@laptop"
    Then the response status is 200
    And the response path "status" is "done"
    And the response path "active" is "false"
    And the response path "live_leases" has 0 items
    When I request the lease "L1" as actor "claude@laptop"
    Then the response path "state" is "ended"
    And the response path "ended_by" is "claude@laptop"
    And the response path "outcome" is "ended: task marked done by claude@laptop"
    And exactly 1 lease_ended event exists for the lease "L1"
    When I request the lease "L2" as actor "claude@laptop"
    Then the response path "state" is "ended"
    And the response path "ended_by" is "claude@laptop"
    And the response path "outcome" is "ended: task marked done by claude@laptop"
    And exactly 1 lease_ended event exists for the lease "L2"

  Scenario: close ends every live lease of the task, the caller's own included
    Given I start a lease with body "{'task':'aa-1','intent':'refactor the parser'}" as actor "codex@laptop"
    And I start a lease with body "{'task':'aa-1','intent':'build it'}" as actor "claude@laptop"
    When I close the task "aa-1" with body "{'resolution':'dropped'}" as actor "claude@laptop"
    Then the response status is 200
    And the response path "status" is "closed"
    And the response path "active" is "false"
    And the response path "live_leases" has 0 items
    When I request the lease "L1" as actor "claude@laptop"
    Then the response path "state" is "ended"
    And the response path "ended_by" is "claude@laptop"
    And the response path "outcome" is "ended: task closed by claude@laptop"
    And exactly 1 lease_ended event exists for the lease "L1"
    When I request the lease "L2" as actor "claude@laptop"
    Then the response path "state" is "ended"
    And the response path "ended_by" is "claude@laptop"
    And the response path "outcome" is "ended: task closed by claude@laptop"
    And exactly 1 lease_ended event exists for the lease "L2"

  Scenario: done after another actor's lease has ended is allowed and leaves no live lease
    Given I start a lease with body "{'task':'aa-1','intent':'refactor the parser'}" as actor "codex@laptop"
    And I end the lease "L1" with body "{'outcome':'gave up'}" as actor "codex@laptop"
    When I mark the task "aa-1" done as actor "claude@laptop"
    Then the response status is 200
    And the response path "status" is "done"
    And the response path "live_leases" has 0 items
    When I request the lease "L1" as actor "claude@laptop"
    Then the response path "state" is "ended"
    And the response path "outcome" is "gave up"
    And exactly 1 task_done event exists for the task "aa-1"

  Scenario: after a forced done a new lease start is refused and no lease is live
    Given I start a lease with body "{'task':'aa-1','intent':'refactor the parser'}" as actor "codex@laptop"
    And I mark the task "aa-1" done with body "{'force':true}" as actor "claude@laptop"
    And I note the counts of tasks, dependencies, events and actors
    When I start a lease with body "{'task':'aa-1','intent':'again'}" as actor "codex@laptop"
    Then the response status is 409
    And the response path "detail" is "cannot start a lease on task aa-1: it is done"
    And there are 1 leases in the database
    And the lease "L1" is "ended" in the database
    And the counts of tasks, dependencies, events and actors are unchanged
