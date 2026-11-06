Feature: Listing events over HTTP
  GET /api/v1/events lists the audit trail in the order the events were written.
  The filters initiative, task, type (repeatable), actor and since combine.
  since is an event ULID, an E<n> ref or an RFC 3339 time. limit and after page the
  list, and next_after names the cursor of the following page. Each item is {id,
  ref, at, type, actor, principal, initiative?, task?, ref_id?, summary, data}.

  Background:
    Given a new database for the board commands
    When I run "board admin migrate"
    And I run "board admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port
    And I create the initiative "aa" titled "First" with no tags as actor "claude@laptop"
    And I create the initiative "bb" titled "Second" with no tags as actor "claude@laptop"
    And I create a task in "aa" titled "one" as actor "claude@laptop"
    And I create a task in "aa" titled "two" as actor "codex@laptop"
    And I create a task in "bb" titled "three" as actor "codex@laptop"
    And I mark the task "aa-1" done as actor "codex@laptop"
    And I record a milestone on the task "aa-2" with body "{'summary':'halfway','data':{'pct':50}}" as actor "claude@laptop"
    And I mark the task "bb-1" done as actor "claude@laptop"

  Scenario: with no filter every event is listed in the order it was written
    When I request the events list "" as actor "claude@laptop"
    Then the response status is 200
    And the response items are the events "1,2,3,4,5,6,7,8"
    And the response items have the types "initiative_created,initiative_created,task_created,task_created,task_created,task_done,milestone,task_done"
    And the response has no next_after

  Scenario: an item carries id, ref, at, type, actor, principal, summary and data
    When I request the events list "" as actor "claude@laptop"
    Then item 1 of the response is a complete event with "initiative"
    And item 1 of the response has no "task"
    And item 3 of the response is a complete event with "initiative,task,ref_id"
    And item 7 of the response is a complete event with "initiative,task,ref_id"
    And the response path "items.0.ref" is "E1"
    And the response path "items.0.type" is "initiative_created"
    And the response path "items.0.initiative" is "aa"
    And the response path "items.0.actor" is "claude@laptop"
    And the response path "items.2.task" is "aa-1"
    And the response path "items.2.summary" is "task aa-1 created"
    And the response path "items.6.data.data.pct" is "50"
    And the response path "items.6.summary" is "milestone on aa-2: halfway"

  Scenario: the initiative filter keeps the events of one initiative
    When I request the events list "initiative=aa" as actor "claude@laptop"
    Then the response items are the events "1,3,4,6,7"
    When I request the events list "initiative=bb" as actor "claude@laptop"
    Then the response items are the events "2,5,8"

  Scenario: an initiative that does not exist is 404
    When I request the events list "initiative=zz" as actor "claude@laptop"
    Then the response status is 404
    And the problem detail contains "zz"

  Scenario: the task filter keeps the events of one task
    When I request the events list "task=aa-1" as actor "claude@laptop"
    Then the response items are the events "3,6"
    When I request the events list "task=aa-2" as actor "claude@laptop"
    Then the response items are the events "4,7"

  Scenario: a task that does not exist is 404
    When I request the events list "task=aa-9" as actor "claude@laptop"
    Then the response status is 404
    And the problem detail contains "aa-9"

  Scenario: the type filter keeps one type, and repeated it keeps any of the types
    When I request the events list "type=milestone" as actor "claude@laptop"
    Then the response items are the events "7"
    When I request the events list "type=task_done&type=milestone" as actor "claude@laptop"
    Then the response items are the events "6,7,8"
    And the response items have the types "task_done,milestone,task_done"
    When I request the events list "type=no_such_type" as actor "claude@laptop"
    Then the response status is 200
    And the response path "items" has 0 items

  Scenario: an empty type is 400
    When I request the events list "type=" as actor "claude@laptop"
    Then the response status is 400
    And the problem has a next field containing "type"

  Scenario: the actor filter keeps the events of one actor handle
    When I request the events list "actor=codex@laptop" as actor "claude@laptop"
    Then the response items are the events "4,5,6"
    And the response items have the actors "codex@laptop,codex@laptop,codex@laptop"
    When I request the events list "actor=nobody@laptop" as actor "claude@laptop"
    Then the response path "items" has 0 items

  Scenario: the filters combine
    When I request the events list "initiative=aa&type=task_created&actor=codex@laptop" as actor "claude@laptop"
    Then the response items are the events "4"
    When I request the events list "task=aa-1&type=task_done&since={ref:3}" as actor "claude@laptop"
    Then the response items are the events "6"
    When I request the events list "type=task_done&type=task_created&actor=claude@laptop&limit=2" as actor "claude@laptop"
    Then the response items are the events "3,8"

  Scenario: since an event ULID keeps the events after it
    When I request the events list "since={id:5}" as actor "claude@laptop"
    Then the response items are the events "6,7,8"
    When I request the events list "since={id:8}" as actor "claude@laptop"
    Then the response path "items" has 0 items

  Scenario: since an E<n> ref keeps the events after it, in either letter case
    When I request the events list "since=E5" as actor "claude@laptop"
    Then the response items are the events "6,7,8"
    When I request the events list "since=e5" as actor "claude@laptop"
    Then the response items are the events "6,7,8"
    When I request the events list "since={ref:1}" as actor "claude@laptop"
    Then the response items are the events "2,3,4,5,6,7,8"

  Scenario: since an RFC 3339 time keeps the events from that time on
    Given the first 4 events are dated "2020-01-01T00:00:00Z"
    When I request the events list "since=2021-06-01T00:00:00Z" as actor "claude@laptop"
    Then the response items are the events "5,6,7,8"
    When I request the events list "since=2021-06-01T02:00:00%2B02:00" as actor "claude@laptop"
    Then the response items are the events "5,6,7,8"
    When I request the events list "since=2019-12-31T23:59:59Z" as actor "claude@laptop"
    Then the response items are the events "1,2,3,4,5,6,7,8"
    When I request the events list "since=2020-01-01T00:00:00Z" as actor "claude@laptop"
    Then the response items are the events "1,2,3,4,5,6,7,8"
    When I request the events list "since=2099-01-01T00:00:00Z" as actor "claude@laptop"
    Then the response path "items" has 0 items

  Scenario Outline: a since that is none of the three forms is 400
    When I request the events list "since=<value>" as actor "claude@laptop"
    Then the response status is 400
    And the problem has a next field containing "RFC 3339"

    Examples:
      | value            |
      | yesterday        |
      | 2021-06-01       |
      | E                |
      | E0x5             |

  Scenario: limit pages the list and next_after names the cursor
    When I request the events list "limit=3" as actor "claude@laptop"
    Then the response items are the events "1,2,3"
    And the response next_after is the id of the event 3
    When I request the next events page with "limit=3" as actor "claude@laptop"
    Then the response items are the events "4,5,6"
    And the response next_after is the id of the event 6
    When I request the next events page with "limit=3" as actor "claude@laptop"
    Then the response items are the events "7,8"
    And the response has no next_after

  Scenario: a page that ends exactly at the last event has no next_after
    When I request the events list "limit=8" as actor "claude@laptop"
    Then the response items are the events "1,2,3,4,5,6,7,8"
    And the response has no next_after

  Scenario: after takes an event ULID or an E<n> ref
    When I request the events list "after={id:2}&limit=2" as actor "claude@laptop"
    Then the response items are the events "3,4"
    When I request the events list "after={ref:6}" as actor "claude@laptop"
    Then the response items are the events "7,8"

  Scenario: paging composes with the filters
    When I request the events list "type=task_created&limit=2" as actor "claude@laptop"
    Then the response items are the events "3,4"
    And the response next_after is the id of the event 4
    When I request the next events page with "type=task_created&limit=2" as actor "claude@laptop"
    Then the response items are the events "5"
    And the response has no next_after
    When I request the events list "since={ref:3}&limit=2" as actor "claude@laptop"
    Then the response items are the events "4,5"

  Scenario Outline: a limit or an after that cannot be used is 400
    When I request the events list "<query>" as actor "claude@laptop"
    Then the response status is 400

    Examples:
      | query                     |
      | limit=0                   |
      | limit=1001                |
      | limit=-1                  |
      | limit=abc                 |
      | after=nonsense            |
      | after=2021-06-01T00:00:00Z |

  Scenario: the largest limit is accepted
    When I request the events list "limit=1000" as actor "claude@laptop"
    Then the response status is 200
    And the response items are the events "1,2,3,4,5,6,7,8"
