Feature: The check-in from the command line
  board now prints who I am and three sections, my leases, others and conflicts,
  one line each. A conflict line starts with CONFLICT. It exits 7 when a conflict
  touches one of my live leases or a resource given with --resources, else 0.

  Background:
    Given a new database for the board commands
    When I run "board admin migrate"
    And I run "board admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port
    And I run the client 'board init add aa "First one"' as actor "claude@laptop"
    And 3 tasks exist in the initiative "aa" as actor "claude@laptop"

  Scenario: with no leases every section is empty and the exit code is 0
    When I run the client 'board now' as actor "claude@laptop"
    Then the exit code is 0
    And stderr is empty
    And stdout has 4 lines
    And stdout line 1 matches '^you: claude@laptop$'
    And stdout line 2 matches '^my leases: none$'
    And stdout line 3 matches '^others: none$'
    And stdout line 4 matches '^conflicts: none$'

  Scenario: my lease is listed under my leases only
    Given I run the client 'board lease start aa-1 "wiring" --ttl 3h --res repo:x' as actor "claude@laptop"
    When I run the client 'board now' as actor "claude@laptop"
    Then the exit code is 0
    And stdout has 5 lines
    And stdout line 2 matches '^my leases:$'
    And stdout line 3 matches '^  L1  aa-1  Task 1  "wiring"  res repo:x  exp (3h|2h59m)$'
    And stdout line 4 matches '^others: none$'
    And stdout line 5 matches '^conflicts: none$'

  Scenario: a lease of another actor is listed under others only
    Given I run the client 'board lease start aa-2 "testing" --res repo:y' as actor "codex@laptop"
    When I run the client 'board now' as actor "claude@laptop"
    Then the exit code is 0
    And stdout line 2 matches '^my leases: none$'
    And stdout line 3 matches '^others:$'
    And stdout line 4 matches '^  L1  aa-2  codex@laptop  "testing"  res repo:y  exp (2h|1h59m)$'
    And stdout line 5 matches '^conflicts: none$'

  Scenario: a conflict on my lease prints a CONFLICT line and exits 7
    Given I run the client 'board lease start aa-2 "testing" --res repo:y' as actor "codex@laptop"
    And I run the client 'board lease start aa-1 "wiring" --res repo:y --res repo:z' as actor "claude@laptop"
    When I run the client 'board now' as actor "claude@laptop"
    Then the exit code is 7
    And stderr is empty
    And stdout line 2 matches '^my leases:$'
    And stdout line 3 matches '^  L2  aa-1  Task 1  "wiring"  res repo:y,repo:z  exp '
    And stdout line 4 matches '^others:$'
    And stdout line 5 matches '^  L1  aa-2  codex@laptop  "testing"  res repo:y  exp '
    And stdout line 6 matches '^conflicts:$'
    And stdout line 7 matches '^  CONFLICT repo:y: L1 codex@laptop \(aa-2\) "testing" exp '

  Scenario: a conflict on a resource given with --resources exits 7 and without it exits 0
    Given I run the client 'board lease start aa-2 "testing" --res host:gw1' as actor "codex@laptop"
    When I run the client 'board now --resources host:gw1,repo:q' as actor "claude@laptop"
    Then the exit code is 7
    And stdout line 2 matches '^my leases: none$'
    And stdout line 6 matches '^  CONFLICT host:gw1: L1 codex@laptop \(aa-2\) "testing" exp '
    When I run the client 'board now --resources repo:q' as actor "claude@laptop"
    Then the exit code is 0
    And stdout line 5 matches '^conflicts: none$'
    When I run the client 'board now' as actor "claude@laptop"
    Then the exit code is 0
    When I run the client 'board now --resources=host:gw1' as actor "claude@laptop"
    Then the exit code is 7

  Scenario: no conflict exits 0 when leases hold different resources
    Given I run the client 'board lease start aa-2 "testing" --res repo:y' as actor "codex@laptop"
    And I run the client 'board lease start aa-1 "wiring" --res repo:x' as actor "claude@laptop"
    When I run the client 'board now' as actor "claude@laptop"
    Then the exit code is 0
    And stdout line 6 matches '^conflicts: none$'

  Scenario: --json prints the answer of the server and keeps the exit code
    Given I run the client 'board lease start aa-2 "testing" --res repo:y' as actor "codex@laptop"
    And I run the client 'board lease start aa-1 "wiring" --res repo:y' as actor "claude@laptop"
    When I run the client 'board now --json' as actor "claude@laptop"
    Then the exit code is 7
    And stdout JSON path "actor" is "claude@laptop"
    And stdout JSON path "my_leases" has 1 items
    And stdout JSON path "my_leases.0.title" is "Task 1"
    And stdout JSON path "others_leases" has 1 items
    And stdout JSON path "conflicts" has 1 items
    And stdout JSON path "conflicts.0.lease" is "L1"
    When I run the client 'board now --json --resources repo:none' as actor "claude@laptop"
    Then the exit code is 7
    When I run the client 'board lease end L2' as actor "claude@laptop"
    And I run the client 'board now --json' as actor "claude@laptop"
    Then the exit code is 0
    And stdout JSON path "my_leases" has 0 items
    And stdout JSON path "conflicts" has 0 items

  Scenario: a section shows five lines and then how to see the rest
    Given 7 tasks exist in the initiative "aa" as actor "other@laptop"
    And I run the client 'board lease start aa-4 "a"' as actor "codex@laptop"
    And I run the client 'board lease start aa-5 "b"' as actor "codex@laptop"
    And I run the client 'board lease start aa-6 "c"' as actor "codex@laptop"
    And I run the client 'board lease start aa-7 "d"' as actor "codex@laptop"
    And I run the client 'board lease start aa-8 "e"' as actor "codex@laptop"
    And I run the client 'board lease start aa-9 "f"' as actor "codex@laptop"
    When I run the client 'board now' as actor "claude@laptop"
    Then the exit code is 0
    And stdout has 10 lines
    And stdout line 4 matches '^  L1  aa-4  codex@laptop  "a"'
    And stdout line 8 matches '^  L5  aa-8  codex@laptop  "e"'
    And stdout line 9 matches '^  \+1 more → board lease ls$'
    And stdout line 10 matches '^conflicts: none$'

  Scenario: a malformed command line exits 2 and a resource the server refuses exits 1
    When I run the client 'board now extra' as actor "claude@laptop"
    Then the exit code is 2
    And stderr is exactly 'usage: board now [--resources r1,r2] [--json]'
    When I run the client 'board now --bogus' as actor "claude@laptop"
    Then the exit code is 2
    When I run the client 'board now --resources a:b,,c:d' as actor "claude@laptop"
    Then the exit code is 1
    And stdout is empty
    When I run the client 'board now --resources ""' as actor "claude@laptop"
    Then the exit code is 0
