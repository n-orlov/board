Feature: Attention from the command line
  board attention prints the five sections of what needs the operator, each
  with its count and one line per item: open asks, done tasks not closed,
  expired leases, overdue watches and live conflicts. --tag (repeated) and
  --init narrow it, --limit and --all change the five-line cap of a section,
  and --json returns the API document.

  Background:
    Given a new database for the board commands
    When I run "board admin migrate"
    And I run "board admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port
    And I run the client 'board init add aa "First one" --tag web --tag team' as actor "claude@laptop"
    And I run the client 'board init add bb "Second one" --tag web' as actor "claude@laptop"
    And 3 tasks exist in the initiative "aa" as actor "claude@laptop"
    And I run the client 'board task add aa "poll the build" --kind watch --cadence 1h' as actor "claude@laptop"
    And I run the client 'board task add bb "ship it"' as actor "claude@laptop"

  Scenario: an empty board prints the five sections with count 0
    When I run the client 'board attention' as actor "operator@laptop"
    Then the exit code is 0
    And stderr is empty
    And stdout has 5 lines
    And stdout line 1 matches '^open asks: 0$'
    And stdout line 2 matches '^done, not closed: 0$'
    And stdout line 3 matches '^expired leases: 0$'
    And stdout line 4 matches '^overdue watches: 0$'
    And stdout line 5 matches '^conflicts: 0$'

  Scenario: a board with the five sections populated prints one line per item under its section
    Given I run the client 'board ask aa-1 "Which database?" --blocking' as actor "claude@laptop"
    And I run the client 'board lease start bb-1 "building" --ttl 1h' as actor "claude@laptop"
    And I run the client 'board lease end L1 "shipped"' as actor "claude@laptop"
    And I run the client 'board task done bb-1' as actor "codex@laptop"
    And I run the client 'board lease start aa-3 "migrate" --ttl 1h' as actor "gemini@laptop"
    And the lease "L2" ran out 30 seconds ago
    And I record a check on the task "aa-4" with body "{'summary':'build is red','next_check_at':'2020-01-01T00:00:00Z'}" as actor "codex@laptop"
    And I run the client 'board lease start aa-1 "one" --res db:x' as actor "claude@laptop"
    And I run the client 'board lease start aa-2 "two" --res db:x' as actor "codex@laptop"
    When I run the client 'board attention' as actor "operator@laptop"
    Then the exit code is 0
    And stderr is empty
    And stdout has 10 lines
    And stdout line 1 matches '^open asks: 1$'
    And stdout line 2 matches '^  A1  (just now|\d+m ago)  aa-1  claude@laptop  "Which database\?"  blocking$'
    And stdout line 3 matches '^done, not closed: 1$'
    And stdout line 4 matches '^  bb-1  ship it  done by codex@laptop (just now|\d+m ago)  L1 "shipped"$'
    And stdout line 5 matches '^expired leases: 1$'
    And stdout line 6 matches '^  L2  aa-3  gemini@laptop  "migrate"  renewed (just now|\d+m ago)$'
    And stdout line 7 matches '^overdue watches: 1$'
    And stdout line 8 matches '^  aa-4  poll the build  claude@laptop  due \d+d ago  last check "build is red" by codex@laptop (just now|\d+m ago)$'
    And stdout line 9 matches '^conflicts: 1$'
    And stdout line 10 matches '^  CONFLICT db:x: L3 claude@laptop aa-1, L4 codex@laptop aa-2$'

  Scenario: --init keeps the items of one initiative
    Given I run the client 'board ask aa-1 "Which database?"' as actor "claude@laptop"
    And I run the client 'board task done bb-1' as actor "codex@laptop"
    When I run the client 'board attention --init bb' as actor "operator@laptop"
    Then the exit code is 0
    And stdout has 6 lines
    And stdout line 1 matches '^open asks: 0$'
    And stdout line 2 matches '^done, not closed: 1$'
    And stdout line 3 matches '^  bb-1  ship it  done by codex@laptop '
    And stdout line 4 matches '^expired leases: 0$'
    When I run the client 'board attention --init aa' as actor "operator@laptop"
    Then stdout has 6 lines
    And stdout line 1 matches '^open asks: 1$'
    And stdout line 2 matches '^  A1  .*  aa-1  claude@laptop  "Which database\?"$'
    And stdout line 3 matches '^done, not closed: 0$'

  Scenario: --tag, repeated, keeps the items of initiatives that carry every tag
    Given I run the client 'board ask aa-1 "in aa"' as actor "claude@laptop"
    And I run the client 'board ask bb-1 "in bb"' as actor "claude@laptop"
    When I run the client 'board attention --tag web' as actor "operator@laptop"
    Then stdout has 7 lines
    And stdout line 1 matches '^open asks: 2$'
    When I run the client 'board attention --tag web --tag team' as actor "operator@laptop"
    Then stdout has 6 lines
    And stdout line 1 matches '^open asks: 1$'
    And stdout line 2 matches '"in aa"'
    When I run the client 'board attention --tag team --init bb' as actor "operator@laptop"
    Then stdout has 5 lines
    And stdout line 1 matches '^open asks: 0$'

  Scenario: a section shows five items and says how to see the rest
    Given I run the client 'board ask aa-1 "q1"' as actor "claude@laptop"
    And I run the client 'board ask aa-1 "q2"' as actor "claude@laptop"
    And I run the client 'board ask aa-1 "q3"' as actor "claude@laptop"
    And I run the client 'board ask aa-1 "q4"' as actor "claude@laptop"
    And I run the client 'board ask aa-1 "q5"' as actor "claude@laptop"
    And I run the client 'board ask aa-1 "q6"' as actor "claude@laptop"
    When I run the client 'board attention' as actor "operator@laptop"
    Then stdout has 11 lines
    And stdout line 1 matches '^open asks: 6$'
    And stdout line 2 matches '"q6"'
    And stdout line 6 matches '"q2"'
    And stdout line 7 matches '^  \+1 more → board attention --all$'
    When I run the client 'board attention --all --init aa' as actor "operator@laptop"
    Then stdout has 11 lines
    And stdout line 7 matches '"q1"'
    And stdout line 8 matches '^done, not closed: 0$'
    When I run the client 'board attention --limit 2 --tag web' as actor "operator@laptop"
    Then stdout has 8 lines
    And stdout line 4 matches '^  \+4 more → board attention --tag web --all$'

  Scenario: --json returns the API document
    Given I run the client 'board ask aa-1 "Which database?"' as actor "claude@laptop"
    When I run the client 'board attention --json' as actor "operator@laptop"
    Then the exit code is 0
    And stdout JSON path "counts.open_asks" is "1"
    And stdout JSON path "counts.total" is "1"
    And stdout JSON path "open_asks" has 1 items
    And stdout JSON path "open_asks.0.question" is "Which database?"
    And stdout JSON path "open_asks.0.asker.handle" is "claude@laptop"
    And stdout JSON path "done_not_closed" has 0 items
    And stdout JSON path "conflicts" has 0 items
    When I run the client 'board attention --json --init bb' as actor "operator@laptop"
    Then stdout JSON path "counts.total" is "0"
    And stdout JSON path "open_asks" has 0 items

  Scenario: an unknown initiative exits 4 with the next action and a bad command line exits 2
    When I run the client 'board attention --init zz' as actor "operator@laptop"
    Then the exit code is 4
    And stdout is empty
    And stderr is one line ending with '→ board init ls'
    When I run the client 'board attention now' as actor "operator@laptop"
    Then the exit code is 2
    And stdout is empty
    And stderr is exactly 'usage: board attention [--tag t]... [--init slug] [--limit N|--all] [--json]'
    When I run the client 'board attention --limit 0' as actor "operator@laptop"
    Then the exit code is 2
