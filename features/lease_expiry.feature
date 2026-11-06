Feature: Lease expiry by the sweeper and on read
  board serve marks a live lease past its expiry as expired and writes one lease_expired event, in the
  background at BOARD_SWEEP_INTERVAL (default 15s; "off" turns the sweeper off). Every read that returns leases
  does the same first, so with the sweeper off GET /leases, GET /leases/{ref} and GET /tasks/{ref} still show a
  lease expired, and the event is written once however many reads follow. Expiry is made by backdating the
  row, never by waiting out a ttl.

  Background:
    Given a new database for the board commands
    When I run "board admin migrate"
    And I run "board admin keys create workstation"
    And I remember the key stdout printed

  Scenario: the sweeper expires a backdated lease within its interval and writes one event
    Given the board server is running on a free port with BOARD_SWEEP_INTERVAL=1s
    And I create the initiative "aa" titled "First" with no tags as actor "claude@laptop"
    And I create a task in "aa" titled "one" as actor "claude@laptop"
    And I start a lease with body "{'task':'aa-1','intent':'first'}" as actor "claude@laptop"
    And the lease "L1" ran out 30 seconds ago
    Then within 8 seconds the lease "L1" is "expired" in the database
    And exactly 1 lease_expired event exists for the lease "L1"
    And exactly 1 lease_expired event exists for the task "aa-1"
    When I request the lease "L1" as actor "claude@laptop"
    Then the response path "state" is "expired"
    And exactly 1 lease_expired event exists for the lease "L1"

  Scenario: a malformed interval stops the server with one error line
    When I run "board serve" with BOARD_SWEEP_INTERVAL=soon
    Then the exit code is 1
    And stderr is exactly one line containing "BOARD_SWEEP_INTERVAL"

  Scenario: with the sweeper off nothing expires until a read asks
    Given the board server is running on a free port with BOARD_SWEEP_INTERVAL=off
    And I create the initiative "aa" titled "First" with no tags as actor "claude@laptop"
    And I create a task in "aa" titled "one" as actor "claude@laptop"
    And I start a lease with body "{'task':'aa-1','intent':'first'}" as actor "claude@laptop"
    And the lease "L1" ran out 30 seconds ago
    Then the lease "L1" is "live" in the database
    And exactly 0 lease_expired events exist for the lease "L1"

  Scenario: the lease list expires a backdated lease
    Given the board server is running on a free port with BOARD_SWEEP_INTERVAL=off
    And I create the initiative "aa" titled "First" with no tags as actor "claude@laptop"
    And I create a task in "aa" titled "one" as actor "claude@laptop"
    And I start a lease with body "{'task':'aa-1','intent':'first'}" as actor "claude@laptop"
    And the lease "L1" ran out 30 seconds ago
    When I list the leases with "state=expired" as actor "claude@laptop"
    Then the response path "items" lists the refs "L1"
    And the lease "L1" is "expired" in the database
    And exactly 1 lease_expired event exists for the lease "L1"

  Scenario: the lease detail expires a backdated lease
    Given the board server is running on a free port with BOARD_SWEEP_INTERVAL=off
    And I create the initiative "aa" titled "First" with no tags as actor "claude@laptop"
    And I create a task in "aa" titled "one" as actor "claude@laptop"
    And I start a lease with body "{'task':'aa-1','intent':'first'}" as actor "claude@laptop"
    And the lease "L1" ran out 30 seconds ago
    When I request the lease "L1" as actor "claude@laptop"
    Then the response path "state" is "expired"
    And the lease "L1" is "expired" in the database
    And exactly 1 lease_expired event exists for the lease "L1"

  Scenario: the task detail expires a backdated lease and lists no live lease
    Given the board server is running on a free port with BOARD_SWEEP_INTERVAL=off
    And I create the initiative "aa" titled "First" with no tags as actor "claude@laptop"
    And I create a task in "aa" titled "one" as actor "claude@laptop"
    And I start a lease with body "{'task':'aa-1','intent':'first'}" as actor "claude@laptop"
    And the lease "L1" ran out 30 seconds ago
    When I request the task "aa-1" as actor "claude@laptop"
    Then the response path "live_leases" has 0 items
    And the lease "L1" is "expired" in the database
    And exactly 1 lease_expired event exists for the lease "L1"

  Scenario: three reads of a backdated lease write exactly one lease_expired event
    Given the board server is running on a free port with BOARD_SWEEP_INTERVAL=off
    And I create the initiative "aa" titled "First" with no tags as actor "claude@laptop"
    And I create a task in "aa" titled "one" as actor "claude@laptop"
    And I start a lease with body "{'task':'aa-1','intent':'first'}" as actor "claude@laptop"
    And the lease "L1" ran out 30 seconds ago
    When I list the leases with "state=all" as actor "claude@laptop"
    Then the response path "items.0.state" is "expired"
    When I request the lease "L1" as actor "claude@laptop"
    Then the response path "state" is "expired"
    When I request the task "aa-1" as actor "claude@laptop"
    Then the response path "live_leases" has 0 items
    And exactly 1 lease_expired event exists for the lease "L1"
    And exactly 1 lease_expired event exists for the task "aa-1"
