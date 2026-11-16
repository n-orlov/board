Feature: Attention first in vh task ls and vh task show
  vh task ls lists the tasks that need attention first (stranded, then asking with the blocking ones first,
  then done, the most recently changed first within a group), then the rest in id order, and appends a marker for
  each reason: !stranded, !asking or !asking(blocking), and !done. --attention keeps only those tasks and --json
  carries attention and stranded_lease. vh task show prints an attention: line when the task needs attention
  and, for a stranded task, a
  stranded: line that names the lease and the command that acknowledges it.

  Background:
    Given a new database for the board commands
    When I run "vh admin migrate"
    And I run "vh admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port
    And I run the client 'vh init add aa "First one"' as actor "claude@laptop"
    And 8 tasks exist in the initiative "aa" as actor "claude@laptop"
    And I run the client 'vh task done aa-2' as actor "claude@laptop"
    And I run the client 'vh ask aa-3 "Which port?" --stop' as actor "claude@laptop"
    And I run the client 'vh ask aa-4 "Which port?" --blocking --stop' as actor "claude@laptop"
    And I run the client 'vh lease start aa-5 "five"' as actor "claude@laptop"
    And I run the client 'vh lease start aa-7 "seven"' as actor "claude@laptop"
    And I run the client 'vh lease start aa-8 "eight"' as actor "codex@laptop"
    And the lease "L1" ran out 120 seconds ago
    And the lease "L2" ran out 7200 seconds ago
    And the lease "L3" ran out 400000 seconds ago
    And I run the client 'vh task done aa-8' as actor "claude@laptop"
    And I run the client 'vh task edit aa-7 --title "seven edited"' as actor "claude@laptop"
    And I run the client 'vh task edit aa-5 --title "five edited"' as actor "claude@laptop"

  Scenario: the tasks that need attention come first, in the order of the reasons, with their markers
    When I run the client 'vh task ls --all' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is:
      """
      aa-5  open  five edited  !stranded
      aa-7  open  seven edited  !stranded
      aa-8  done  Task 8  !stranded !done
      aa-4  open  Task 4  (blocked)  !asking(blocking)
      aa-3  open  Task 3  !asking
      aa-2  done  Task 2  !done
      aa-1  open  Task 1
      aa-6  open  Task 6
      """

  Scenario: --attention keeps only the tasks that need attention and counts the rest out of the cap
    When I run the client 'vh task ls --attention --all' as actor "claude@laptop"
    Then stdout has 6 lines
    And stdout has the line "aa-5  open  five edited  !stranded"
    When I run the client 'vh task ls --attention --limit 2' as actor "claude@laptop"
    Then stdout is:
      """
      aa-5  open  five edited  !stranded
      aa-7  open  seven edited  !stranded
      +4 more → vh task ls --attention --all
      """
    When I run the client 'vh task ls --attention --kind watch' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is empty

  Scenario: --json carries the attention list and the stranded lease in the same order
    When I run the client 'vh task ls --json --all' as actor "claude@laptop"
    Then stdout is a JSON array whose refs are "aa-5,aa-7,aa-8,aa-4,aa-3,aa-2,aa-1,aa-6"
    And stdout JSON path "0.attention.0.reason" is "stranded"
    And stdout JSON path "0.stranded_lease.ref" is "L1"
    And stdout JSON path "0.stranded_lease.holder.handle" is "claude@laptop"
    And stdout JSON path "0.stranded_lease.intent" is "five"
    And stdout JSON path "2.attention.1.reason" is "done"
    And stdout JSON path "3.attention.0.blocking" is "true"
    And stdout JSON path "4.attention.0.reason" is "asking"
    And stdout JSON path "6.attention" has 0 items
    And stdout JSON path "6.stranded_lease" is "<nil>"
    When I run the client 'vh task ls --attention --json --all' as actor "claude@laptop"
    Then stdout is a JSON array whose refs are "aa-5,aa-7,aa-8,aa-4,aa-3,aa-2"

  Scenario: task show prints the attention line and, for a stranded task, the lease to acknowledge
    When I run the client 'vh task show aa-5' as actor "claude@laptop"
    Then the exit code is 0
    And stdout has the line "aa-5  open  five edited  !stranded"
    And stdout has the line "attention: !stranded"
    And stdout has the line 'stranded: L1 by claude@laptop: "five" expired 2m ago → vh lease ack L1'
    When I run the client 'vh task show aa-7' as actor "claude@laptop"
    Then stdout has the line 'stranded: L2 by claude@laptop: "seven" expired 2h ago → vh lease ack L2'
    When I run the client 'vh task show aa-8' as actor "claude@laptop"
    Then stdout has the line "attention: !stranded !done"
    And stdout has the line 'stranded: L3 by codex@laptop: "eight" expired 4d ago → vh lease ack L3'
    When I run the client 'vh task show aa-4' as actor "claude@laptop"
    Then stdout has the line "attention: !asking(blocking)"
    When I run the client 'vh task show aa-3' as actor "claude@laptop"
    Then stdout has the line "attention: !asking"
    When I run the client 'vh task show aa-2' as actor "claude@laptop"
    Then stdout has the line "attention: !done"
    When I run the client 'vh task show aa-1' as actor "claude@laptop"
    Then stdout has no line starting with "attention:"
    And stdout has no line starting with "stranded:"
    When I run the client 'vh task show aa-5 --json' as actor "claude@laptop"
    Then stdout JSON path "stranded_lease.ref" is "L1"
    And stdout JSON path "attention.0.reason" is "stranded"

  Scenario: init ls marks an initiative with its attention count and leaves a quiet one unmarked
    When I run the client 'vh init add bb "Quiet one" --tag calm' as actor "claude@laptop"
    And I run the client 'vh init ls' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is:
      """
      inbox  open  Inbox
      aa  open  First one !6
      bb  open  Quiet one  [calm]
      """
    When I run the client 'vh init ls --json' as actor "claude@laptop"
    Then stdout JSON path "0.attention_count" is "0"
    And stdout JSON path "1.attention_count" is "6"
    And stdout JSON path "2.attention_count" is "0"
