Feature: Ending or keeping the lease while raising an ask, POST /api/v1/asks with lease_action
  POST /api/v1/asks takes lease_action: none (the default), keep or stop. stop ends the caller's live lease on the
  task with the outcome "blocked on A<n>" in the same transaction as the ask, keep leaves the lease live and none
  leaves it alone. A stop with no live lease still raises the ask. An unknown value is a 400 and writes nothing.

  Background:
    Given a new database for the board commands
    When I run "vh admin migrate"
    And I run "vh admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port
    And I create the initiative "aa" titled "First" with no tags as actor "claude@laptop"
    And I create a task in "aa" titled "one" as actor "claude@laptop"
    And I create a task in "aa" titled "two" as actor "claude@laptop"

  Scenario: stop with a live lease ends it as blocked on the ask
    Given I start with body "{'task':'aa-1','intent':'work'}" as actor "claude@laptop"
    And I start with body "{'task':'aa-2','intent':'other work'}" as actor "claude@laptop"
    And I note the counts of asks, events and live leases
    When I raise an ask with body "{'task':'aa-1','question':'Which database?','lease_action':'stop'}" as actor "claude@laptop"
    Then the response status is 201
    And the response path "ref" is "A1"
    And the response path "state" is "open"
    And the response path "lease.ref" is "L1"
    And the response path "lease.task" is "aa-1"
    And the response path "lease.state" is "ended"
    And the response path "lease.outcome" is "blocked on A1"
    And the counts of asks, events and live leases changed by 1, 2 and -1
    And exactly 1 ask_raised event exists for the ask "A1"
    And exactly 1 lease_ended event exists for the lease "L1"
    And the lease "L1" is "ended" in the database
    And the lease "L1" has the outcome "blocked on A1" in the database
    And the lease "L2" is "live" in the database
    And exactly 0 lease_ended events exist for the lease "L2"

  Scenario: stop never ends another actor's lease and still raises the ask
    Given I start with body "{'task':'aa-1','intent':'theirs'}" as actor "codex@laptop"
    And I note the counts of asks, events and live leases
    When I raise an ask with body "{'task':'aa-1','question':'Which database?','lease_action':'stop'}" as actor "claude@laptop"
    Then the response status is 201
    And the response path "ref" is "A1"
    And the response path "lease" is absent
    And the counts of asks, events and live leases changed by 1, 1 and 0
    And exactly 1 ask_raised event exists for the ask "A1"
    And exactly 0 lease_ended events exist for the lease "L1"
    And the lease "L1" is "live" in the database

  Scenario: stop without any lease still raises the ask
    Given I note the counts of asks, events and live leases
    When I raise an ask with body "{'task':'aa-1','question':'Which database?','lease_action':'stop'}" as actor "claude@laptop"
    Then the response status is 201
    And the response path "ref" is "A1"
    And the response path "lease" is absent
    And the counts of asks, events and live leases changed by 1, 1 and 0
    And exactly 1 ask_raised event exists for the ask "A1"
    And there are 0 leases in the database

  Scenario: stop on an ask with no task ends nothing
    Given I start with body "{'task':'aa-1','intent':'work'}" as actor "claude@laptop"
    And I note the counts of asks, events and live leases
    When I raise an ask with body "{'initiative':'aa','question':'Ship it?','lease_action':'stop'}" as actor "claude@laptop"
    Then the response status is 201
    And the response path "lease" is absent
    And the counts of asks, events and live leases changed by 1, 1 and 0
    And the lease "L1" is "live" in the database

  Scenario: keep leaves the lease live and answers it
    Given I start with body "{'task':'aa-1','intent':'work'}" as actor "claude@laptop"
    And I note the counts of asks, events and live leases
    When I raise an ask with body "{'task':'aa-1','question':'Which database?','lease_action':'keep'}" as actor "claude@laptop"
    Then the response status is 201
    And the response path "ref" is "A1"
    And the response path "lease.ref" is "L1"
    And the response path "lease.state" is "live"
    And the response path "lease.outcome" is absent
    And the counts of asks, events and live leases changed by 1, 1 and 0
    And exactly 1 ask_raised event exists for the ask "A1"
    And exactly 0 lease_ended events exist for the lease "L1"
    And exactly 0 lease_renewed events exist for the lease "L1"
    And the lease "L1" is "live" in the database

  Scenario Outline: none, written or left out, leaves the lease alone
    Given I start with body "{'task':'aa-1','intent':'work'}" as actor "claude@laptop"
    And I note the counts of asks, events and live leases
    When I raise an ask with body "<body>" as actor "claude@laptop"
    Then the response status is 201
    And the response path "ref" is "A1"
    And the response path "lease" is absent
    And the counts of asks, events and live leases changed by 1, 1 and 0
    And the lease "L1" is "live" in the database

    Examples:
      | body                                                                         |
      | {'task':'aa-1','question':'Which database?'}                                 |
      | {'task':'aa-1','question':'Which database?','lease_action':'none'}           |

  Scenario Outline: an unknown lease_action is a 400 and writes nothing
    Given I start with body "{'task':'aa-1','intent':'work'}" as actor "claude@laptop"
    And I note the counts of asks, events and live leases
    When I raise an ask with body "{'task':'aa-1','question':'Which database?','lease_action':'<value>'}" as actor "ghost@laptop"
    Then the response status is 400
    And the response path "next" is present
    And the counts of asks, events and live leases changed by 0, 0 and 0
    And there are 0 asks in the database
    And the lease "L1" is "live" in the database
    And the actor "ghost@laptop" has no row in actors

    Examples:
      | value |
      | pause |
      | STOP  |
      | end   |

  Scenario: a stop that the ask itself refuses ends nothing
    Given I start with body "{'task':'aa-1','intent':'work'}" as actor "claude@laptop"
    And I note the counts of asks, events and live leases
    When I raise an ask with body "{'task':'aa-9','question':'Which database?','lease_action':'stop'}" as actor "claude@laptop"
    Then the response status is 404
    And the counts of asks, events and live leases changed by 0, 0 and 0
    And the lease "L1" is "live" in the database
