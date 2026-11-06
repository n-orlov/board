Feature: Adding and removing task dependencies from the command line
  board task dep add <ref> <blocked-by-ref> makes a task blocked by another and
  board task dep rm removes the dependency. Each prints one ok line, or the
  object with --json. A dependency that would close a cycle exits 6 with the
  path and the command to run next, and removing a dependency that does not
  exist exits 4.

  Background:
    Given a new database for the board commands
    When I run "board admin migrate"
    And I run "board admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port
    And I run the client 'board init add aa "First one"' as actor "claude@laptop"
    And I run the client 'board init add bb "Second one"' as actor "claude@laptop"
    And 3 tasks exist in the initiative "aa" as actor "claude@laptop"
    And 1 tasks exist in the initiative "bb" as actor "claude@laptop"

  Scenario: dep add and dep rm confirm in one line each
    When I run the client 'board task dep add aa-2 aa-1' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is exactly "ok task aa-2 blocked by aa-1"
    And stderr is empty
    And exactly 1 dep_added event exists for the task "aa-2"
    When I run the client 'board task show aa-2' as actor "claude@laptop"
    Then stdout has the line "blocked by aa-1 (open) Task 1"
    When I run the client 'board task dep rm aa-2 aa-1' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is exactly "ok task aa-2 no longer blocked by aa-1"
    And stderr is empty
    And exactly 1 dep_removed event exists for the task "aa-2"

  Scenario: the refs may be in either letter case and in another initiative
    When I run the client 'board task dep add BB-1 Aa-1' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is exactly "ok task bb-1 blocked by aa-1"

  Scenario: --json prints the dependent task for both commands
    When I run the client 'board task dep add aa-2 aa-1 --json' as actor "claude@laptop"
    Then stdout is one JSON object whose "ref" is "aa-2"
    When I run the client 'board task show aa-2' as actor "claude@laptop"
    Then stdout has the line "aa-2  open  Task 2  (blocked)"
    When I run the client 'board task dep rm --json aa-2 aa-1' as actor "claude@laptop"
    Then the exit code is 0
    Then stdout is one JSON object whose "ref" is "aa-2"
    When I run the client 'board task show aa-2' as actor "claude@laptop"
    Then stdout has the line "aa-2  open  Task 2"

  Scenario: adding a dependency that exists confirms again and writes one event
    Given I run the client 'board task dep add aa-2 aa-1' as actor "claude@laptop"
    When I run the client 'board task dep add aa-2 aa-1' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is exactly "ok task aa-2 blocked by aa-1"
    And exactly 1 dep_added event exists for the task "aa-2"

  Scenario: a cycle exits 6 with the path and the command to run next
    Given I run the client 'board task dep add aa-2 aa-1' as actor "claude@laptop"
    And I run the client 'board task dep add bb-1 aa-2' as actor "claude@laptop"
    When I run the client 'board task dep add aa-1 bb-1' as actor "claude@laptop"
    Then the exit code is 6
    And stdout is empty
    And stderr is exactly one line containing "dependency would close a cycle: aa-1 is blocked by bb-1 is blocked by aa-2 is blocked by aa-1"
    And stderr is exactly one line containing "→ board task show aa-1"
    And exactly 0 dep_added events exist for the task "aa-1"

  Scenario: removing a dependency that does not exist exits 4
    When I run the client 'board task dep rm aa-2 aa-1' as actor "claude@laptop"
    Then the exit code is 4
    And stdout is empty
    And stderr is exactly one line containing "is not blocked by"
    And stderr is exactly one line containing "→ board task show aa-2"
    And exactly 0 dep_removed events exist for the task "aa-2"

  Scenario: an unknown task exits 4 and a task blocked by itself exits 1
    When I run the client 'board task dep add aa-9 aa-1' as actor "claude@laptop"
    Then the exit code is 4
    And stderr is exactly one line containing "→ board task ls"
    When I run the client 'board task dep add aa-1 aa-1' as actor "claude@laptop"
    Then the exit code is 1
    And stdout is empty
    And stderr is exactly one line containing "cannot depend on itself"

  Scenario: a malformed command line exits 2 with the usage line
    When I run the client 'board task dep add aa-2' as actor "claude@laptop"
    Then the exit code is 2
    And stdout is empty
    And stderr is exactly one line containing "dep add|rm <ref> <blocked-by-ref>"
    When I run the client 'board task dep frob aa-2 aa-1' as actor "claude@laptop"
    Then the exit code is 2
    When I run the client 'board task dep' as actor "claude@laptop"
    Then the exit code is 2
