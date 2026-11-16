Feature: Renewing every lease in one request, POST /api/v1/ping
  POST /api/v1/ping renews every live lease the caller holds and, with progress, writes one milestone with that
  summary on each leased task. The renewals, the milestones and their events are written in one transaction, and a
  refused request writes nothing: no renewal, no milestone, no event.

  Background:
    Given a new database for the board commands
    When I run "vh admin migrate"
    And I run "vh admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port
    And I create the initiative "aa" titled "First" with no tags as actor "claude@laptop"
    And I create a task in "aa" titled "one" as actor "claude@laptop"
    And I create a task in "aa" titled "two" as actor "claude@laptop"
    And I create a task in "aa" titled "three" as actor "claude@laptop"

  Scenario Outline: no live lease is a conflict and writes nothing
    Given I start with body "{'task':'aa-1','intent':'theirs'}" as actor "codex@laptop"
    And I note the counts of events, notes and live leases
    When I ping with body "<body>" as actor "ghost@laptop"
    Then the response status is 409
    And the response path "detail" is "you hold no live lease"
    And the response path "next" is present
    And the counts of events, notes and live leases changed by 0, 0 and 0
    And exactly 0 lease_renewed events exist for the lease "L1"
    And exactly 0 milestone events exist for the task "aa-1"
    And the actor "ghost@laptop" has no row in actors

    Examples:
      | body                    |
      |                         |
      | {}                      |
      | {'progress':'half way'} |

  Scenario Outline: one live lease is renewed, with or without progress
    Given I start with body "{'task':'aa-1','intent':'work'}" as actor "claude@laptop"
    And I start with body "{'task':'aa-2','intent':'theirs'}" as actor "codex@laptop"
    And I note the counts of events, notes and live leases
    When I ping with body "<body>" as actor "claude@laptop"
    Then the response status is 200
    And the response path "leases" has 1 items
    And the response path "leases.0.ref" is "L1"
    And the response path "leases.0.task" is "aa-1"
    And the response path "leases.0.state" is "live"
    And the response path "leases.0.expires_at" is the expiry stored for the lease "L1"
    And the response path "milestones" has <milestones> items
    And the counts of events, notes and live leases changed by <events>, 0 and 0
    And exactly 1 lease_renewed event exists for the lease "L1"
    And exactly <milestones> milestone events exist for the task "aa-1"
    And exactly 0 lease_renewed events exist for the lease "L2"
    And exactly 0 milestone events exist for the task "aa-2"

    Examples:
      | body                    | events | milestones |
      |                         | 1      | 0          |
      | {}                      | 1      | 0          |
      | {'progress':'half way'} | 2      | 1          |

  Scenario Outline: three live leases are all renewed, with or without progress
    Given I start with body "{'task':'aa-1','intent':'one'}" as actor "claude@laptop"
    And I start with body "{'task':'aa-2','intent':'two'}" as actor "claude@laptop"
    And I start with body "{'task':'aa-3','intent':'three'}" as actor "claude@laptop"
    And I note the counts of events, notes and live leases
    When I ping with body "<body>" as actor "claude@laptop"
    Then the response status is 200
    And the response path "leases" has 3 items
    And the response path "leases.0.ref" is "L1"
    And the response path "leases.1.ref" is "L2"
    And the response path "leases.2.ref" is "L3"
    And the response path "leases.2.expires_at" is the expiry stored for the lease "L3"
    And the response path "milestones" has <milestones> items
    And the counts of events, notes and live leases changed by <events>, 0 and 0
    And exactly 1 lease_renewed event exists for the lease "L1"
    And exactly 1 lease_renewed event exists for the lease "L2"
    And exactly 1 lease_renewed event exists for the lease "L3"
    And exactly <each> milestone event exists for the task "aa-1"
    And exactly <each> milestone event exists for the task "aa-2"
    And exactly <each> milestone event exists for the task "aa-3"

    Examples:
      | body                    | events | milestones | each |
      |                         | 3      | 0          | 0    |
      | {'progress':'half way'} | 6      | 3          | 1    |

  Scenario: the milestone carries the progress as its summary
    Given I start with body "{'task':'aa-1','intent':'work'}" as actor "claude@laptop"
    When I ping with body "{'progress':'  tests green  '}" as actor "claude@laptop"
    Then the response status is 200
    And the response path "milestones.0" is "aa-1"
    And the task "aa-1" has a milestone event with the summary "tests green"

  Scenario Outline: a progress that is empty or too long is 400 and writes nothing
    Given I start with body "{'task':'aa-1','intent':'mine'}" as actor "claude@laptop"
    And I note the counts of events, notes and live leases
    When I ping with body "<body>" as actor "claude@laptop"
    Then the response status is 400
    And the response path "next" is present
    And the counts of events, notes and live leases changed by 0, 0 and 0
    And exactly 0 lease_renewed events exist for the lease "L1"

    Examples:
      | body                         |
      | {'progress':''}              |
      | {'progress':'   '}           |
      | {'progress':'<<x*201>>'}     |
