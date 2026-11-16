Feature: Asks and the blocked state of a task
  A task is blocked while it has an open dependency or an open ask whose blocking is true.
  GET /api/v1/tasks/{ref} lists the task's open asks as open_asks, blocking or not. An ask that is
  answered or withdrawn is not open and no longer blocks. GET /api/v1/tasks?blocked=true follows the
  same rule, and the task's events list carries ask_raised, ask_answered and ask_withdrawn.

  Background:
    Given a new database for the board commands
    When I run "vh admin migrate"
    And I run "vh admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port
    And I create the initiative "aa" titled "First" with no tags as actor "claude@laptop"
    And I create a task in "aa" titled "one" as actor "claude@laptop"
    And I create a task in "aa" titled "two" as actor "claude@laptop"

  Scenario: a task with no ask is not blocked and lists no open asks
    When I request the task "aa-1" as actor "claude@laptop"
    Then the response path "blocked" is "false"
    And the response path "open_asks" has 0 items

  Scenario: an open blocking ask blocks the task and is listed
    When I raise an ask with body "{'task':'aa-1','question':'Which database?','blocking':true}" as actor "claude@laptop"
    And I request the task "aa-1" as actor "claude@laptop"
    Then the response path "blocked" is "true"
    And the response path "open_asks" has 1 items
    And the response path "open_asks.0.ref" is "A1"
    And the response path "open_asks.0.question" is "Which database?"
    And the response path "open_asks.0.blocking" is "true"
    And the response path "open_asks.0.state" is "open"
    And the response path "open_asks.0.task" is "aa-1"

  Scenario: an open ask that is not blocking is listed and does not block
    When I raise an ask with body "{'task':'aa-1','question':'Any preference?'}" as actor "claude@laptop"
    And I request the task "aa-1" as actor "claude@laptop"
    Then the response path "blocked" is "false"
    And the response path "open_asks" has 1 items
    And the response path "open_asks.0.blocking" is "false"

  Scenario: answering the blocking ask unblocks the task and drops it from open_asks
    Given I raise an ask with body "{'task':'aa-1','question':'Which database?','blocking':true}" as actor "claude@laptop"
    When I answer the ask "A1" with body "{'answer':'Postgres'}" as actor "operator@laptop"
    And I request the task "aa-1" as actor "claude@laptop"
    Then the response path "blocked" is "false"
    And the response path "open_asks" has 0 items

  Scenario: withdrawing the blocking ask unblocks the task
    Given I raise an ask with body "{'task':'aa-1','question':'Which database?','blocking':true}" as actor "claude@laptop"
    When I withdraw the ask "A1" with body "{'reason':'decided myself'}" as actor "claude@laptop"
    And I request the task "aa-1" as actor "claude@laptop"
    Then the response path "blocked" is "false"
    And the response path "open_asks" has 0 items

  Scenario: a task stays blocked while one of two blocking asks is still open
    Given I raise an ask with body "{'task':'aa-1','question':'First?','blocking':true}" as actor "claude@laptop"
    And I raise an ask with body "{'task':'aa-1','question':'Second?','blocking':true}" as actor "claude@laptop"
    When I answer the ask "A1" with body "{'answer':'yes'}" as actor "operator@laptop"
    And I request the task "aa-1" as actor "claude@laptop"
    Then the response path "blocked" is "true"
    And the response path "open_asks" has 1 items
    And the response path "open_asks.0.ref" is "A2"

  Scenario: an ask on another task or on the initiative does not block this task
    Given I raise an ask with body "{'task':'aa-2','question':'Other?','blocking':true}" as actor "claude@laptop"
    And I raise an ask with body "{'initiative':'aa','question':'Whole initiative?','blocking':true}" as actor "claude@laptop"
    When I request the task "aa-1" as actor "claude@laptop"
    Then the response path "blocked" is "false"
    And the response path "open_asks" has 0 items

  Scenario: an open dependency still blocks a task with no ask
    Given I make the task "aa-2" blocked by "aa-1" as actor "claude@laptop"
    When I request the task "aa-2" as actor "claude@laptop"
    Then the response path "blocked" is "true"
    And the response path "open_asks" has 0 items

  Scenario: the dependency keeps the task blocked after its blocking ask is answered
    Given I make the task "aa-2" blocked by "aa-1" as actor "claude@laptop"
    And I raise an ask with body "{'task':'aa-2','question':'Which one?','blocking':true}" as actor "claude@laptop"
    When I answer the ask "A1" with body "{'answer':'this one'}" as actor "operator@laptop"
    And I request the task "aa-2" as actor "claude@laptop"
    Then the response path "blocked" is "true"
    And the response path "open_asks" has 0 items

  Scenario: GET /tasks?blocked=true includes the task while its blocking ask is open and excludes it after
    Given I raise an ask with body "{'task':'aa-1','question':'Which database?','blocking':true}" as actor "claude@laptop"
    When I list the tasks with "blocked=true" as actor "claude@laptop"
    Then the response path "items" lists the refs "aa-1"
    And the response path "items.0.blocked" is "true"
    When I list the tasks with "blocked=false" as actor "claude@laptop"
    Then the response path "items" lists the refs "aa-2"
    When I answer the ask "A1" with body "{'answer':'Postgres'}" as actor "operator@laptop"
    And I list the tasks with "blocked=true" as actor "claude@laptop"
    Then the response path "items" has 0 items
    When I list the tasks with "blocked=false" as actor "claude@laptop"
    Then the response path "items" lists the refs "aa-1,aa-2"

  Scenario: a non-blocking ask leaves the task out of blocked=true
    Given I raise an ask with body "{'task':'aa-1','question':'Any preference?'}" as actor "claude@laptop"
    When I list the tasks with "blocked=true" as actor "claude@laptop"
    Then the response path "items" has 0 items

  Scenario: the events of the task show ask_raised, ask_answered and ask_withdrawn
    Given I raise an ask with body "{'task':'aa-1','question':'First?','blocking':true}" as actor "claude@laptop"
    And I raise an ask with body "{'task':'aa-1','question':'Second?'}" as actor "claude@laptop"
    When I answer the ask "A1" with body "{'answer':'yes'}" as actor "operator@laptop"
    And I withdraw the ask "A2" with body "{'reason':'moot'}" as actor "claude@laptop"
    And I request the events of the task "aa-1" as actor "claude@laptop"
    Then the response items have the types "task_created,ask_raised,ask_raised,ask_answered,ask_withdrawn"
