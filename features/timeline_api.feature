Feature: Timeline over HTTP
  GET /api/v1/timeline?from=&to= answers the initiatives, tasks, leases and markers that fall in a window of
  time. A task is in the window when its actual span (created to closed, open while it is not closed) or its
  planned span overlaps it, and so is a lease whose span overlaps it, so a window that cuts a lease keeps it.
  Closed tasks are left out unless closed_within brings back those closed that recently. The filters initiative,
  tag (repeatable), actor and closed_within narrow it, each task carries its attention reasons, and a window
  that is empty, over 400 days or unreadable is 400.

  Background:
    Given a new database for the board commands
    When I run "board admin migrate"
    And I run "board admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port
    And I create the initiative "aa" titled "First" with tags "web,team" as actor "claude@laptop"
    And I create the initiative "bb" titled "Second" with tags "web" as actor "claude@laptop"
    And I create the initiative "cc" titled "Third" with no tags as actor "claude@laptop"
    And I create a task in "aa" titled "one" as actor "claude@laptop"
    And I create a task in "aa" titled "two" as actor "claude@laptop"
    And I create a task in "aa" titled "three" as actor "claude@laptop"
    And I create a task in "aa" titled "four" as actor "claude@laptop"
    And I create a watch task in "bb" titled "watch me" with a cadence of 3600 seconds as actor "claude@laptop"
    And I create a task in "bb" titled "old" as actor "claude@laptop"
    And I create a task in "cc" titled "ancient" as actor "claude@laptop"
    And I make the task "aa-2" blocked by "aa-1" as actor "claude@laptop"
    And I start a lease with body "{'task':'aa-1','intent':'spike'}" as actor "claude@laptop"
    And I end the lease "L1" without a body as actor "claude@laptop"
    And I start a lease with body "{'task':'aa-1','intent':'parser'}" as actor "claude@laptop"
    And I end the lease "L2" without a body as actor "claude@laptop"
    And I start a lease with body "{'task':'bb-1','intent':'watching'}" as actor "codex@laptop"
    And I start a lease with body "{'task':'aa-2','intent':'schema'}" as actor "codex@laptop"
    And I record a milestone on the task "aa-1" with body "{'summary':'early spike'}" as actor "claude@laptop"
    And I record a milestone on the task "aa-1" with body "{'summary':'started parser'}" as actor "claude@laptop"
    And I raise an ask with body "{'task':'aa-2','question':'Which database?'}" as actor "codex@laptop"
    And I record a check on the task "bb-1" with body "{'summary':'build green'}" as actor "codex@laptop"
    And I mark the task "aa-4" done as actor "codex@laptop"
    And I close the task "bb-2" with body "{'resolution':'completed'}" as actor "claude@laptop"
    And I close the task "cc-1" with body "{'resolution':'dropped'}" as actor "claude@laptop"
    And the task "aa-1" was created 20 days ago
    And the task "aa-2" was created 3 days ago
    And the task "aa-3" was created -10 days ago
    And the task "aa-3" is planned from 2 to 3 days from now
    And the task "bb-1" was created 2 days ago
    And the task "bb-2" was created 15 days ago
    And the task "bb-2" was closed 5 days ago
    And the task_closed events of the task "bb-2" containing "closed" happened 5 days ago
    And the task "cc-1" was created 40 days ago
    And the task "cc-1" was closed 30 days ago
    And the task_closed events of the task "cc-1" containing "closed" happened 30 days ago
    And the lease "L1" ran from 20 days ago to 15 days ago
    And the lease "L2" ran from 12 days ago to 9 days ago
    And the lease "L4" ran from 1 days ago to 0 days ago
    And the milestone events of the task "aa-1" containing "early spike" happened 12 days ago
    And the milestone events of the task "aa-1" containing "started parser" happened 4 days ago

  Scenario: a seeded window answers its tasks, leases and markers in stable order
    When I request the timeline for the window from -10 to 5 days with "" as actor "operator@laptop"
    Then the response status is 200
    And the timeline initiatives have slug "aa,bb"
    And the timeline tasks have ref "aa-1,aa-2,aa-3,aa-4,bb-1"
    And the timeline leases have ref "L2,L3,L4"
    And the timeline markers are "aa-1:milestone,aa-2:ask,bb-1:check,aa-4:done"
    And the response path "initiatives.0.title" is "First"
    And the response path "initiatives.0.tags" is "team,web"
    And the response path "initiatives.0.closed" is "false"
    And the response path "initiatives.1.tags" is "web"
    And the response path "tasks.0.initiative" is "aa"
    And the response path "tasks.0.title" is "one"
    And the response path "tasks.0.status" is "open"
    And the response path "tasks.0.kind" is "work"
    And the response path "tasks.0.created_at" is present
    And the response path "tasks.0.closed_at" is absent
    And the response path "tasks.0.planned_start" is absent
    And the response path "tasks.0.deps" has 0 items
    And the response path "tasks.1.deps" is "aa-1"
    And the response path "tasks.2.planned_start" is present
    And the response path "tasks.2.planned_end" is present
    And the response path "tasks.3.status" is "done"
    And the response path "tasks.4.kind" is "watch"
    And the response path "leases.0.task" is "aa-1"
    And the response path "leases.0.holder" is "claude@laptop"
    And the response path "leases.0.state" is "ended"
    And the response path "leases.0.intent" is "parser"
    And the response path "leases.0.started_at" is present
    And the response path "leases.0.ended_at" is present
    And the response path "leases.1.state" is "live"
    And the response path "leases.1.ended_at" is absent
    And the response path "leases.2.state" is "expired"
    And the response path "leases.2.holder" is "codex@laptop"
    And the response path "leases.2.ended_at" is present
    And the response path "markers.0.label" is "started parser"
    And the response path "markers.1.label" is "Which database?"
    And the response path "markers.2.label" is "build green"
    And the response path "markers.3.type" is "done"
    And the response path "markers.3.at" is present
    And the response path "from" is present
    And the response path "to" is present

  Scenario: a window that leaves out the default 14 days back and 7 days ahead is the default
    When I request the timeline with "" as actor "operator@laptop"
    Then the response status is 200
    And the timeline window is 21 days long
    And the timeline tasks have ref "aa-1,aa-2,aa-3,aa-4,bb-1"
    And the timeline leases have ref "L2,L3,L4"

  Scenario: a window that cuts a lease keeps it and a window that misses it leaves it out
    When I request the timeline for the window from -14 to -10 days with "" as actor "operator@laptop"
    Then the timeline leases have ref "L2"
    And the response path "leases.0.state" is "ended"
    When I request the timeline for the window from -10 to -8 days with "" as actor "operator@laptop"
    Then the timeline leases have ref "L2"
    When I request the timeline for the window from -8 to 5 days with "" as actor "operator@laptop"
    Then the timeline leases have ref "L3,L4"
    When I request the timeline for the window from -30 to -11 days with "" as actor "operator@laptop"
    Then the timeline leases have ref "L1,L2"

  Scenario: a task whose plan overlaps the window is in it though it is created after it
    When I request the timeline for the window from 1 to 4 days with "" as actor "operator@laptop"
    Then the timeline tasks have ref "aa-1,aa-2,aa-3,aa-4,bb-1"
    When I request the timeline for the window from -2 to 1 days with "" as actor "operator@laptop"
    Then the timeline tasks have ref "aa-1,aa-2,aa-4,bb-1"

  Scenario: closed tasks are absent without closed_within and present with it
    When I request the timeline for the window from -10 to 5 days with "" as actor "operator@laptop"
    Then the timeline tasks have ref "aa-1,aa-2,aa-3,aa-4,bb-1"
    When I request the timeline for the window from -10 to 5 days with "closed_within=7d" as actor "operator@laptop"
    Then the timeline tasks have ref "aa-1,aa-2,aa-3,aa-4,bb-1,bb-2"
    And the timeline markers are "bb-2:closed,aa-1:milestone,aa-2:ask,bb-1:check,aa-4:done"
    And the response path "tasks.5.status" is "closed"
    And the response path "tasks.5.closed_at" is present
    When I request the timeline for the window from -10 to 5 days with "closed_within=3" as actor "operator@laptop"
    Then the timeline tasks have ref "aa-1,aa-2,aa-3,aa-4,bb-1"

  Scenario: closed_within does not bring back a task that is closed outside the window
    When I request the timeline for the window from -10 to 5 days with "closed_within=90d" as actor "operator@laptop"
    Then the timeline tasks have ref "aa-1,aa-2,aa-3,aa-4,bb-1,bb-2"
    When I request the timeline for the window from -45 to -25 days with "closed_within=90d" as actor "operator@laptop"
    Then the timeline tasks have ref "cc-1"
    And the timeline initiatives have slug "cc"
    And the timeline markers are "cc-1:closed"

  Scenario Outline: the initiative filter keeps the tasks of the initiatives named
    When I request the timeline for the window from -10 to 5 days with "<query>" as actor "operator@laptop"
    Then the response status is 200
    And the timeline tasks have ref "<tasks>"
    And the timeline initiatives have slug "<initiatives>"
    And the timeline leases have ref "<leases>"

    Examples:
      | query                        | tasks                | initiatives | leases   |
      | initiative=aa                | aa-1,aa-2,aa-3,aa-4  | aa          | L2,L4    |
      | initiative=bb                | bb-1                 | bb          | L3       |
      | initiative=bb&initiative=aa  | aa-1,aa-2,aa-3,aa-4,bb-1 | aa,bb   | L2,L3,L4 |

  Scenario: an initiative that does not exist is 404
    When I request the timeline for the window from -10 to 5 days with "initiative=nope" as actor "operator@laptop"
    Then the response status is 404

  Scenario Outline: the tag filter keeps the tasks of initiatives that carry every tag
    When I request the timeline for the window from -10 to 5 days with "<query>" as actor "operator@laptop"
    Then the response status is 200
    And the timeline tasks have ref "<tasks>"

    Examples:
      | query               | tasks                    |
      | tag=web             | aa-1,aa-2,aa-3,aa-4,bb-1 |
      | tag=team            | aa-1,aa-2,aa-3,aa-4      |
      | tag=web&tag=team    | aa-1,aa-2,aa-3,aa-4      |
      | tag=nothing         |                          |
      | tag=team&tag=nothing |                         |

  Scenario: a tag that is not a tag is 400
    When I request the timeline for the window from -10 to 5 days with "tag=Not%20A%20Tag" as actor "operator@laptop"
    Then the response status is 400

  Scenario Outline: the actor filter keeps the tasks on which the actor held a lease in the window
    When I request the timeline for the window from -10 to 5 days with "actor=<actor>" as actor "operator@laptop"
    Then the response status is 200
    And the timeline tasks have ref "<tasks>"
    And the timeline leases have ref "<leases>"

    Examples:
      | actor           | tasks     | leases |
      | codex@laptop    | aa-2,bb-1 | L3,L4  |
      | claude@laptop   | aa-1      | L2     |
      | nobody@laptop   |           |        |

  Scenario: the actor filter judges the leases by the window
    When I request the timeline for the window from -30 to -11 days with "actor=claude@laptop" as actor "operator@laptop"
    Then the timeline tasks have ref "aa-1"
    And the timeline leases have ref "L1,L2"
    When I request the timeline for the window from -8 to 5 days with "actor=claude@laptop" as actor "operator@laptop"
    Then the timeline tasks have ref ""

  Scenario: the attention list of each task comes from the attention derivation
    When I request the timeline for the window from -10 to 5 days with "" as actor "operator@laptop"
    Then the timeline tasks have ref "aa-1,aa-2,aa-3,aa-4,bb-1"
    And the response path "tasks.0.attention" has 0 items
    And the response path "tasks.1.attention" has 2 items
    And the response path "tasks.1.attention.0.reason" is "stranded"
    And the response path "tasks.1.attention.1.reason" is "asking"
    And the response path "tasks.2.attention" has 0 items
    And the response path "tasks.3.attention" has 1 items
    And the response path "tasks.3.attention.0.reason" is "done"
    And the response path "tasks.4.attention" has 0 items
    When I acknowledge the lease "L4" as actor "operator@laptop"
    And I request the timeline for the window from -10 to 5 days with "" as actor "operator@laptop"
    Then the response path "tasks.1.attention" has 1 items
    And the response path "tasks.1.attention.0.reason" is "asking"

  Scenario: a window ending before it starts is 400
    When I request the timeline with "from=2030-01-02T00:00:00Z&to=2030-01-01T00:00:00Z" as actor "operator@laptop"
    Then the response status is 400
    And the problem detail contains "after from"
    And the problem has a next field containing "RFC 3339"

  Scenario: an empty window is 400
    When I request the timeline with "from=2030-01-01T00:00:00Z&to=2030-01-01T00:00:00Z" as actor "operator@laptop"
    Then the response status is 400

  Scenario Outline: a window over 400 days is 400 and one of 400 days is answered
    When I request the timeline with "from=2030-01-01T00:00:00Z&to=<to>" as actor "operator@laptop"
    Then the response status is <status>

    Examples:
      | to                   | status |
      | 2031-02-05T00:00:00Z | 200    |
      | 2031-02-05T00:00:01Z | 400    |
      | 2035-01-01T00:00:00Z | 400    |

  Scenario Outline: a time that does not read is 400
    When I request the timeline with "<query>" as actor "operator@laptop"
    Then the response status is 400
    And the problem detail contains "RFC 3339"

    Examples:
      | query                                          |
      | from=yesterday                                 |
      | to=tomorrow                                    |
      | from=2030-01-01&to=2030-02-01T00:00:00Z        |
      | from=2030-01-01T00:00:00Z&to=2030-01-32T00:00:00Z |

  Scenario: a closed_within that is none of the values is 400
    When I request the timeline with "closed_within=soon" as actor "operator@laptop"
    Then the response status is 400
    And the problem has a next field containing "7d"

  Scenario: rows=window keeps only the tasks in the window and is the default
    When I request the timeline for the window from -45 to -25 days with "" as actor "operator@laptop"
    Then the timeline tasks have ref ""
    When I request the timeline for the window from -45 to -25 days with "rows=window" as actor "operator@laptop"
    Then the response status is 200
    And the timeline tasks have ref ""

  Scenario: rows=all keeps the tasks wherever their spans fall and the window picks the leases and markers
    When I request the timeline for the window from -45 to -25 days with "rows=all" as actor "operator@laptop"
    Then the response status is 200
    And the timeline tasks have ref "aa-1,aa-2,aa-3,aa-4,bb-1"
    And the timeline initiatives have slug "aa,bb"
    And the timeline leases have ref ""
    And the timeline markers are ""
    When I request the timeline for the window from -22 to -14 days with "rows=all" as actor "operator@laptop"
    Then the timeline tasks have ref "aa-1,aa-2,aa-3,aa-4,bb-1"
    And the timeline leases have ref "L1"
    When I request the timeline for the window from -10 to 5 days with "rows=all" as actor "operator@laptop"
    Then the timeline tasks have ref "aa-1,aa-2,aa-3,aa-4,bb-1"
    And the timeline leases have ref "L2,L3,L4"
    When I request the timeline for the window from -45 to -25 days with "rows=all&closed_within=90d" as actor "operator@laptop"
    Then the timeline tasks have ref "aa-1,aa-2,aa-3,aa-4,bb-1,bb-2,cc-1"

  Scenario: rows=all with an actor keeps the tasks the actor ever leased
    When I request the timeline for the window from -8 to 5 days with "actor=claude@laptop" as actor "operator@laptop"
    Then the timeline tasks have ref ""
    When I request the timeline for the window from -8 to 5 days with "rows=all&actor=claude@laptop" as actor "operator@laptop"
    Then the response status is 200
    And the timeline tasks have ref "aa-1"
    And the timeline leases have ref ""
    When I request the timeline for the window from -8 to 5 days with "rows=all&actor=codex@laptop" as actor "operator@laptop"
    Then the timeline tasks have ref "aa-2,bb-1"
    When I request the timeline for the window from -8 to 5 days with "rows=all&actor=nobody@laptop" as actor "operator@laptop"
    Then the timeline tasks have ref ""

  Scenario: a refused rows leaves a lease that is due live, and a supplied empty rows is refused too
    When I note the lease rows
    And I request the timeline for the window from -10 to 5 days with "rows=some" as actor "operator@laptop"
    Then the response status is 400
    And the problem detail contains "rows"
    And the lease rows are unchanged
    When I request the timeline for the window from -10 to 5 days with "rows=" as actor "operator@laptop"
    Then the response status is 400
    And the problem has a next field containing "window"
    And the lease rows are unchanged
    When I request the timeline for the window from -10 to 5 days with "rows=ALL" as actor "operator@laptop"
    Then the response status is 400
    And the lease rows are unchanged
    When I request the timeline for the window from -10 to 5 days with "rows=all" as actor "operator@laptop"
    Then the response status is 200
    And the timeline leases have ref "L2,L3,L4"

  Scenario: a rows that is neither window nor all is 400 and changes nothing
    When I request the timeline for the window from -10 to 5 days with "" as actor "operator@laptop"
    Then the timeline tasks have ref "aa-1,aa-2,aa-3,aa-4,bb-1"
    When I request the timeline for the window from -10 to 5 days with "rows=some" as actor "operator@laptop"
    Then the response status is 400
    And the problem detail contains "rows"
    And the problem has a next field containing "window"
    When I request the timeline for the window from -10 to 5 days with "" as actor "operator@laptop"
    Then the timeline tasks have ref "aa-1,aa-2,aa-3,aa-4,bb-1"
    And the timeline leases have ref "L2,L3,L4"
    And the timeline markers are "aa-1:milestone,aa-2:ask,bb-1:check,aa-4:done"
