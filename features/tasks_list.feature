Feature: Listing tasks over HTTP
  GET /api/v1/tasks pages tasks in id order, each with its live leases. It filters by
  initiative (repeatable), initiative tag (repeatable, every tag must match), status
  (open, done, closed, not_closed by default, all), kind, the derived states active,
  blocked and overdue, closed_within and a case-insensitive text q. It expires leases
  that ran out before it answers, so a task is never active on a lease past its expiry.

  Background:
    Given a new database for the board commands
    When I run "board admin migrate"
    And I run "board admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port with BOARD_SWEEP_INTERVAL=off
    And I create the initiative "aa" titled "First" with tags "home,lab" as actor "claude@laptop"
    And I create the initiative "bb" titled "Second" with tags "home" as actor "claude@laptop"
    And I create the initiative "cc" titled "Third" with no tags as actor "claude@laptop"

  Scenario: an empty board lists no tasks
    When I list the tasks as actor "claude@laptop"
    Then the response status is 200
    And the response path "items" has 0 items
    And the response path "next_after" is absent

  Scenario: tasks come back in id order with their fields
    Given I create a task in "bb" titled "first made" as actor "claude@laptop"
    And I create a task in "aa" titled "second made" as actor "claude@laptop"
    And I create a task in "cc" titled "third made" as actor "claude@laptop"
    When I list the tasks as actor "claude@laptop"
    Then the response status is 200
    And the response path "items" lists the refs "bb-1,aa-1,cc-1"
    And the response path "items.0.title" is "first made"
    And the response path "items.0.initiative" is "bb"
    And the response path "items.0.kind" is "work"
    And the response path "items.0.status" is "open"
    And the response path "items.0.active" is "false"
    And the response path "items.0.blocked" is "false"
    And the response path "items.0.overdue" is "false"
    And the response path "items.0.live_leases" has 0 items

  Scenario: the initiative filter takes one initiative, a repeated one and an id
    Given I create a task in "aa" titled "a one" as actor "claude@laptop"
    And I create a task in "bb" titled "b one" as actor "claude@laptop"
    And I create a task in "cc" titled "c one" as actor "claude@laptop"
    And I create a task in "aa" titled "a two" as actor "claude@laptop"
    When I list the tasks with "initiative=aa" as actor "claude@laptop"
    Then the response path "items" lists the refs "aa-1,aa-2"
    When I list the tasks with "initiative=aa&initiative=cc" as actor "claude@laptop"
    Then the response path "items" lists the refs "aa-1,cc-1,aa-2"
    When I list the tasks with "initiative=AA" as actor "claude@laptop"
    Then the response path "items" lists the refs "aa-1,aa-2"

  Scenario: an initiative that does not exist is 404
    When I list the tasks with "initiative=zz" as actor "claude@laptop"
    Then the response status is 404
    And the problem detail contains "zz"
    And the problem has a next field containing "initiatives"

  Scenario: the tag filter needs every tag to match
    Given I create a task in "aa" titled "a one" as actor "claude@laptop"
    And I create a task in "bb" titled "b one" as actor "claude@laptop"
    And I create a task in "cc" titled "c one" as actor "claude@laptop"
    When I list the tasks with "tag=home" as actor "claude@laptop"
    Then the response path "items" lists the refs "aa-1,bb-1"
    When I list the tasks with "tag=home&tag=lab" as actor "claude@laptop"
    Then the response path "items" lists the refs "aa-1"
    When I list the tasks with "tag=lab&tag=nothing" as actor "claude@laptop"
    Then the response path "items" has 0 items
    When I list the tasks with "tag=home&initiative=bb" as actor "claude@laptop"
    Then the response path "items" lists the refs "bb-1"

  Scenario: a bad tag is 400
    When I list the tasks with "tag=Bad%20Tag!" as actor "claude@laptop"
    Then the response status is 400
    And the problem has a next field containing "tags"

  Scenario: the status filter keeps open, done, closed, not closed or all
    Given I create a task in "aa" titled "stays open" as actor "claude@laptop"
    And I create a task in "aa" titled "gets done" as actor "claude@laptop"
    And I create a task in "aa" titled "gets closed" as actor "claude@laptop"
    And I mark the task "aa-2" done as actor "claude@laptop"
    And I close the task "aa-3" with body "{'resolution':'dropped'}" as actor "claude@laptop"
    When I list the tasks as actor "claude@laptop"
    Then the response path "items" lists the refs "aa-1,aa-2"
    When I list the tasks with "status=not_closed" as actor "claude@laptop"
    Then the response path "items" lists the refs "aa-1,aa-2"
    When I list the tasks with "status=open" as actor "claude@laptop"
    Then the response path "items" lists the refs "aa-1"
    When I list the tasks with "status=done" as actor "claude@laptop"
    Then the response path "items" lists the refs "aa-2"
    When I list the tasks with "status=closed" as actor "claude@laptop"
    Then the response path "items" lists the refs "aa-3"
    And the response path "items.0.resolution" is "dropped"
    When I list the tasks with "status=all" as actor "claude@laptop"
    Then the response path "items" lists the refs "aa-1,aa-2,aa-3"

  Scenario: a status that is not a filter is 400
    When I list the tasks with "status=finished" as actor "claude@laptop"
    Then the response status is 400
    And the problem detail contains "finished"
    And the problem has a next field containing "not_closed"

  Scenario: the kind filter keeps work or watch tasks
    Given I create a task in "aa" titled "plain" as actor "claude@laptop"
    And I create a watch task in "aa" titled "watcher" with a cadence of 3600 seconds as actor "claude@laptop"
    When I list the tasks with "kind=work" as actor "claude@laptop"
    Then the response path "items" lists the refs "aa-1"
    When I list the tasks with "kind=watch" as actor "claude@laptop"
    Then the response path "items" lists the refs "aa-2"
    And the response path "items.0.cadence_seconds" is "3600"
    When I list the tasks with "kind=chore" as actor "claude@laptop"
    Then the response status is 400

  Scenario: the active filter follows live leases
    Given I create a task in "aa" titled "leased" as actor "claude@laptop"
    And I create a task in "aa" titled "free" as actor "claude@laptop"
    And I start a lease with body "{'task':'aa-1','intent':'working'}" as actor "claude@laptop"
    When I list the tasks with "active=true" as actor "claude@laptop"
    Then the response path "items" lists the refs "aa-1"
    And the response path "items.0.active" is "true"
    And the response path "items.0.live_leases" has 1 items
    And the response path "items.0.live_leases.0.ref" is "L1"
    And the response path "items.0.live_leases.0.intent" is "working"
    When I list the tasks with "active=false" as actor "claude@laptop"
    Then the response path "items" lists the refs "aa-2"
    And the response path "items.0.active" is "false"

  Scenario: the blocked filter follows open dependencies
    Given I create a task in "aa" titled "blocker" as actor "claude@laptop"
    And I create a task in "aa" titled "waiting" as actor "claude@laptop"
    And I create a task in "aa" titled "free" as actor "claude@laptop"
    And I make the task "aa-2" blocked by "aa-1" as actor "claude@laptop"
    When I list the tasks with "blocked=true" as actor "claude@laptop"
    Then the response path "items" lists the refs "aa-2"
    And the response path "items.0.blocked" is "true"
    When I list the tasks with "blocked=false" as actor "claude@laptop"
    Then the response path "items" lists the refs "aa-1,aa-3"
    Given I close the task "aa-1" with body "{'resolution':'completed'}" as actor "claude@laptop"
    When I list the tasks with "blocked=true" as actor "claude@laptop"
    Then the response path "items" has 0 items
    When I list the tasks with "blocked=false" as actor "claude@laptop"
    Then the response path "items" lists the refs "aa-2,aa-3"

  Scenario: the overdue filter follows next_check_at of open watch tasks
    Given I create the task with body "{'initiative':'aa','title':'late','kind':'watch','cadence_seconds':60,'next_check_at':'2020-01-01T00:00:00Z'}" as actor "claude@laptop"
    And I create the task with body "{'initiative':'aa','title':'on time','kind':'watch','cadence_seconds':60,'next_check_at':'2999-01-01T00:00:00Z'}" as actor "claude@laptop"
    And I create a task in "aa" titled "plain work" as actor "claude@laptop"
    When I list the tasks with "overdue=true" as actor "claude@laptop"
    Then the response path "items" lists the refs "aa-1"
    And the response path "items.0.overdue" is "true"
    When I list the tasks with "overdue=false" as actor "claude@laptop"
    Then the response path "items" lists the refs "aa-2,aa-3"
    Given I close the task "aa-1" with body "{'resolution':'dropped'}" as actor "claude@laptop"
    When I list the tasks with "overdue=true&status=all" as actor "claude@laptop"
    Then the response path "items" has 0 items

  Scenario: a flag that is not true or false is 400
    When I list the tasks with "active=maybe" as actor "claude@laptop"
    Then the response status is 400

  Scenario Outline: closed_within keeps the tasks closed inside the window
    Given I create a task in "aa" titled "closed recently" as actor "claude@laptop"
    And I create a task in "aa" titled "closed a while ago" as actor "claude@laptop"
    And I create a task in "aa" titled "closed long ago" as actor "claude@laptop"
    And I create a task in "aa" titled "still open" as actor "claude@laptop"
    And I close the task "aa-1" with body "{'resolution':'completed'}" as actor "claude@laptop"
    And I close the task "aa-2" with body "{'resolution':'completed'}" as actor "claude@laptop"
    And I close the task "aa-3" with body "{'resolution':'completed'}" as actor "claude@laptop"
    And the task "aa-2" was closed 20 days ago
    And the task "aa-3" was closed 60 days ago
    When I list the tasks with "status=closed&closed_within=<within>" as actor "claude@laptop"
    Then the response path "items" lists the refs "<closed>"
    When I list the tasks with "status=all&closed_within=<within>" as actor "claude@laptop"
    Then the response path "items" lists the refs "<all>"

    Examples:
      | within | closed        | all                |
      | 7d     | aa-1          | aa-1,aa-4          |
      | 30d    | aa-1,aa-2     | aa-1,aa-2,aa-4     |
      | 90d    | aa-1,aa-2,aa-3 | aa-1,aa-2,aa-3,aa-4 |
      | 25     | aa-1,aa-2     | aa-1,aa-2,aa-4     |
      | 10     | aa-1          | aa-1,aa-4          |

  Scenario Outline: a closed_within that cannot apply is 400
    When I list the tasks with "<query>" as actor "claude@laptop"
    Then the response status is 400
    And the problem has a next field containing "closed_within"

    Examples:
      | query                              |
      | status=closed&closed_within=1w     |
      | status=closed&closed_within=0      |
      | status=closed&closed_within=-3     |
      | status=closed&closed_within=40000  |
      | closed_within=7d                   |
      | status=open&closed_within=7d       |

  Scenario: q matches the title or the description in any letter case
    Given I create the task with body "{'initiative':'aa','title':'Water the Plants'}" as actor "claude@laptop"
    And I create the task with body "{'initiative':'aa','title':'Other','description':'remember the WATERING can'}" as actor "claude@laptop"
    And I create the task with body "{'initiative':'aa','title':'Unrelated','description':'nothing here'}" as actor "claude@laptop"
    When I list the tasks with "q=water" as actor "claude@laptop"
    Then the response path "items" lists the refs "aa-1,aa-2"
    When I list the tasks with "q=PLANTS" as actor "claude@laptop"
    Then the response path "items" lists the refs "aa-1"
    When I list the tasks with "q=watering%20can" as actor "claude@laptop"
    Then the response path "items" lists the refs "aa-2"
    When I list the tasks with "q=no%20such%20text" as actor "claude@laptop"
    Then the response path "items" has 0 items

  Scenario: q treats percent and underscore as plain text
    Given I create the task with body "{'initiative':'aa','title':'100% done_ish'}" as actor "claude@laptop"
    And I create the task with body "{'initiative':'aa','title':'1000 doneXish'}" as actor "claude@laptop"
    When I list the tasks with "q=0%25%20done_" as actor "claude@laptop"
    Then the response path "items" lists the refs "aa-1"

  Scenario: filters combine
    Given I create a task in "aa" titled "alpha job" as actor "claude@laptop"
    And I create a task in "bb" titled "alpha job" as actor "claude@laptop"
    And I create a task in "aa" titled "beta job" as actor "claude@laptop"
    And I start a lease with body "{'task':'bb-1','intent':'x'}" as actor "claude@laptop"
    When I list the tasks with "q=alpha&tag=lab" as actor "claude@laptop"
    Then the response path "items" lists the refs "aa-1"
    When I list the tasks with "q=alpha&active=true" as actor "claude@laptop"
    Then the response path "items" lists the refs "bb-1"
    When I list the tasks with "q=job&initiative=aa&active=false&kind=work&status=open" as actor "claude@laptop"
    Then the response path "items" lists the refs "aa-1,aa-2"

  Scenario: limit and after page through the tasks in id order
    Given I create a task in "bb" titled "t1" as actor "claude@laptop"
    And I create a task in "aa" titled "t2" as actor "claude@laptop"
    And I create a task in "cc" titled "t3" as actor "claude@laptop"
    And I create a task in "aa" titled "t4" as actor "claude@laptop"
    And I create a task in "bb" titled "t5" as actor "claude@laptop"
    When I list the tasks with "limit=2" as actor "claude@laptop"
    Then the response path "items" lists the refs "bb-1,aa-1"
    When I request the next tasks page with "limit=2" as actor "claude@laptop"
    Then the response path "items" lists the refs "cc-1,aa-2"
    When I request the next tasks page with "limit=2" as actor "claude@laptop"
    Then the response path "items" lists the refs "bb-2"
    And the response path "next_after" is absent

  Scenario: after takes a ref in either letter case or an id
    Given I create a task in "aa" titled "t1" as actor "claude@laptop"
    And I create a task in "aa" titled "t2" as actor "claude@laptop"
    And I create a task in "aa" titled "t3" as actor "claude@laptop"
    When I list the tasks with "after=aa-1" as actor "claude@laptop"
    Then the response path "items" lists the refs "aa-2,aa-3"
    When I list the tasks with "after=AA-2" as actor "claude@laptop"
    Then the response path "items" lists the refs "aa-3"
    When I list the tasks with "after=aa-3" as actor "claude@laptop"
    Then the response path "items" has 0 items
    When I list the tasks with "after=aa-99" as actor "claude@laptop"
    Then the response status is 400
    And the problem has a next field containing "next_after"
    When I list the tasks with "after=%21%21" as actor "claude@laptop"
    Then the response status is 400

  Scenario Outline: a limit out of range is 400
    When I list the tasks with "limit=<limit>" as actor "claude@laptop"
    Then the response status is 400
    And the problem has a next field containing "limit"

    Examples:
      | limit |
      | 0     |
      | -1    |
      | 1001  |

  Scenario: the largest page size is accepted
    When I list the tasks with "limit=1000" as actor "claude@laptop"
    Then the response status is 200

  Scenario: a lease past its expiry is expired by the list and makes the task inactive
    Given I create a task in "aa" titled "leased" as actor "claude@laptop"
    And I start a lease with body "{'task':'aa-1','intent':'first'}" as actor "claude@laptop"
    And the lease "L1" ran out 30 seconds ago
    Then the lease "L1" is "live" in the database
    When I list the tasks as actor "claude@laptop"
    Then the response status is 200
    And the response path "items" lists the refs "aa-1"
    And the response path "items.0.live_leases" has 0 items
    And the response path "items.0.active" is "false"
    When I list the tasks with "active=true" as actor "claude@laptop"
    Then the response path "items" has 0 items
    And the lease "L1" is "expired" in the database
    And exactly 1 lease_expired event exists for the lease "L1"
    And exactly 1 lease_expired event exists for the task "aa-1"
