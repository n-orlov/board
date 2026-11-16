Feature: Finishing work from the command line, vh finish
  vh finish [<task>] "<outcome>" [--done] ends the caller's live lease, writes the outcome as a note on the task
  and, with --done, marks the task done, with one request. It prints the ok line of vh lease end, and with --done
  then the ok line of vh task done. It exits 0, 6 when the finish is a conflict (no live lease, several leases
  with no task named, or --done under another actor's lease), 4 for an unknown task and 2 for a malformed
  command line. A refusal writes nothing.

  Background:
    Given a new database for the board commands
    When I run "vh admin migrate"
    And I run "vh admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port
    And I run the client 'vh init add aa "First one"' as actor "claude@laptop"
    And 3 tasks exist in the initiative "aa" as actor "claude@laptop"

  Scenario: a finish on a given task prints the lease end line only
    Given I run the client 'vh start aa-1 "fixing it"' as actor "claude@laptop"
    And I note the counts of events, notes and live leases
    When I run the client 'vh finish aa-1 "fixed and tested"' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is exactly 'ok lease L1 ended'
    And stderr is empty
    And the counts of events, notes and live leases changed by 2, 1 and -1
    And exactly 1 lease_ended event exists for the lease "L1"
    And exactly 1 note_posted event exists for the task "aa-1"
    And the task "aa-1" is open

  Scenario: --done prints the lease end line and then the task done line
    Given I run the client 'vh start aa-1 "fixing it"' as actor "claude@laptop"
    And I note the counts of events, notes and live leases
    When I run the client 'vh finish aa-1 "all of it" --done' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is:
      """
      ok lease L1 ended
      ok task aa-1 done
      """
    And stderr is empty
    And the counts of events, notes and live leases changed by 3, 1 and -1
    And exactly 1 task_done event exists for the task "aa-1"
    And the task "aa-1" is done

  Scenario: the task is left out and the only live lease names it
    Given I run the client 'vh start aa-2 "fixing it"' as actor "claude@laptop"
    When I run the client 'vh finish "left it tidy" --done' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is:
      """
      ok lease L1 ended
      ok task aa-2 done
      """
    And the task "aa-2" is done

  Scenario: --json prints the response with the ended lease, the note and done
    Given I run the client 'vh start aa-1 "fixing it"' as actor "claude@laptop"
    When I run the client 'vh finish aa-1 "shipped" --json' as actor "claude@laptop"
    Then the exit code is 0
    And stdout JSON path "lease.ref" is "L1"
    And stdout JSON path "lease.outcome" is "shipped"
    And stdout JSON path "note.summary" is "shipped"
    And stdout JSON path "done" is "false"

  Scenario: no live lease is a conflict, exit 6, and writes nothing
    Given I run the client 'vh start aa-1 "theirs"' as actor "claude@laptop"
    And I note the counts of events, notes and live leases
    When I run the client 'vh finish "x"' as actor "codex@laptop"
    Then the exit code is 6
    And stdout is empty
    And stderr is one line ending with '→ Start one first with vh start <task> "<intent>", or name the task you meant.'
    When I run the client 'vh finish aa-1 "x"' as actor "codex@laptop"
    Then the exit code is 6
    And stdout is empty
    And stderr is one line ending with '→ Start one first with vh start aa-1 "<intent>", or name a task you hold a lease on.'
    And the counts of events, notes and live leases changed by 0, 0 and 0

  Scenario: several live leases and no task is a conflict that lists them
    Given I run the client 'vh start aa-1 "one"' as actor "claude@laptop"
    And I run the client 'vh start aa-2 "two"' as actor "claude@laptop"
    And I note the counts of events, notes and live leases
    When I run the client 'vh finish "x"' as actor "claude@laptop"
    Then the exit code is 6
    And stdout is empty
    And stderr is one line ending with "→ Name the task to finish: aa-1 (L1), aa-2 (L2)."
    And the counts of events, notes and live leases changed by 0, 0 and 0

  Scenario: --done under another actor's live lease is a conflict that changes nothing
    Given I run the client 'vh start aa-1 "mine"' as actor "claude@laptop"
    And I run the client 'vh start aa-1 "theirs"' as actor "codex@laptop"
    And I note the counts of events, notes and live leases
    When I run the client 'vh finish aa-1 "x" --done' as actor "claude@laptop"
    Then the exit code is 6
    And stdout is empty
    And stderr is one line ending with "→ wait for the other lease to end, or vh task done aa-1 --force"
    And the counts of events, notes and live leases changed by 0, 0 and 0
    And the lease "L1" is "live" in the database
    And the task "aa-1" is open

  Scenario: an unknown task exits 4 and writes nothing
    Given I note the counts of events, notes and live leases
    When I run the client 'vh finish zz-1 "x"' as actor "claude@laptop"
    Then the exit code is 4
    And stdout is empty
    And stderr is one line ending with "→ vh task ls"
    And the counts of events, notes and live leases changed by 0, 0 and 0

  Scenario Outline: a malformed command line exits 2 with one usage line and sends nothing
    Given I note the counts of events, notes and live leases
    When I run the client '<command>' as actor "claude@laptop"
    Then the exit code is 2
    And stdout is empty
    And stderr is one line ending with '<line>'
    And the counts of events, notes and live leases changed by 0, 0 and 0

    Examples:
      | command                      | line                                                       |
      | vh finish                    | usage: vh finish [<task>] "<outcome>" [--done] [--json]    |
      | vh finish --done             | usage: vh finish [<task>] "<outcome>" [--done] [--json]    |
      | vh finish aa-1 x extra       | usage: vh finish [<task>] "<outcome>" [--done] [--json]    |
      | vh finish x --no-such        | usage: vh finish [<task>] "<outcome>" [--done] [--json]    |
