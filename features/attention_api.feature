Feature: Attention over HTTP
  GET /api/v1/attention answers what needs the operator in five sections: open asks, done tasks not yet closed,
  expired leases nobody acknowledged, overdue watches and live conflicts. Each section is cleared by the
  action that deals with it, and every item carries the actors involved with their way back. counts holds the
  size of each section and their sum; tag and initiative narrow both; fields=counts answers only counts.

  Background:
    Given a new database for the board commands
    When I run "vh admin migrate"
    And I run "vh admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port
    And I create the initiative "aa" titled "First" with tags "web,team" as actor "claude@laptop"
    And I create the initiative "bb" titled "Second" with tags "web" as actor "claude@laptop"
    And I create the initiative "cc" titled "Third" with no tags as actor "claude@laptop"
    And I create a task in "aa" titled "one" as actor "claude@laptop"
    And I create a task in "aa" titled "two" as actor "claude@laptop"
    And I create a task in "bb" titled "other" as actor "claude@laptop"
    And I create a watch task in "aa" titled "poll the build" with a cadence of 3600 seconds as actor "claude@laptop"

  Scenario: an empty board counts 0 in every section
    When I request the attention with "" as actor "claude@laptop"
    Then the response status is 200
    And the attention counts are "0,0,0,0,0,0"
    And the response path "open_asks" has 0 items
    And the response path "done_not_closed" has 0 items
    And the response path "expired_leases" has 0 items
    And the response path "overdue_watches" has 0 items
    And the response path "conflicts" has 0 items

  Scenario: an open ask is listed with its asker and way back until it is answered
    Given the next API request carries the header "Board-Actor-Way-Back" with value "tmux attach -t ws1"
    And I raise an ask with body "{'task':'aa-1','question':'Which database?','mode':'poll','blocking':true}" as actor "claude@laptop"
    When I request the attention with "" as actor "operator@laptop"
    Then the response status is 200
    And the attention counts are "1,0,0,0,0,1"
    And the response path "open_asks.0.ref" is "A1"
    And the response path "open_asks.0.question" is "Which database?"
    And the response path "open_asks.0.task" is "aa-1"
    And the response path "open_asks.0.asker.handle" is "claude@laptop"
    And the response path "open_asks.0.asker.way_back" is "tmux attach -t ws1"
    And the response path "open_asks.0.mode" is "poll"
    And the response path "open_asks.0.asker_waiting" is "false"
    And the response path "open_asks.0.blocking" is "true"
    And the response path "open_asks.0.created_at" is present
    And the response path "open_asks.0.age_seconds" is present
    When I answer the ask "A1" with body "{'answer':'Postgres'}" as actor "operator@laptop"
    And I request the attention with "" as actor "operator@laptop"
    Then the attention counts are "0,0,0,0,0,0"
    And the response path "open_asks" has 0 items

  Scenario: a poll ask whose asker is polling says so, and a board-wide ask has no task
    Given I raise an ask with body "{'question':'Anything else?','mode':'poll'}" as actor "claude@laptop"
    And the poll of the ask "A1" was last seen 10 seconds ago
    When I request the attention with "" as actor "operator@laptop"
    Then the attention counts are "1,0,0,0,0,1"
    And the response path "open_asks.0.asker_waiting" is "true"
    And the response path "open_asks.0.task" is absent
    And the response path "open_asks.0.asker.way_back" is absent

  Scenario: a withdrawn ask leaves the open asks
    Given I raise an ask with body "{'task':'aa-2','question':'Which one?'}" as actor "claude@laptop"
    And I request the attention with "" as actor "operator@laptop"
    And the attention counts are "1,0,0,0,0,1"
    When I withdraw the ask "A1" with body "{'reason':'found it'}" as actor "claude@laptop"
    And I request the attention with "" as actor "operator@laptop"
    Then the attention counts are "0,0,0,0,0,0"

  Scenario: open asks come newest first
    Given I raise an ask with body "{'task':'aa-1','question':'first'}" as actor "claude@laptop"
    And I raise an ask with body "{'task':'aa-2','question':'second'}" as actor "codex@laptop"
    When I request the attention with "" as actor "operator@laptop"
    Then the attention counts are "2,0,0,0,0,2"
    And the response path "open_asks.0.question" is "second"
    And the response path "open_asks.1.question" is "first"

  Scenario: a done task is listed with the outcome of its last lease until it is closed
    Given I start a lease with body "{'task':'aa-1','intent':'build it'}" as actor "claude@laptop"
    And I end the lease "L1" with body "{'outcome':'shipped'}" as actor "claude@laptop"
    And I mark the task "aa-1" done as actor "codex@laptop"
    When I request the attention with "" as actor "operator@laptop"
    Then the response status is 200
    And the attention counts are "0,1,0,0,0,1"
    And the response path "done_not_closed.0.task" is "aa-1"
    And the response path "done_not_closed.0.title" is "one"
    And the response path "done_not_closed.0.done_by.handle" is "codex@laptop"
    And the response path "done_not_closed.0.done_at" is present
    And the response path "done_not_closed.0.last_lease.lease" is "L1"
    And the response path "done_not_closed.0.last_lease.holder.handle" is "claude@laptop"
    And the response path "done_not_closed.0.last_lease.outcome" is "shipped"
    When I close the task "aa-1" with body "{'resolution':'completed'}" as actor "operator@laptop"
    And I request the attention with "" as actor "operator@laptop"
    Then the attention counts are "0,0,0,0,0,0"
    And the response path "done_not_closed" has 0 items

  Scenario: a done task that no lease worked on has no last lease
    Given I mark the task "aa-2" done as actor "codex@laptop"
    When I request the attention with "" as actor "operator@laptop"
    Then the attention counts are "0,1,0,0,0,1"
    And the response path "done_not_closed.0.task" is "aa-2"
    And the response path "done_not_closed.0.last_lease" is absent

  Scenario: an expired lease is listed until the operator acknowledges it
    Given the next API request carries the header "Board-Actor-Way-Back" with value "ssh build1"
    And I start a lease with body "{'task':'aa-1','intent':'migrate'}" as actor "claude@laptop"
    When I request the attention with "" as actor "operator@laptop"
    Then the attention counts are "0,0,0,0,0,0"
    When the lease "L1" ran out 30 seconds ago
    And I request the attention with "" as actor "operator@laptop"
    Then the response status is 200
    And the attention counts are "0,0,1,0,0,1"
    And the response path "expired_leases.0.lease" is "L1"
    And the response path "expired_leases.0.holder.handle" is "claude@laptop"
    And the response path "expired_leases.0.holder.way_back" is "ssh build1"
    And the response path "expired_leases.0.task" is "aa-1"
    And the response path "expired_leases.0.title" is "one"
    And the response path "expired_leases.0.intent" is "migrate"
    And the response path "expired_leases.0.renewed_at" is present
    And the response path "expired_leases.0.expired_at" is present
    When I acknowledge the lease "L1" as actor "operator@laptop"
    And I request the attention with "" as actor "operator@laptop"
    Then the attention counts are "0,0,0,0,0,0"

  Scenario: a later lease on the task clears an expired lease
    Given I start a lease with body "{'task':'aa-1','intent':'migrate'}" as actor "claude@laptop"
    And the lease "L1" ran out 30 seconds ago
    And I request the attention with "" as actor "operator@laptop"
    And the attention counts are "0,0,1,0,0,1"
    When I start a lease with body "{'task':'aa-1','intent':'finish the migration'}" as actor "codex@laptop"
    And I request the attention with "" as actor "operator@laptop"
    Then the attention counts are "0,0,0,0,0,0"

  Scenario: an overdue watch is listed with its last check until a check is recorded
    Given I record a check on the task "aa-3" with body "{'summary':'build is red','next_check_at':'2020-01-01T00:00:00Z'}" as actor "codex@laptop"
    When I request the attention with "" as actor "operator@laptop"
    Then the response status is 200
    And the attention counts are "0,0,0,1,0,1"
    And the response path "overdue_watches.0.task" is "aa-3"
    And the response path "overdue_watches.0.title" is "poll the build"
    And the response path "overdue_watches.0.owner.handle" is "claude@laptop"
    And the response path "overdue_watches.0.next_check_at" is "2020-01-01T00:00:00Z"
    And the response path "overdue_watches.0.last_check.summary" is "build is red"
    And the response path "overdue_watches.0.last_check.by.handle" is "codex@laptop"
    And the response path "overdue_watches.0.last_check.at" is present
    When I record a check on the task "aa-3" with body "{'summary':'build is green'}" as actor "codex@laptop"
    And I request the attention with "" as actor "operator@laptop"
    Then the attention counts are "0,0,0,0,0,0"

  Scenario: two live leases on one resource are a conflict until one of them ends
    Given I start a lease with body "{'task':'aa-1','intent':'migrate','resources':['db:x']}" as actor "claude@laptop"
    And I start a lease with body "{'task':'aa-2','intent':'reindex','resources':['db:x','db:y']}" as actor "codex@laptop"
    When I request the attention with "" as actor "operator@laptop"
    Then the response status is 200
    And the attention counts are "0,0,0,0,1,1"
    And the response path "conflicts.0.resource" is "db:x"
    And the response path "conflicts.0.holders" has 2 items
    And the response path "conflicts.0.holders.0.lease" is "L1"
    And the response path "conflicts.0.holders.0.actor.handle" is "claude@laptop"
    And the response path "conflicts.0.holders.0.task" is "aa-1"
    And the response path "conflicts.0.holders.0.intent" is "migrate"
    And the response path "conflicts.0.holders.0.expires_at" is present
    And the response path "conflicts.0.holders.1.lease" is "L2"
    And the response path "conflicts.0.holders.1.actor.handle" is "codex@laptop"
    And the response path "conflicts.0.tasks" is "aa-1,aa-2"
    When I end the lease "L2" without a body as actor "codex@laptop"
    And I request the attention with "" as actor "operator@laptop"
    Then the attention counts are "0,0,0,0,0,0"
    And the response path "conflicts" has 0 items

  Scenario: the total is the sum of the five sections
    Given I raise an ask with body "{'task':'aa-1','question':'Which?'}" as actor "claude@laptop"
    And I record a check on the task "aa-3" with body "{'summary':'late','next_check_at':'2020-01-01T00:00:00Z'}" as actor "codex@laptop"
    And I start a lease with body "{'task':'aa-1','intent':'one','resources':['db:x']}" as actor "claude@laptop"
    And I start a lease with body "{'task':'bb-1','intent':'two','resources':['db:x']}" as actor "codex@laptop"
    And I start a lease with body "{'task':'aa-2','intent':'old'}" as actor "gemini@laptop"
    And the lease "L3" ran out 30 seconds ago
    And I mark the task "aa-2" done as actor "codex@laptop"
    When I request the attention with "" as actor "operator@laptop"
    Then the attention counts are "1,1,1,1,1,5"

  Scenario: the initiative and tag filters narrow every section
    Given I raise an ask with body "{'task':'aa-1','question':'in aa'}" as actor "claude@laptop"
    And I raise an ask with body "{'task':'bb-1','question':'in bb'}" as actor "claude@laptop"
    And I raise an ask with body "{'question':'on no initiative'}" as actor "claude@laptop"
    When I request the attention with "initiative=bb" as actor "operator@laptop"
    Then the attention counts are "1,0,0,0,0,1"
    And the response path "open_asks.0.question" is "in bb"
    When I request the attention with "initiative=aa&initiative=bb" as actor "operator@laptop"
    Then the attention counts are "2,0,0,0,0,2"
    When I request the attention with "tag=team" as actor "operator@laptop"
    Then the attention counts are "1,0,0,0,0,1"
    And the response path "open_asks.0.question" is "in aa"
    When I request the attention with "tag=web" as actor "operator@laptop"
    Then the attention counts are "2,0,0,0,0,2"
    When I request the attention with "tag=web&tag=team" as actor "operator@laptop"
    Then the attention counts are "1,0,0,0,0,1"
    When I request the attention with "tag=web&initiative=bb" as actor "operator@laptop"
    Then the attention counts are "1,0,0,0,0,1"
    When I request the attention with "initiative=cc" as actor "operator@laptop"
    Then the attention counts are "0,0,0,0,0,0"
    When I request the attention with "tag=other" as actor "operator@laptop"
    Then the attention counts are "0,0,0,0,0,0"
    When I request the attention with "" as actor "operator@laptop"
    Then the attention counts are "3,0,0,0,0,3"

  Scenario: the filters apply to done tasks, expired leases, overdue watches and conflicts
    Given I record a check on the task "aa-3" with body "{'summary':'late','next_check_at':'2020-01-01T00:00:00Z'}" as actor "codex@laptop"
    And I start a lease with body "{'task':'aa-1','intent':'one','resources':['db:x']}" as actor "claude@laptop"
    And I start a lease with body "{'task':'aa-2','intent':'two','resources':['db:x']}" as actor "codex@laptop"
    And I start a lease with body "{'task':'bb-1','intent':'old'}" as actor "gemini@laptop"
    And the lease "L3" ran out 30 seconds ago
    And I mark the task "bb-1" done as actor "codex@laptop"
    When I request the attention with "" as actor "operator@laptop"
    Then the attention counts are "0,1,1,1,1,4"
    When I request the attention with "initiative=aa" as actor "operator@laptop"
    Then the attention counts are "0,0,0,1,1,2"
    When I request the attention with "initiative=bb" as actor "operator@laptop"
    Then the attention counts are "0,1,1,0,0,2"
    When I request the attention with "tag=team" as actor "operator@laptop"
    Then the attention counts are "0,0,0,1,1,2"

  Scenario: fields=counts answers only the counts
    Given I raise an ask with body "{'task':'aa-1','question':'in aa'}" as actor "claude@laptop"
    And I mark the task "aa-2" done as actor "codex@laptop"
    When I request the attention with "fields=counts" as actor "operator@laptop"
    Then the response status is 200
    And the response members are exactly "counts"
    And the attention counts are "1,1,0,0,0,2"
    When I request the attention with "fields=counts&initiative=bb" as actor "operator@laptop"
    Then the response members are exactly "counts"
    And the attention counts are "0,0,0,0,0,0"
    When I request the attention with "fields=full" as actor "operator@laptop"
    Then the response members are exactly "counts,open_asks,done_not_closed,expired_leases,overdue_watches,conflicts"

  Scenario: fields=counts counts the same as the full answer for expired leases and conflicts
    Given I start a lease with body "{'task':'aa-1','intent':'one','resources':['db:x']}" as actor "claude@laptop"
    And I start a lease with body "{'task':'aa-2','intent':'two','resources':['db:x']}" as actor "codex@laptop"
    And the lease "L2" ran out 30 seconds ago
    When I request the attention with "fields=counts" as actor "operator@laptop"
    Then the attention counts are "0,0,1,0,0,1"

  Scenario Outline: a bad parameter is refused with a next line
    When I request the attention with "<query>" as actor "operator@laptop"
    Then the response status is <status>
    And the problem detail contains "<saying>"
    And the response path "next" is present

    Examples:
      | query            | status | saying                     |
      | fields=nothing   | 400    | is not full or counts      |
      | tag=bad%20tag    | 400    | must match                 |
      | initiative=zzz   | 404    | there is no initiative zzz |

  Scenario: every attention item that is a task carries its own, initiative and actor-kind tags with their sources
    Given I add the tag "mine" to the task "aa-1" as actor "claude@laptop"
    And I start a lease with body "{'task':'aa-1','intent':'build it'}" as actor "ralph:r1"
    And I end the lease "L1" with body "{'outcome':'shipped'}" as actor "ralph:r1"
    And I mark the task "aa-1" done as actor "codex@laptop"
    And I start a lease with body "{'task':'bb-1','intent':'migrate'}" as actor "pi:p1"
    And the lease "L2" ran out 30 seconds ago
    And I add the tag "nightly" to the task "aa-3" as actor "claude@laptop"
    And I record a check on the task "aa-3" with body "{'summary':'build is red','next_check_at':'2020-01-01T00:00:00Z'}" as actor "codex@laptop"
    When I request the attention with "" as actor "operator@laptop"
    Then the response status is 200
    And the attention counts are "0,1,1,1,0,3"
    And the response path "done_not_closed.0.tags" is "mine"
    And the response path "done_not_closed.0.tag_marks" has 4 items
    And the response path "done_not_closed.0.tag_marks.0.tag" is "mine"
    And the response path "done_not_closed.0.tag_marks.0.source" is "task"
    And the response path "done_not_closed.0.tag_marks.1.tag" is "team"
    And the response path "done_not_closed.0.tag_marks.1.source" is "initiative"
    And the response path "done_not_closed.0.tag_marks.2.tag" is "web"
    And the response path "done_not_closed.0.tag_marks.3.tag" is "ralph"
    And the response path "done_not_closed.0.tag_marks.3.source" is "actor"
    And the response path "expired_leases.0.tags" has 0 items
    And the response path "expired_leases.0.tag_marks" has 2 items
    And the response path "expired_leases.0.tag_marks.0.tag" is "web"
    And the response path "expired_leases.0.tag_marks.0.source" is "initiative"
    And the response path "expired_leases.0.tag_marks.1.tag" is "pi"
    And the response path "expired_leases.0.tag_marks.1.source" is "actor"
    And the response path "overdue_watches.0.tags" is "nightly"
    And the response path "overdue_watches.0.tag_marks" has 3 items
    And the response path "overdue_watches.0.tag_marks.0.source" is "task"
    And the response path "overdue_watches.0.tag_marks.1.tag" is "team"
