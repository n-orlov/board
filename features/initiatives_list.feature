Feature: Listing initiatives with filters, task counts and live leases
  GET /api/v1/initiatives keeps the open initiatives by default and takes status (open,
  closed, all) and closed_within (7d, 30d, 90d or a number of days, with status closed or
  all). Each item carries counts of its tasks by status and its live_leases. The list
  expires the leases that ran out before it answers.

  Background:
    Given a new database for the board commands
    When I run "board admin migrate"
    And I run "board admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port with BOARD_SWEEP_INTERVAL=off
    And I create the initiative "aa" titled "First" with tags "home" as actor "claude@laptop"
    And I create the initiative "bb" titled "Second" with tags "home" as actor "claude@laptop"
    And I create the initiative "cc" titled "Third" with no tags as actor "claude@laptop"
    And I create the initiative "dd" titled "Fourth" with no tags as actor "claude@laptop"

  Scenario: status open is the default and closed initiatives are left out
    Given I close the initiative "bb" without a body as actor "claude@laptop"
    When I list the initiatives as actor "claude@laptop"
    Then the response status is 200
    And the response path "items" lists the refs "aa,cc,dd"
    When I list the initiatives with "status=open" as actor "claude@laptop"
    Then the response path "items" lists the refs "aa,cc,dd"

  Scenario: status closed keeps only the closed initiatives and all keeps both
    Given I close the initiative "bb" without a body as actor "claude@laptop"
    And I close the initiative "dd" without a body as actor "claude@laptop"
    When I list the initiatives with "status=closed" as actor "claude@laptop"
    Then the response path "items" lists the refs "bb,dd"
    And the response path "items.0.status" is "closed"
    When I list the initiatives with "status=all" as actor "claude@laptop"
    Then the response path "items" lists the refs "aa,bb,cc,dd"
    And the response path "items.0.status" is "open"
    And the response path "items.1.status" is "closed"

  Scenario: the status filter combines with tag and paging
    Given I close the initiative "aa" without a body as actor "claude@laptop"
    When I list the initiatives with "status=all&tag=home" as actor "claude@laptop"
    Then the response path "items" lists the refs "aa,bb"
    When I list the initiatives with "tag=home" as actor "claude@laptop"
    Then the response path "items" lists the refs "bb"
    When I list the initiatives with "status=all&limit=2" as actor "claude@laptop"
    Then the response path "items" lists the refs "aa,bb"
    When I request the next initiatives page with "status=all&limit=2" as actor "claude@laptop"
    Then the response path "items" lists the refs "cc,dd"

  Scenario Outline: closed_within keeps the initiatives closed inside the window
    Given I close the initiative "aa" without a body as actor "claude@laptop"
    And I close the initiative "bb" without a body as actor "claude@laptop"
    And I close the initiative "cc" without a body as actor "claude@laptop"
    And the initiative "bb" was closed 20 days ago
    And the initiative "cc" was closed 60 days ago
    When I list the initiatives with "status=closed&closed_within=<within>" as actor "claude@laptop"
    Then the response path "items" lists the refs "<closed>"
    When I list the initiatives with "status=all&closed_within=<within>" as actor "claude@laptop"
    Then the response path "items" lists the refs "<all>"

    Examples:
      | within | closed      | all            |
      | 7d     | aa          | aa,dd          |
      | 30d    | aa,bb       | aa,bb,dd       |
      | 90d    | aa,bb,cc    | aa,bb,cc,dd    |
      | 25     | aa,bb       | aa,bb,dd       |
      | 10     | aa          | aa,dd          |

  Scenario Outline: a status or closed_within that cannot apply is 400
    When I list the initiatives with "<query>" as actor "claude@laptop"
    Then the response status is 400
    And the problem has a next field containing "<next>"

    Examples:
      | query                            | next          |
      | closed_within=7d                 | closed_within |
      | status=open&closed_within=7d     | closed_within |
      | status=closed&closed_within=1w   | closed_within |
      | status=closed&closed_within=0    | closed_within |
      | status=all&closed_within=40000   | closed_within |
      | status=done                      | status        |
      | status=ALL                       | status        |

  Scenario: counts show the tasks of each initiative by status
    Given I create a task in "aa" titled "open one" as actor "claude@laptop"
    And I create a task in "aa" titled "done one" as actor "claude@laptop"
    And I create a task in "aa" titled "closed one" as actor "claude@laptop"
    And I create a task in "aa" titled "open two" as actor "claude@laptop"
    And I create a task in "bb" titled "only" as actor "claude@laptop"
    And I mark the task "aa-2" done as actor "claude@laptop"
    And I close the task "aa-3" with body "{'resolution':'dropped'}" as actor "claude@laptop"
    When I list the initiatives as actor "claude@laptop"
    Then the response path "items" lists the refs "aa,bb,cc,dd"
    And the response path "items.0.counts.open" is "2"
    And the response path "items.0.counts.done" is "1"
    And the response path "items.0.counts.closed" is "1"
    And the response path "items.1.counts.open" is "1"
    And the response path "items.1.counts.done" is "0"
    And the response path "items.1.counts.closed" is "0"
    And the response path "items.2.counts.open" is "0"
    And the response path "items.2.counts.done" is "0"
    And the response path "items.2.counts.closed" is "0"

  Scenario: live_leases counts the live leases on the tasks of each initiative
    Given I create a task in "aa" titled "one" as actor "claude@laptop"
    And I create a task in "aa" titled "two" as actor "claude@laptop"
    And I create a task in "bb" titled "three" as actor "claude@laptop"
    And I start a lease with body "{'task':'aa-1','intent':'first'}" as actor "claude@laptop"
    And I start a lease with body "{'task':'aa-2','intent':'second'}" as actor "claude@laptop"
    And I start a lease with body "{'task':'bb-1','intent':'third'}" as actor "claude@laptop"
    When I list the initiatives as actor "claude@laptop"
    Then the response path "items.0.live_leases" is "2"
    And the response path "items.1.live_leases" is "1"
    And the response path "items.2.live_leases" is "0"
    When I end the lease "L3" without a body as actor "claude@laptop"
    And I list the initiatives as actor "claude@laptop"
    Then the response path "items.0.live_leases" is "2"
    And the response path "items.1.live_leases" is "0"

  Scenario: an initiative whose only lease ran out has no live lease and one lease_expired event
    Given I create a task in "aa" titled "leased" as actor "claude@laptop"
    And I start a lease with body "{'task':'aa-1','intent':'first'}" as actor "claude@laptop"
    And the lease "L1" ran out 30 seconds ago
    Then the lease "L1" is "live" in the database
    When I list the initiatives as actor "claude@laptop"
    Then the response status is 200
    And the response path "items.0.live_leases" is "0"
    And the lease "L1" is "expired" in the database
    And exactly 1 lease_expired event exists for the lease "L1"
    When I list the initiatives as actor "claude@laptop"
    Then the response path "items.0.live_leases" is "0"
    And exactly 1 lease_expired event exists for the lease "L1"
