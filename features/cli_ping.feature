Feature: Renewing every lease from the command line, vh ping
  vh ping ["<progress>"] renews every live lease of the caller with one request. It prints the ok line of
  vh lease renew for each renewed lease and, with progress, the ok line of vh milestone for each leased task.
  It exits 0, 6 when the caller holds no live lease and 2 for a malformed command line. A refusal writes nothing.

  Background:
    Given a new database for the board commands
    When I run "vh admin migrate"
    And I run "vh admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port
    And I run the client 'vh init add aa "First one"' as actor "claude@laptop"
    And 3 tasks exist in the initiative "aa" as actor "claude@laptop"

  Scenario: one live lease prints one lease renew line
    Given I run the client 'vh start aa-1 "working"' as actor "claude@laptop"
    And I note the counts of events, notes and live leases
    When I run the client 'vh ping' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is exactly 'ok lease L1 aa-1 exp 2h'
    And stderr is empty
    And the counts of events, notes and live leases changed by 1, 0 and 0
    And exactly 1 lease_renewed event exists for the lease "L1"
    And exactly 0 milestone events exist for the task "aa-1"

  Scenario: one live lease with progress prints the lease line and then the milestone line
    Given I run the client 'vh start aa-1 "working"' as actor "claude@laptop"
    And I note the counts of events, notes and live leases
    When I run the client 'vh ping "tests green"' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is:
      """
      ok lease L1 aa-1 exp 2h
      ok task aa-1 milestone recorded
      """
    And stderr is empty
    And the counts of events, notes and live leases changed by 2, 0 and 0
    And exactly 1 lease_renewed event exists for the lease "L1"
    And exactly 1 milestone event exists for the task "aa-1"

  Scenario: three live leases print three lease lines
    Given I run the client 'vh start aa-1 "one"' as actor "claude@laptop"
    And I run the client 'vh start aa-2 "two"' as actor "claude@laptop"
    And I run the client 'vh start aa-3 "three"' as actor "claude@laptop"
    And I run the client 'vh start aa-1 "theirs"' as actor "codex@laptop"
    And I note the counts of events, notes and live leases
    When I run the client 'vh ping' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is:
      """
      ok lease L1 aa-1 exp 2h
      ok lease L2 aa-2 exp 2h
      ok lease L3 aa-3 exp 2h
      """
    And the counts of events, notes and live leases changed by 3, 0 and 0
    And exactly 1 lease_renewed event exists for the lease "L2"
    And exactly 0 lease_renewed events exist for the lease "L4"

  Scenario: three live leases with progress print three lease lines and three milestone lines
    Given I run the client 'vh start aa-1 "one"' as actor "claude@laptop"
    And I run the client 'vh start aa-2 "two"' as actor "claude@laptop"
    And I run the client 'vh start aa-3 "three"' as actor "claude@laptop"
    And I note the counts of events, notes and live leases
    When I run the client 'vh ping "half way"' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is:
      """
      ok lease L1 aa-1 exp 2h
      ok lease L2 aa-2 exp 2h
      ok lease L3 aa-3 exp 2h
      ok task aa-1 milestone recorded
      ok task aa-2 milestone recorded
      ok task aa-3 milestone recorded
      """
    And the counts of events, notes and live leases changed by 6, 0 and 0
    And exactly 1 milestone event exists for the task "aa-1"
    And exactly 1 milestone event exists for the task "aa-2"
    And exactly 1 milestone event exists for the task "aa-3"

  Scenario: --json prints the response with the renewed leases and the milestone tasks
    Given I run the client 'vh start aa-1 "working"' as actor "claude@laptop"
    When I run the client 'vh ping "tests green" --json' as actor "claude@laptop"
    Then the exit code is 0
    And stdout JSON path "leases.0.ref" is "L1"
    And stdout JSON path "milestones.0" is "aa-1"

  Scenario: no live lease is a conflict, exit 6, and writes nothing
    Given I run the client 'vh start aa-1 "theirs"' as actor "claude@laptop"
    And I note the counts of events, notes and live leases
    When I run the client 'vh ping' as actor "codex@laptop"
    Then the exit code is 6
    And stdout is empty
    And stderr is one line ending with '→ Start one first with vh start <task> "<intent>".'
    When I run the client 'vh ping "half way"' as actor "codex@laptop"
    Then the exit code is 6
    And stdout is empty
    And the counts of events, notes and live leases changed by 0, 0 and 0

  Scenario Outline: a malformed command line exits 2 with one usage line and sends nothing
    Given I run the client 'vh start aa-1 "working"' as actor "claude@laptop"
    And I note the counts of events, notes and live leases
    When I run the client '<command>' as actor "claude@laptop"
    Then the exit code is 2
    And stdout is empty
    And stderr is one line ending with '<line>'
    And the counts of events, notes and live leases changed by 0, 0 and 0

    Examples:
      | command                | line                                      |
      | vh ping a b            | usage: vh ping ["<progress>"] [--json]    |
      | vh ping x --no-such    | usage: vh ping ["<progress>"] [--json]    |
