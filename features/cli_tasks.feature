Feature: Adding, listing, showing and editing tasks from the command line
  An agent adds a task with board task add, lists tasks with board task ls,
  reads one with board task show and changes it with board task edit. Each
  write prints one ok line; an unknown task or initiative exits 4, a closed
  one exits 6 and a value the server refuses exits 1, each with the command
  to run next. Flags may come before or after the positional arguments.

  Background:
    Given a new database for the board commands
    When I run "board admin migrate"
    And I run "board admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port
    And I run the client 'board init add aa "First one"' as actor "claude@laptop"
    And I run the client 'board init add bb "Second one"' as actor "claude@laptop"

  Scenario: task add confirms in one line, with the flags before, between and after the positionals
    When I run the client 'board task add aa "Wire the lights"' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is exactly "ok task aa-1 created"
    And stderr is empty
    When I run the client 'board task add aa "Hang the door" --after aa-1 --start 2026-12-01 --end 2026-12-03T17:00:00Z --link "PR 12=https://example.com/pr/12"' as actor "claude@laptop"
    Then stdout is exactly "ok task aa-2 created"
    When I run the client 'board task add --after=aa-1 bb "Other initiative"' as actor "claude@laptop"
    Then stdout is exactly "ok task bb-1 created"
    When I run the client 'board task add aa --kind watch "Watch the build" --cadence=24h' as actor "claude@laptop"
    Then stdout is exactly "ok task aa-3 created"
    And exactly 1 task_created event exists for the task "aa-3"

  Scenario: task ls prints one line per task with its marks and filters by every flag
    Given I run the client 'board task add aa "Wire the lights"' as actor "claude@laptop"
    And I run the client 'board task add aa "Hang the door" --after aa-1' as actor "claude@laptop"
    And I run the client 'board task add aa "Watch the build" --kind watch --cadence 1d' as actor "claude@laptop"
    And I run the client 'board task add bb "Other initiative"' as actor "claude@laptop"
    When I run the client 'board task ls' as actor "claude@laptop"
    Then stdout is:
      """
      aa-1  open  Wire the lights
      aa-2  open  Hang the door  (blocked)
      aa-3  open  Watch the build  (watch)
      bb-1  open  Other initiative
      """
    When I run the client 'board task ls aa --blocked' as actor "claude@laptop"
    Then stdout is:
      """
      aa-2  open  Hang the door  (blocked)
      """
    When I run the client 'board task ls --kind watch' as actor "claude@laptop"
    Then stdout is:
      """
      aa-3  open  Watch the build  (watch)
      """
    When I run the client 'board task ls --q "THE LIGHTS" bb' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is empty
    When I run the client 'board task ls --q "the lights"' as actor "claude@laptop"
    Then stdout is:
      """
      aa-1  open  Wire the lights
      """
    When I run the client 'board task ls --active' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is empty
    When I run the client 'board task ls --overdue' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is empty
    When I start a lease with body "{'task':'aa-1','intent':'wire it'}" as actor "claude@laptop"
    And I run the client 'board task ls --active' as actor "claude@laptop"
    Then stdout is:
      """
      aa-1  open  Wire the lights  (active)
      """
    When I mark the task "bb-1" done as actor "claude@laptop"
    And I run the client 'board task ls bb' as actor "claude@laptop"
    Then stdout is exactly "bb-1  done  Other initiative  !done"
    When I run the client 'board task ls --status all bb' as actor "claude@laptop"
    Then stdout is exactly "bb-1  done  Other initiative  !done"
    When I run the client 'board task ls --status open bb' as actor "claude@laptop"
    Then stdout is empty
    When I run the client 'board task ls --json aa' as actor "claude@laptop"
    Then stdout is a JSON array of 3 objects with ULID ids

  Scenario: task ls of 21 tasks prints 20 lines and a +1 more line
    Given 21 tasks exist in the initiative "aa" as actor "claude@laptop"
    When I run the client 'board task ls' as actor "claude@laptop"
    Then the exit code is 0
    And stdout has 21 lines
    And stdout has the line "aa-1  open  Task 1"
    And stdout line 21 matches "^\+1 more → board task ls --all$"
    When I run the client 'board task ls aa --kind work' as actor "claude@laptop"
    Then stdout line 21 matches "^\+1 more → board task ls aa --kind work --all$"
    When I run the client 'board task ls --all' as actor "claude@laptop"
    Then stdout has 21 lines
    And stdout has no more line
    When I run the client 'board task ls --limit 3' as actor "claude@laptop"
    Then stdout has 4 lines
    And stdout line 4 matches "^\+18 more → board task ls --all$"
    When I run the client 'board task ls --all --json' as actor "claude@laptop"
    Then stdout is a JSON array of 21 objects with ULID ids

  Scenario: task show prints the fields, the dependencies, the blocked tasks and the link
    Given I create the task with body "{'initiative':'aa','title':'Wire the lights','description':'Use the **hub**'}" as actor "claude@laptop"
    And I run the client 'board task add aa "Hang the door" --after aa-1 --start 2026-12-01 --end 2026-12-03T17:00:00Z --link "PR 12=https://example.com/pr/12"' as actor "claude@laptop"
    And I run the client 'board task add aa "Watch the build" --kind watch --cadence 36h' as actor "claude@laptop"
    When I run the client 'board task show aa-2' as actor "claude@laptop"
    Then the exit code is 0
    And stdout has the line "aa-2  open  Hang the door  (blocked)"
    And stdout has the line "initiative: aa"
    And stdout has the line "planned: 2026-12-01 → 2026-12-03T17:00:00Z"
    And stdout has the line "link: PR 12 https://example.com/pr/12"
    And stdout has the line "blocked by aa-1 (open) Wire the lights"
    When I run the client 'board task show AA-1' as actor "claude@laptop"
    Then stdout line 1 matches "^aa-1  open  Wire the lights$"
    And stdout has the line "blocking aa-2 (open) Hang the door"
    And stdout has the line "Use the **hub**"
    When I run the client 'board task show aa-3' as actor "claude@laptop"
    Then stdout line 1 matches "^aa-3  open  Watch the build  \(watch\)$"
    And stdout line 3 matches "^cadence: 36h, next check in (35|36)h$"

  Scenario: task show prints the live leases and only the last 10 events
    Given I run the client 'board task add aa "Wire the lights"' as actor "claude@laptop"
    And I start a lease with body "{'task':'aa-1','intent':'wire it','ttl_seconds':7200}" as actor "owner@laptop"
    And 11 milestones are recorded on the task "aa-1" as actor "claude@laptop"
    When I run the client 'board task show aa-1' as actor "claude@laptop"
    Then the exit code is 0
    And stdout has 15 lines
    And stdout line 1 matches "^aa-1  open  Wire the lights  \(active\)$"
    And stdout line 3 matches "^created by claude@laptop (just now|[0-9]+m ago)$"
    And stdout line 4 matches "^lease L1 by owner@laptop: wire it \(expires in [0-9]+[hm]\)$"
    And stdout line 5 matches "^events:$"
    And stdout line 6 matches "^  E[0-9]+  (just now|[0-9]+m ago)  claude@laptop  milestone on aa-1: m2$"
    And stdout line 15 matches "^  E[0-9]+  (just now|[0-9]+m ago)  claude@laptop  milestone on aa-1: m11$"
    When I run the client 'board task show aa-1 --json' as actor "claude@laptop"
    Then stdout is one JSON object whose "ref" is "aa-1"

  Scenario: task edit changes the fields, confirms in one line and prints the object with --json
    Given I run the client 'board task add aa "Watch the build" --kind watch --cadence 1d' as actor "claude@laptop"
    When I run the client 'board task edit aa-1 --title "Watch the pipeline" --cadence 2d --start 2026-01-02 --link "CI=https://example.com/ci"' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is exactly "ok task aa-1 updated"
    And stderr is empty
    When I run the client 'board task show aa-1' as actor "claude@laptop"
    Then stdout line 1 matches "^aa-1  open  Watch the pipeline  \(watch\)$"
    And stdout has the line "link: CI https://example.com/ci"
    And stdout has the line "cadence: 2d, next check in 23h"
    When I run the client 'board task edit aa-1 --no-links --end 2026-03-04' as actor "claude@laptop"
    Then stdout is exactly "ok task aa-1 updated"
    When I run the client 'board task show aa-1' as actor "claude@laptop"
    Then stdout has the line "planned: 2026-01-02 → 2026-03-04"
    When I run the client 'board task edit aa-1 --title "As JSON" --json' as actor "claude@laptop"
    Then stdout is one JSON object whose "title" is "As JSON"
    And exactly 3 task_updated events exist for the task "aa-1"
    When I run the client 'board task add aa "Plain" --json' as actor "claude@laptop"
    Then stdout is one JSON object whose "ref" is "aa-2"

  Scenario: an unknown task or initiative exits 4 with the command to run next
    When I run the client 'board task show aa-9' as actor "claude@laptop"
    Then the exit code is 4
    And stdout is empty
    And stderr is exactly one line containing "→ board task ls"
    When I run the client 'board task edit aa-9 --title "X"' as actor "claude@laptop"
    Then the exit code is 4
    And stderr is exactly one line containing "→ board task ls"
    When I run the client 'board task add nope-nope "X"' as actor "claude@laptop"
    Then the exit code is 4
    And stderr is exactly one line containing "→ board init ls"
    When I run the client 'board task ls nope-nope' as actor "claude@laptop"
    Then the exit code is 4
    And stderr is exactly one line containing "→ board init ls"

  Scenario: a value the server refuses exits 1, a closed task or initiative exits 6
    Given I run the client 'board task add aa "Wire the lights"' as actor "claude@laptop"
    When I run the client 'board task add aa "A watch task" --kind watch' as actor "claude@laptop"
    Then the exit code is 1
    And stdout is empty
    And stderr is exactly one line containing "cadence"
    When I run the client 'board task add aa "A work task" --cadence 1d' as actor "claude@laptop"
    Then the exit code is 1
    And stderr is exactly one line containing "cadence"
    When I run the client 'board task edit aa-1 --link "X=ftp://example.com"' as actor "claude@laptop"
    Then the exit code is 1
    And stdout is empty
    When I close the task "aa-1" with body "{'resolution':'dropped'}" as actor "claude@laptop"
    And I run the client 'board task edit aa-1 --title "Too late"' as actor "claude@laptop"
    Then the exit code is 6
    And stdout is empty
    And stderr is exactly one line containing "→ board task show aa-1"
    When I run the client 'board init close aa --force' as actor "claude@laptop"
    And I run the client 'board task add aa "In a closed initiative"' as actor "claude@laptop"
    Then the exit code is 6
    And stderr is exactly one line containing "→ board init show aa"

  Scenario: a malformed command line exits 2 with one line
    When I run the client 'board task add aa' as actor "claude@laptop"
    Then the exit code is 2
    And stdout is empty
    And stderr is exactly one line containing "usage: board task add"
    When I run the client 'board task add aa "T" --cadence 5' as actor "claude@laptop"
    Then the exit code is 2
    And stderr is exactly one line containing "board: --cadence: invalid duration"
    When I run the client 'board task add aa "T" --start tomorrow' as actor "claude@laptop"
    Then the exit code is 2
    And stderr is exactly one line containing "board: --start: invalid time"
    When I run the client 'board task ls --status weird' as actor "claude@laptop"
    Then the exit code is 2
    And stderr is exactly one line containing "usage: board task"
    When I run the client 'board task show' as actor "claude@laptop"
    Then the exit code is 2
    When I run the client 'board task edit aa-1' as actor "claude@laptop"
    Then the exit code is 2
    And stderr is exactly one line containing "usage: board task"
    When I run the client 'board task ls --all --limit 2' as actor "claude@laptop"
    Then the exit code is 2
    When I run the client 'board task frobnicate' as actor "claude@laptop"
    Then the exit code is 2
