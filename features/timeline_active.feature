Feature: Timeline active filter
  GET /api/v1/timeline?active=true keeps only the active tasks. A task is active when it has a live lease, or any
  attention reason (stranded, asking or done and not closed), or an event in the last window (8h, 24h by default, or
  3d, counted back from now). active adds to the other filters; a window that is not one of the three is 400. Event
  times are backdated through the database, never waited out, and the sweeper is off so a lease that ran out is
  expired by the read itself.

  Background:
    Given a new database for the board commands
    When I run "vh admin migrate"
    And I run "vh admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port with BOARD_SWEEP_INTERVAL=off
    And I create the initiative "aa" titled "First" with tags "web" as actor "claude@laptop"
    And I create the initiative "bb" titled "Second" with no tags as actor "claude@laptop"
    And I create a task in "aa" titled "leased" as actor "claude@laptop"
    And I create a task in "aa" titled "asking" as actor "claude@laptop"
    And I create a task in "aa" titled "touched" as actor "claude@laptop"
    And I create a task in "aa" titled "idle" as actor "claude@laptop"
    And I create a task in "bb" titled "other leased" as actor "claude@laptop"
    And I start a lease with body "{'task':'aa-1','intent':'working'}" as actor "claude@laptop"
    And I start a lease with body "{'task':'bb-1','intent':'working'}" as actor "claude@laptop"
    And I raise an ask with body "{'task':'aa-2','question':'Which database?'}" as actor "codex@laptop"
    And I record a milestone on the task "aa-3" with body "{'summary':'started'}" as actor "claude@laptop"
    And every event on the board is 100 hours old
    And the events of the task "aa-3" are 10 hours old

  Scenario: without active every task is on the timeline whether or not it is active
    When I request the timeline with "" as actor "operator@laptop"
    Then the response status is 200
    And the timeline tasks have ref "aa-1,aa-2,aa-3,aa-4,bb-1"
    When I request the timeline with "active=false" as actor "operator@laptop"
    Then the timeline tasks have ref "aa-1,aa-2,aa-3,aa-4,bb-1"
    When I request the timeline with "window=8h" as actor "operator@laptop"
    Then the timeline tasks have ref "aa-1,aa-2,aa-3,aa-4,bb-1"

  Scenario: active keeps a leased task, an attention task and a task touched in the window and leaves out an untouched one
    When I request the timeline with "active=true" as actor "operator@laptop"
    Then the response status is 200
    And the timeline tasks have ref "aa-1,aa-2,aa-3,bb-1"
    And the timeline leases have ref "L1,L2"
    And the response path "tasks.1.attention.0.reason" is "asking"

  Scenario: the window decides which events make a task active
    When I request the timeline with "active=true&window=8h" as actor "operator@laptop"
    Then the timeline tasks have ref "aa-1,aa-2,bb-1"
    When I request the timeline with "active=true&window=24h" as actor "operator@laptop"
    Then the timeline tasks have ref "aa-1,aa-2,aa-3,bb-1"
    When I request the timeline with "active=true&window=3d" as actor "operator@laptop"
    Then the timeline tasks have ref "aa-1,aa-2,aa-3,bb-1"

  Scenario: a task whose only touch is older than every window is active only by its lease or attention
    When I end the lease "L1" with body "{'outcome':'wrapped up'}" as actor "claude@laptop"
    And the events of the task "aa-1" are 100 hours old
    And I request the timeline with "active=true&window=3d" as actor "operator@laptop"
    Then the timeline tasks have ref "aa-2,aa-3,bb-1"

  Scenario: active adds to the other filters
    When I request the timeline with "active=true&initiative=bb" as actor "operator@laptop"
    Then the timeline tasks have ref "bb-1"
    When I request the timeline with "active=true&tag=web" as actor "operator@laptop"
    Then the timeline tasks have ref "aa-1,aa-2,aa-3"

  Scenario: a window that is not 8h, 24h or 3d is refused
    When I request the timeline with "active=true&window=2h" as actor "operator@laptop"
    Then the response status is 400
    When I request the timeline with "window=1d" as actor "operator@laptop"
    Then the response status is 400
    When I request the timeline with "active=maybe" as actor "operator@laptop"
    Then the response status is 400
