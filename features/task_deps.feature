Feature: Task dependencies over HTTP
  POST /api/v1/tasks/{ref}/deps/{blockedByRef} makes a task blocked by another and
  DELETE removes the dependency. The two tasks may be in different initiatives.
  Each change writes a dep_added or dep_removed event on the dependent task. A
  dependency that exists is added again with no event, a task cannot depend on
  itself, and a dependency that would close a cycle of any length is refused with
  the path. A task is blocked while a task it depends on is not closed or while it has an
  open blocking ask (asks_blocking.feature).

  Background:
    Given a new database for the board commands
    When I run "board admin migrate"
    And I run "board admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port
    And I create the initiative "aa" titled "First" with no tags as actor "claude@laptop"
    And I create the initiative "bb" titled "Second" with no tags as actor "claude@laptop"
    And I create a task in "aa" titled "one" as actor "claude@laptop"
    And I create a task in "aa" titled "two" as actor "claude@laptop"
    And I create a task in "bb" titled "three" as actor "claude@laptop"

  Scenario: adding a dependency answers the dependent task and writes dep_added
    When I make the task "aa-2" blocked by "aa-1" as actor "codex@laptop"
    Then the response status is 200
    And the response path "ref" is "aa-2"
    And the response path "blocked" is "true"
    And the response path "deps" lists the refs "aa-1"
    And the response path "deps.0.title" is "one"
    And exactly 1 dep_added event exists for the task "aa-2"
    And exactly 0 dep_added events exist for the task "aa-1"
    And the latest dep_added event of "aa-2" has data.blocked_by "aa-1"

  Scenario: adding the same dependency again answers 200 and writes nothing
    Given I make the task "aa-2" blocked by "aa-1" as actor "claude@laptop"
    And I note the counts of tasks, dependencies, events and actors
    When I make the task "aa-2" blocked by "aa-1" as actor "someone-else@laptop"
    Then the response status is 200
    And the response path "deps" lists the refs "aa-1"
    And exactly 1 dep_added event exists for the task "aa-2"
    And the counts of tasks, dependencies, events and actors are unchanged

  Scenario: removing a dependency answers the dependent task and writes dep_removed
    Given I make the task "aa-2" blocked by "aa-1" as actor "claude@laptop"
    When I remove the dependency of the task "aa-2" on "aa-1" as actor "codex@laptop"
    Then the response status is 200
    And the response path "deps" has 0 items
    And the response path "blocked" is "false"
    And exactly 1 dep_removed event exists for the task "aa-2"
    And the latest dep_removed event of "aa-2" has data.blocked_by "aa-1"

  Scenario: removing a dependency that does not exist is 404 and writes nothing
    Given I note the counts of tasks, dependencies, events and actors
    When I remove the dependency of the task "aa-2" on "aa-1" as actor "refused@laptop"
    Then the response status is 404
    And the problem detail contains "aa-2 is not blocked by aa-1"
    And the counts of tasks, dependencies, events and actors are unchanged

  Scenario: a task that does not exist is 404
    When I make the task "aa-2" blocked by "aa-9" as actor "claude@laptop"
    Then the response status is 404
    When I remove the dependency of the task "aa-9" on "aa-1" as actor "claude@laptop"
    Then the response status is 404

  Scenario: a task cannot depend on itself
    Given I note the counts of tasks, dependencies, events and actors
    When I make the task "aa-1" blocked by "aa-1" as actor "refused@laptop"
    Then the response status is 400
    And the problem detail contains "cannot depend on itself"
    And the counts of tasks, dependencies, events and actors are unchanged

  Scenario: a dependency may cross initiatives and both tasks list it
    When I make the task "bb-1" blocked by "aa-1" as actor "claude@laptop"
    Then the response status is 200
    And the response path "deps" lists the refs "aa-1"
    When I request the task "aa-1" as actor "claude@laptop"
    Then the response path "blocking" lists the refs "bb-1"
    And the response path "blocking.0.title" is "three"
    And the response path "blocking.0.status" is "open"
    And the response path "blocked" is "false"
    When I request the task "bb-1" as actor "claude@laptop"
    Then the response path "deps" lists the refs "aa-1"
    And the response path "blocking" has 0 items

  Scenario: the refs may be written in either letter case
    When I make the task "BB-1" blocked by "Aa-1" as actor "claude@laptop"
    Then the response status is 200
    And the response path "deps" lists the refs "aa-1"

  Scenario: a 2-cycle is refused with its path
    Given I make the task "aa-2" blocked by "aa-1" as actor "claude@laptop"
    And I note the counts of tasks, dependencies, events and actors
    When I make the task "aa-1" blocked by "aa-2" as actor "refused@laptop"
    Then the response status is 409
    And the problem detail contains "dependency would close a cycle: aa-1 is blocked by aa-2 is blocked by aa-1"
    And the counts of tasks, dependencies, events and actors are unchanged
    And exactly 0 dep_added events exist for the task "aa-1"

  Scenario: a 3-cycle across initiatives is refused with its path
    Given I make the task "aa-2" blocked by "aa-1" as actor "claude@laptop"
    And I make the task "bb-1" blocked by "aa-2" as actor "claude@laptop"
    And I note the counts of tasks, dependencies, events and actors
    When I make the task "aa-1" blocked by "bb-1" as actor "refused@laptop"
    Then the response status is 409
    And the problem detail contains "aa-1 is blocked by bb-1 is blocked by aa-2 is blocked by aa-1"
    And the counts of tasks, dependencies, events and actors are unchanged

  Scenario: a task is blocked while its blocker is done and not closed
    Given I make the task "aa-2" blocked by "aa-1" as actor "claude@laptop"
    When I mark the task "aa-1" done as actor "claude@laptop"
    And I request the task "aa-2" as actor "claude@laptop"
    Then the response path "blocked" is "true"
    And the response path "deps.0.status" is "done"
    When I close the task "aa-1" with body "{'resolution':'completed'}" as actor "claude@laptop"
    And I request the task "aa-2" as actor "claude@laptop"
    Then the response path "blocked" is "false"
    And the response path "deps.0.status" is "closed"

  Scenario: a closed blocker that is reopened blocks again
    Given I make the task "aa-2" blocked by "aa-1" as actor "claude@laptop"
    And I close the task "aa-1" with body "{'resolution':'dropped'}" as actor "claude@laptop"
    When I reopen the task "aa-1" as actor "claude@laptop"
    And I request the task "aa-2" as actor "claude@laptop"
    Then the response path "blocked" is "true"
