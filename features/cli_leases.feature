Feature: Leases from the command line
  vh lease start, renew, end and ls start, keep, end and list leases. A start
  or a renewal prints an ok line and one CONFLICT line for each live lease of
  another that holds a resource of the lease, and exits 7 when it printed any.

  Background:
    Given a new database for the board commands
    When I run "vh admin migrate"
    And I run "vh admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port
    And I run the client 'vh init add aa "First one"' as actor "claude@laptop"
    And 3 tasks exist in the initiative "aa" as actor "claude@laptop"

  Scenario: lease start prints one ok line with the ttl
    When I run the client 'vh lease start aa-1 "fixing it" --ttl 4h --res repo:x --res host:y' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is exactly "ok lease L1 aa-1 exp 4h"
    And stderr is empty
    And exactly 1 lease_started event exists for the lease "L1"

  Scenario: lease start without a ttl takes the default of two hours and a ttl of 90m is spelled out
    When I run the client 'vh lease start aa-1 "fixing it"' as actor "claude@laptop"
    Then stdout is exactly "ok lease L1 aa-1 exp 2h"
    When I run the client 'vh lease start aa-2 "other" --ttl 90m' as actor "claude@laptop"
    Then stdout is exactly "ok lease L2 aa-2 exp 1h30m"

  Scenario: a second start by the same actor on the same task renews the lease
    Given I run the client 'vh lease start aa-1 "first" --ttl 1h' as actor "claude@laptop"
    When I run the client 'vh lease start aa-1 "second" --ttl 3h' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is exactly "ok lease L1 aa-1 exp 3h"
    And there are 1 leases in the database
    And exactly 1 lease_renewed event exists for the lease "L1"

  Scenario: lease start --json prints the lease and its conflicts
    When I run the client 'vh lease start aa-1 "fixing it" --res repo:x --json' as actor "claude@laptop"
    Then the exit code is 0
    And stdout JSON path "lease.ref" is "L1"
    And stdout JSON path "lease.task" is "aa-1"
    And stdout JSON path "lease.resources.0" is "repo:x"
    And stdout JSON path "conflicts" has 0 items

  Scenario: lease start refuses a bad ttl before any request and a closed task with exit 6
    When I run the client 'vh lease start aa-1 "x" --ttl 2' as actor "claude@laptop"
    Then the exit code is 2
    And stderr is one line ending with 'invalid duration "2": want a whole number and a unit s, m, h or d, such as 90m'
    Given I run the client 'vh task close aa-3' as actor "claude@laptop"
    When I run the client 'vh lease start aa-3 "x"' as actor "claude@laptop"
    Then the exit code is 6
    And stderr is one line ending with "→ vh task show aa-3"
    When I run the client 'vh lease start zz-1 "x"' as actor "claude@laptop"
    Then the exit code is 4
    And stderr is one line ending with "→ vh task ls"
    And there are 0 leases in the database

  Scenario: lease renew by lease ref and by task ref
    Given I run the client 'vh lease start aa-1 "one" --ttl 1h' as actor "claude@laptop"
    When I run the client 'vh lease renew L1 --ttl 5h' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is exactly "ok lease L1 aa-1 exp 5h"
    When I run the client 'vh lease renew aa-1 --intent "still one"' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is exactly "ok lease L1 aa-1 exp 5h"
    And exactly 2 lease_renewed events exist for the lease "L1"
    When I run the client 'vh lease ls' as actor "claude@laptop"
    Then stdout line 1 matches '^L1  live  aa-1  claude@laptop  "still one"  exp (5h|4h59m)$'

  Scenario: a renewal with no argument renews both of my live leases and leaves the others alone
    Given I run the client 'vh lease start aa-1 "one" --ttl 1h' as actor "claude@laptop"
    And I run the client 'vh lease start aa-2 "two" --ttl 1h' as actor "claude@laptop"
    And I run the client 'vh lease start aa-3 "three" --ttl 1h' as actor "other@laptop"
    When I run the client 'vh lease renew --ttl 3h' as actor "claude@laptop"
    Then the exit code is 0
    And stdout has 2 lines
    And stdout line 1 matches "^ok lease L1 aa-1 exp 3h$"
    And stdout line 2 matches "^ok lease L2 aa-2 exp 3h$"
    And exactly 1 lease_renewed event exists for the lease "L1"
    And exactly 1 lease_renewed event exists for the lease "L2"
    And exactly 0 lease_renewed events exist for the lease "L3"
    When I run the client 'vh lease renew --json' as actor "claude@laptop"
    Then stdout JSON path "" has 2 items
    And stdout JSON path "0.lease.ref" is "L1"
    And stdout JSON path "1.lease.ref" is "L2"

  Scenario: a renewal with no argument and no live lease exits 4
    When I run the client 'vh lease renew' as actor "claude@laptop"
    Then the exit code is 4
    And stderr is exactly "you hold no live lease → vh lease ls"

  Scenario: renewing an expired lease exits 6 with the start that replaces it
    Given I run the client 'vh lease start aa-1 "one"' as actor "claude@laptop"
    And the lease "L1" ran out 120 seconds ago
    When I run the client 'vh lease renew L1' as actor "claude@laptop"
    Then the exit code is 6
    And stdout is empty
    And stderr is exactly 'lease L1 expired 2m ago → vh lease start aa-1 "<intent>"'
    When I run the client 'vh lease renew aa-1' as actor "claude@laptop"
    Then the exit code is 6
    And stderr is exactly 'lease L1 expired 2m ago → vh lease start aa-1 "<intent>"'

  Scenario: renewing an ended lease exits 6 and an unknown lease exits 4
    Given I run the client 'vh lease start aa-1 "one"' as actor "claude@laptop"
    And I run the client 'vh lease end L1' as actor "claude@laptop"
    When I run the client 'vh lease renew L1' as actor "claude@laptop"
    Then the exit code is 6
    And stderr is exactly 'cannot renew lease L1: it is ended → vh lease start aa-1 "<intent>"'
    When I run the client 'vh lease renew L9' as actor "claude@laptop"
    Then the exit code is 4
    And stderr is one line ending with "→ vh lease ls"
    When I run the client 'vh lease renew zz-1' as actor "claude@laptop"
    Then the exit code is 4
    And stderr is one line ending with "→ vh task ls"

  Scenario: lease end by lease ref, by task ref and with no argument
    Given I run the client 'vh lease start aa-1 "one"' as actor "claude@laptop"
    And I run the client 'vh lease start aa-2 "two"' as actor "claude@laptop"
    When I run the client 'vh lease end L1' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is exactly "ok lease L1 ended"
    And exactly 1 lease_ended event exists for the lease "L1"
    When I run the client 'vh lease end aa-2 "all done"' as actor "claude@laptop"
    Then stdout is exactly "ok lease L2 ended"
    And the lease "L2" is "ended" in the database
    Given I run the client 'vh lease start aa-3 "three"' as actor "claude@laptop"
    When I run the client 'vh lease end "shipped it"' as actor "claude@laptop"
    Then stdout is exactly "ok lease L3 ended"
    When I run the client 'vh lease ls --state ended --task aa-3 --json' as actor "claude@laptop"
    Then stdout JSON path "0.outcome" is "shipped it"

  Scenario: lease end --json, ending twice, and ending with several live leases and no argument
    Given I run the client 'vh lease start aa-1 "one"' as actor "claude@laptop"
    And I run the client 'vh lease start aa-2 "two"' as actor "claude@laptop"
    When I run the client 'vh lease end' as actor "claude@laptop"
    Then the exit code is 2
    And stderr is exactly "you hold 2 live leases: L1, L2 → vh lease end <L…>"
    When I run the client 'vh lease end L1 --json' as actor "claude@laptop"
    Then stdout JSON path "state" is "ended"
    When I run the client 'vh lease end L1' as actor "claude@laptop"
    Then the exit code is 6
    And stderr is one line ending with "→ vh lease ls --mine --state all"
    When I run the client 'vh lease end L9' as actor "claude@laptop"
    Then the exit code is 4

  Scenario: lease ls lists live leases by default and filters by state, owner, task and resource
    Given I run the client 'vh lease start aa-1 "one" --ttl 1h --res host:gw1' as actor "claude@laptop"
    And I run the client 'vh lease start aa-2 "two" --ttl 1h --res repo:x --res host:gw2' as actor "other@laptop"
    And I run the client 'vh lease start aa-3 "three" --ttl 1h' as actor "other@laptop"
    And I run the client 'vh lease end L3' as actor "other@laptop"
    When I run the client 'vh lease ls' as actor "claude@laptop"
    Then stdout has 2 lines
    And stdout line 1 matches '^L1  live  aa-1  claude@laptop  "one"  res host:gw1  exp (1h|59m\d*s?)$'
    And stdout line 2 matches '^L2  live  aa-2  other@laptop  "two"  res host:gw2,repo:x  exp (1h|59m)$'
    When I run the client 'vh lease ls --mine' as actor "claude@laptop"
    Then stdout has 1 line
    And stdout line 1 matches "^L1  live  aa-1  "
    When I run the client 'vh lease ls --state ended' as actor "claude@laptop"
    Then stdout has 1 line
    And stdout line 1 matches '^L3  ended  aa-3  other@laptop  "three"  ended just now$'
    When I run the client 'vh lease ls --state all --task AA-2' as actor "claude@laptop"
    Then stdout has 1 line
    And stdout line 1 matches "^L2  live  aa-2  "
    When I run the client 'vh lease ls --res repo:x' as actor "claude@laptop"
    Then stdout has 1 line
    And stdout line 1 matches "^L2  live  aa-2  "
    When I run the client 'vh lease ls --state all --json' as actor "claude@laptop"
    Then stdout JSON path "" has 3 items
    When I run the client 'vh lease ls --state weird' as actor "claude@laptop"
    Then the exit code is 2

  Scenario: lease ls shows an expired lease as such and prints nothing when there is none
    Given I run the client 'vh lease start aa-1 "one"' as actor "claude@laptop"
    And the lease "L1" ran out 120 seconds ago
    When I run the client 'vh lease ls' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is empty
    When I run the client 'vh lease ls --state expired' as actor "claude@laptop"
    Then stdout is exactly 'L1  expired  aa-1  claude@laptop  "one"  expired 2m ago'
    When I run the client 'vh lease ls --task zz-1' as actor "claude@laptop"
    Then the exit code is 4

  Scenario: lease start prints one CONFLICT line for each conflict and exits 7
    Given I run the client 'vh lease start aa-1 "rotate prefix" --ttl 1h --res host:gw1 --res repo:x' as actor "other@laptop"
    And I run the client 'vh lease start aa-3 "attachments" --ttl 1h --res repo:x' as actor "third@laptop"
    When I run the client 'vh lease start aa-2 "my work" --ttl 2h --res host:gw1 --res repo:x --res repo:y' as actor "claude@laptop"
    Then the exit code is 7
    And stderr is empty
    And stdout has 3 lines
    And stdout line 1 matches "^ok lease L3 aa-2 exp 2h$"
    And stdout line 2 matches '^CONFLICT host:gw1,repo:x: L1 other@laptop \(aa-1\) "rotate prefix" exp (1h|59m)$'
    And stdout line 3 matches '^CONFLICT repo:x: L2 third@laptop \(aa-3\) "attachments" exp (1h|59m)$'
    And there are 3 leases in the database

  Scenario: lease start exits 0 when no live lease of another holds a resource
    Given I run the client 'vh lease start aa-1 "one" --res host:gw1' as actor "other@laptop"
    When I run the client 'vh lease start aa-2 "two" --res host:gw2' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is exactly "ok lease L2 aa-2 exp 2h"
    Given I run the client 'vh lease end L1' as actor "other@laptop"
    When I run the client 'vh lease start aa-3 "three" --res host:gw1' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is exactly "ok lease L3 aa-3 exp 2h"

  Scenario: two leases of one actor that share a resource conflict
    Given I run the client 'vh lease start aa-1 "one" --res repo:x' as actor "claude@laptop"
    When I run the client 'vh lease start aa-2 "two" --res repo:x' as actor "claude@laptop"
    Then the exit code is 7
    And stdout line 2 matches '^CONFLICT repo:x: L1 claude@laptop \(aa-1\) "one" exp '

  Scenario: lease renew prints the conflicts and exits 7, and exits 0 once the other lease is gone
    Given I run the client 'vh lease start aa-1 "one" --ttl 1h --res host:gw1' as actor "other@laptop"
    And I run the client 'vh lease start aa-2 "two" --ttl 1h --res host:gw1' as actor "claude@laptop"
    When I run the client 'vh lease renew L2 --ttl 3h' as actor "claude@laptop"
    Then the exit code is 7
    And stdout has 2 lines
    And stdout line 1 matches "^ok lease L2 aa-2 exp 3h$"
    And stdout line 2 matches '^CONFLICT host:gw1: L1 other@laptop \(aa-1\) "one" exp (1h|59m)$'
    When I run the client 'vh lease renew' as actor "claude@laptop"
    Then the exit code is 7
    And stdout has 2 lines
    When I run the client 'vh lease renew L1' as actor "other@laptop"
    Then the exit code is 7
    And stdout line 2 matches '^CONFLICT host:gw1: L2 claude@laptop \(aa-2\) "two" exp '
    Given I run the client 'vh lease end L1' as actor "other@laptop"
    When I run the client 'vh lease renew L2' as actor "claude@laptop"
    Then the exit code is 0
    And stdout has 1 line

  Scenario: --json keeps the conflicts and the exit 7
    Given I run the client 'vh lease start aa-1 "one" --res host:gw1' as actor "other@laptop"
    When I run the client 'vh lease start aa-2 "two" --res host:gw1 --json' as actor "claude@laptop"
    Then the exit code is 7
    And stdout JSON path "conflicts" has 1 items
    And stdout JSON path "conflicts.0.lease" is "L1"
    And stdout JSON path "conflicts.0.holder" is "other@laptop"
    When I run the client 'vh lease renew L2 --json' as actor "claude@laptop"
    Then the exit code is 7
    And stdout JSON path "lease.ref" is "L2"
    And stdout JSON path "conflicts" has 1 items

  Scenario: lease ack acknowledges an expired lease and prints one ok line
    Given I run the client 'vh lease start aa-1 "one"' as actor "claude@laptop"
    And the lease "L1" ran out 120 seconds ago
    When I run the client 'vh lease ack L1' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is exactly "ok lease L1 acknowledged"
    And stderr is empty
    When I run the client 'vh lease ack l1 --json' as actor "claude@laptop"
    Then the exit code is 0
    And stdout JSON path "ref" is "L1"

  Scenario: lease ack of a lease that has not expired exits 6 and an unknown lease exits 4
    Given I run the client 'vh lease start aa-1 "one"' as actor "claude@laptop"
    When I run the client 'vh lease ack L1' as actor "claude@laptop"
    Then the exit code is 6
    And stdout is empty
    And stderr is one line ending with "→ vh lease ls --mine --state all"
    When I run the client 'vh lease end L1' as actor "claude@laptop"
    And I run the client 'vh lease ack L1' as actor "claude@laptop"
    Then the exit code is 6
    When I run the client 'vh lease ack L9' as actor "claude@laptop"
    Then the exit code is 4
    And stdout is empty
    And stderr is one line ending with "→ vh lease ls"

  Scenario: wrong arguments exit 2 with the usage line
    When I run the client 'vh lease' as actor "claude@laptop"
    Then the exit code is 2
    When I run the client 'vh lease start aa-1' as actor "claude@laptop"
    Then the exit code is 2
    When I run the client 'vh lease renew L1 L2' as actor "claude@laptop"
    Then the exit code is 2
    When I run the client 'vh lease ls extra' as actor "claude@laptop"
    Then the exit code is 2
    When I run the client 'vh lease nothing' as actor "claude@laptop"
    Then the exit code is 2
    When I run the client 'vh lease ack' as actor "claude@laptop"
    Then the exit code is 2
    When I run the client 'vh lease ack L1 L2' as actor "claude@laptop"
    Then the exit code is 2
    When I run the client 'vh lease ack aa-1' as actor "claude@laptop"
    Then the exit code is 2
