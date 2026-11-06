Feature: Milestones, checks and events from the command line
  board milestone records a milestone on a task and board check records a check
  on a watch task, optionally with the next check due in a given time; each
  prints one ok line, or the task with --json. A check on a work task exits 6
  with the command to run next. board task events lists a task's events oldest
  first and board events lists all events, filtered by --since, --task,
  --initiative, --type (repeatable) and --actor. Both print the same line
  format and accept --json.

  Background:
    Given a new database for the board commands
    When I run "board admin migrate"
    And I run "board admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port
    And I run the client 'board init add aa "First one"' as actor "claude@laptop"
    And I run the client 'board init add bb "Second one"' as actor "claude@laptop"
    And I run the client 'board task add aa "Build it"' as actor "claude@laptop"
    And I run the client 'board task add aa "Watch the build" --kind watch --cadence 1h' as actor "claude@laptop"
    And I run the client 'board task add bb "Other work"' as actor "claude@laptop"

  Scenario: milestone and check confirm in one line each
    When I run the client 'board milestone aa-1 "tests are green"' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is exactly "ok task aa-1 milestone recorded"
    And stderr is empty
    And exactly 1 milestone event exists for the task "aa-1"
    When I run the client 'board check aa-2 "build is queued"' as actor "codex@laptop"
    Then the exit code is 0
    And stdout is exactly "ok task aa-2 checked"
    And stderr is empty
    And exactly 1 check_recorded event exists for the task "aa-2"

  Scenario: check --next moves the next check to that long from now
    When I run the client 'board check aa-2 "waiting for the runner" --next 24h' as actor "codex@laptop"
    Then the exit code is 0
    And stdout is exactly "ok task aa-2 checked"
    When I run the client 'board task show aa-2' as actor "claude@laptop"
    Then stdout has the line "cadence: 1h, next check in 23h"

  Scenario: --json prints the task for both commands
    When I run the client 'board milestone AA-1 "half way" --json' as actor "claude@laptop"
    Then stdout is one JSON object whose "ref" is "aa-1"
    When I run the client 'board check aa-2 "still running" --json' as actor "claude@laptop"
    Then stdout is one JSON object whose "ref" is "aa-2"

  Scenario: a check on a work task exits 6 and names the next command
    When I run the client 'board check aa-1 "is it up"' as actor "claude@laptop"
    Then the exit code is 6
    And stdout is empty
    And stderr is one line ending with "→ board task show aa-1"
    And exactly 0 check_recorded event exists for the task "aa-1"

  Scenario: an unknown task exits 4 for both commands
    When I run the client 'board milestone aa-9 "nothing"' as actor "claude@laptop"
    Then the exit code is 4
    And stderr is one line ending with "→ board task ls"
    When I run the client 'board check aa-9 "nothing"' as actor "claude@laptop"
    Then the exit code is 4
    And stderr is one line ending with "→ board task ls"

  Scenario: a bad --next and a wrong argument count exit 2 before any request
    When I run the client 'board check aa-2 "late" --next soon' as actor "claude@laptop"
    Then the exit code is 2
    And stderr is exactly one line containing "board: --next: invalid duration"
    When I run the client 'board milestone aa-1' as actor "claude@laptop"
    Then the exit code is 2
    And stderr is exactly one line containing "usage: board milestone <task>"

  Scenario: task events lists a task's events oldest first
    When I run the client 'board task edit aa-1 --title "Build it well"' as actor "claude@laptop"
    And I run the client 'board task done aa-1' as actor "claude@laptop"
    And I run the client 'board task reopen aa-1' as actor "claude@laptop"
    And I run the client 'board milestone aa-1 "second try"' as actor "codex@laptop"
    And I run the client 'board task close aa-1 --resolution dropped' as actor "claude@laptop"
    And I run the client 'board task events AA-1' as actor "claude@laptop"
    Then the exit code is 0
    And stdout has 6 lines
    And stdout line 1 matches "^E[0-9]+  (just now|[0-9]+m ago)  claude@laptop  .*created"
    And stdout line 2 matches "^E[0-9]+  (just now|[0-9]+m ago)  claude@laptop  .*updated"
    And stdout line 3 matches "^E[0-9]+  (just now|[0-9]+m ago)  claude@laptop  .*done"
    And stdout line 4 matches "^E[0-9]+  (just now|[0-9]+m ago)  claude@laptop  .*reopened"
    And stdout line 5 matches "^E[0-9]+  (just now|[0-9]+m ago)  codex@laptop  .*second try"
    And stdout line 6 matches "^E[0-9]+  (just now|[0-9]+m ago)  claude@laptop  .*closed"

  Scenario: task events caps the list and prints the events with --json
    When I run the client 'board task events aa-2 --limit 1' as actor "claude@laptop"
    And I run the client 'board milestone aa-2 "m1"' as actor "claude@laptop"
    And I run the client 'board task events aa-2 --limit 1' as actor "claude@laptop"
    Then stdout has 2 lines
    And stdout line 2 matches "^\+1 more → board task events aa-2 --all$"
    When I run the client 'board task events aa-2 --json' as actor "claude@laptop"
    Then stdout is a JSON array of 2 objects with ULID ids

  Scenario: an unknown task has no events
    When I run the client 'board task events aa-9' as actor "claude@laptop"
    Then the exit code is 4
    And stderr is one line ending with "→ board task ls"

  Scenario: events lists everything and --since E<n> keeps the later events
    When I run the client 'board events' as actor "claude@laptop"
    Then the exit code is 0
    And stdout has 5 lines
    And stdout line 1 matches "^E[0-9]+  (just now|[0-9]+m ago)  claude@laptop  "
    When I run the client 'board events --since E1' as actor "claude@laptop"
    Then stdout has 4 lines
    When I run the client 'board milestone aa-1 "earlier"' as actor "claude@laptop"
    And I run the client 'board events --task aa-1' as actor "claude@laptop"
    Then stdout has 2 lines
    And the event ref on line 2 of stdout is remembered
    When I run the client 'board milestone aa-1 "later"' as actor "claude@laptop"
    And I run the client 'board events --since <event>' as actor "claude@laptop"
    Then stdout has 1 line
    And stdout line 1 matches "later"
    When I run the client 'board events --since 2000-01-01' as actor "claude@laptop"
    Then stdout has 7 lines
    When I run the client 'board events --since 2999-01-01' as actor "claude@laptop"
    Then stdout has 0 lines

  Scenario: events filters by task, initiative, type and actor
    When I run the client 'board milestone aa-1 "one"' as actor "claude@laptop"
    And I run the client 'board check aa-2 "two"' as actor "codex@laptop"
    And I run the client 'board milestone bb-1 "three"' as actor "codex@laptop"
    And I run the client 'board events --task aa-1' as actor "claude@laptop"
    Then stdout has 2 lines
    When I run the client 'board events --initiative bb' as actor "claude@laptop"
    Then stdout has 3 lines
    When I run the client 'board events --type milestone' as actor "claude@laptop"
    Then stdout has 2 lines
    When I run the client 'board events --type milestone --type check_recorded' as actor "claude@laptop"
    Then stdout has 3 lines
    When I run the client 'board events --actor codex@laptop' as actor "claude@laptop"
    Then stdout has 2 lines
    And stdout line 1 matches "codex@laptop  .*two"
    When I run the client 'board events --initiative aa --type milestone --actor claude@laptop' as actor "claude@laptop"
    Then stdout has 1 line
    And stdout line 1 matches "one"

  Scenario: events caps its list and prints the events with --json
    When I run the client 'board events --limit 2 --initiative aa' as actor "claude@laptop"
    Then stdout has 3 lines
    And stdout line 3 matches "^\+1 more → board events --initiative aa --all$"
    When I run the client 'board events --initiative aa --json' as actor "claude@laptop"
    Then stdout is a JSON array of 3 objects with ULID ids
    When I run the client 'board events --type no_such_type --json' as actor "claude@laptop"
    Then stdout is exactly "[]"

  Scenario: events refuses an unknown initiative or task and a bad line
    When I run the client 'board events --initiative zz' as actor "claude@laptop"
    Then the exit code is 4
    And stderr is one line ending with "→ board init ls"
    When I run the client 'board events --task zz-1' as actor "claude@laptop"
    Then the exit code is 4
    And stderr is one line ending with "→ board task ls"
    When I run the client 'board events extra' as actor "claude@laptop"
    Then the exit code is 2
    And stderr is exactly one line containing "usage: board events "
