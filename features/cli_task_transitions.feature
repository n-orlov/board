Feature: Marking tasks done, reopening and closing them from the command line
  An agent marks a task done with board task done, closes it with board task
  close (resolution completed unless --resolution says otherwise) and reopens it
  with board task reopen. Each write prints one ok line, or the object with
  --json. A move the task's status does not allow exits 6 and a missing task
  exits 4, each with the command to run next. A close made for the operator
  with --for operator is stored as the on_behalf_of of its task_closed event.

  Background:
    Given a new database for the board commands
    When I run "board admin migrate"
    And I run "board admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port
    And I run the client 'board init add aa "First one"' as actor "claude@laptop"
    And 3 tasks exist in the initiative "aa" as actor "claude@laptop"

  Scenario: done, reopen and close confirm in one line each
    When I run the client 'board task done aa-1' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is exactly "ok task aa-1 done"
    And stderr is empty
    And exactly 1 task_done event exists for the task "aa-1"
    When I run the client 'board task reopen aa-1' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is exactly "ok task aa-1 reopened"
    And exactly 1 task_reopened event exists for the task "aa-1"
    When I run the client 'board task close aa-1' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is exactly "ok task aa-1 closed"
    And stderr is empty
    When I run the client 'board task reopen AA-1' as actor "claude@laptop"
    Then stdout is exactly "ok task aa-1 reopened"

  Scenario: close without --resolution is completed and --resolution picks the others
    When I run the client 'board task close aa-1' as actor "claude@laptop"
    Then the exit code is 0
    And the latest task_closed event of "aa-1" has data.resolution "completed"
    When I run the client 'board task close aa-2 --resolution dropped' as actor "claude@laptop"
    Then stdout is exactly "ok task aa-2 closed"
    And the latest task_closed event of "aa-2" has data.resolution "dropped"
    When I run the client 'board task close --resolution=duplicate aa-3' as actor "claude@laptop"
    Then the latest task_closed event of "aa-3" has data.resolution "duplicate"
    When I run the client 'board task show aa-2' as actor "claude@laptop"
    Then stdout line 1 matches "^aa-2  closed  Task 2$"

  Scenario: a done task closes and keeps the done record
    When I run the client 'board task done aa-1' as actor "claude@laptop"
    And I run the client 'board task close aa-1 --resolution superseded' as actor "claude@laptop"
    Then the exit code is 0
    And the latest task_closed event of "aa-1" has data.resolution "superseded"
    And the latest task_closed event of "aa-1" has data.from "done"

  Scenario: done on a done task exits 6 and names task show
    Given I run the client 'board task done aa-1' as actor "claude@laptop"
    When I run the client 'board task done aa-1' as actor "claude@laptop"
    Then the exit code is 6
    And stdout is empty
    And stderr is exactly one line containing "it is done"
    And stderr is exactly one line containing "→ board task show aa-1"
    And exactly 1 task_done event exists for the task "aa-1"

  Scenario: the other refused moves exit 6 too
    When I run the client 'board task reopen aa-1' as actor "claude@laptop"
    Then the exit code is 6
    And stderr is exactly one line containing "→ board task show aa-1"
    When I run the client 'board task close aa-1' as actor "claude@laptop"
    And I run the client 'board task close aa-1' as actor "claude@laptop"
    Then the exit code is 6
    And stdout is empty
    And stderr is exactly one line containing "→ board task show aa-1"
    When I run the client 'board task done aa-1' as actor "claude@laptop"
    Then the exit code is 6
    And stderr is exactly one line containing "→ board task show aa-1"
    When I run the client 'board init close aa --force' as actor "claude@laptop"
    And I run the client 'board task reopen aa-1' as actor "claude@laptop"
    Then the exit code is 6
    And stderr is exactly one line containing "→ board task show aa-1"

  Scenario: an unknown task exits 4 and names task ls
    When I run the client 'board task done aa-9' as actor "claude@laptop"
    Then the exit code is 4
    And stdout is empty
    And stderr is exactly one line containing "→ board task ls"
    When I run the client 'board task reopen aa-9' as actor "claude@laptop"
    Then the exit code is 4
    When I run the client 'board task close aa-9' as actor "claude@laptop"
    Then the exit code is 4
    And stderr is exactly one line containing "→ board task ls"

  Scenario: --json prints the task after each move
    When I run the client 'board task done aa-1 --json' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is one JSON object whose "status" is "done"
    And stdout is one JSON object whose "ref" is "aa-1"
    When I run the client 'board task close aa-1 --resolution dropped --json' as actor "claude@laptop"
    Then stdout is one JSON object whose "status" is "closed"
    And stdout is one JSON object whose "resolution" is "dropped"
    When I run the client 'board task reopen aa-1 --json' as actor "claude@laptop"
    Then stdout is one JSON object whose "status" is "open"

  Scenario: close --for operator stores the operator on the task_closed event
    When I run the client 'board task close aa-1 --for operator' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is exactly "ok task aa-1 closed"
    And the latest task_closed event of "aa-1" is on behalf of "operator"
    When I run the client 'board task close aa-2' as actor "claude@laptop"
    Then the latest task_closed event of "aa-2" is on behalf of nobody

  Scenario: a malformed command line exits 2 before any request
    When I run the client 'board task done' as actor "claude@laptop"
    Then the exit code is 2
    And stdout is empty
    And stderr is exactly one line containing "usage: board task"
    When I run the client 'board task done aa-1 aa-2 aa-3' as actor "claude@laptop"
    Then the exit code is 2
    When I run the client 'board task reopen aa-1 --resolution dropped' as actor "claude@laptop"
    Then the exit code is 2
    When I run the client 'board task done aa-1 --for operator' as actor "claude@laptop"
    Then the exit code is 2
    When I run the client 'board task close aa-1 --resolution' as actor "claude@laptop"
    Then the exit code is 2
    When I run the client 'board task close aa-1 --resolution finished' as actor "claude@laptop"
    Then the exit code is 2
    And stderr is exactly one line containing "board: --resolution: invalid resolution"
    When I run the client 'board task close aa-1 --for someone' as actor "claude@laptop"
    Then the exit code is 2
    And stderr is exactly one line containing "board: --for: invalid actor"
    When I run the client 'board task show aa-1' as actor "claude@laptop"
    Then stdout line 1 matches "^aa-1  open  Task 1$"

  Scenario: done with an outcome ends the caller's own lease with it
    Given I run the client 'board lease start aa-1 "build it"' as actor "claude@laptop"
    When I run the client 'board task done aa-1 "shipped the thing"' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is exactly "ok task aa-1 done"
    And the lease "L1" is "ended" in the database
    When I run the client 'board lease ls --state ended --task aa-1 --json' as actor "claude@laptop"
    Then stdout JSON path "0.outcome" is "shipped the thing"

  Scenario: done while another actor holds a lease exits 6 naming the holder and --force ends that lease
    Given I run the client 'board lease start aa-1 "refactor the parser"' as actor "codex@laptop"
    When I run the client 'board task done aa-1 "shipped"' as actor "claude@laptop"
    Then the exit code is 6
    And stdout is empty
    And stderr is exactly one line containing "codex@laptop"
    And stderr is exactly one line containing "L1"
    And stderr is exactly one line containing "→ wait for L1 to end, or --force"
    And the lease "L1" is "live" in the database
    When I run the client 'board task show aa-1' as actor "claude@laptop"
    Then stdout line 1 matches "^aa-1  open  Task 1  \(active\)$"
    When I run the client 'board task done aa-1 "shipped" --force' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is exactly "ok task aa-1 done"
    And the lease "L1" is "ended" in the database
    When I run the client 'board lease ls --state ended --task aa-1 --json' as actor "claude@laptop"
    Then stdout JSON path "0.ended_by" is "claude@laptop"
    When I run the client 'board task show aa-1' as actor "claude@laptop"
    Then stdout line 1 matches "^aa-1  done  Task 1  !done$"

  Scenario: starting a lease on a done task exits 6 and says to reopen it first
    Given I run the client 'board task done aa-1' as actor "claude@laptop"
    When I run the client 'board lease start aa-1 "x"' as actor "claude@laptop"
    Then the exit code is 6
    And stdout is empty
    And stderr is exactly one line containing "it is done"
    And stderr is exactly one line containing "→ board task reopen aa-1 first"
    And there are 0 leases in the database
