Feature: GET /api/v1/now, the lease and conflict parts of the check-in
  The check-in answers the caller's actor, its live leases with task ref, title and expiry, the live leases of
  other actors, and the conflicts: other live leases that hold a resource of one of the caller's live leases or
  one of the resources asked about in ?resources=. In this phase it carries no ask, note or attention part.
  Expiry is made by backdating the row with the sweeper off, never by waiting out a ttl.

  Background:
    Given a new database for the board commands
    When I run "board admin migrate"
    And I run "board admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port with BOARD_SWEEP_INTERVAL=off
    And I create the initiative "aa" titled "First" with no tags as actor "claude@laptop"
    And I create a task in "aa" titled "wire the thing" as actor "claude@laptop"
    And I create a task in "aa" titled "test the thing" as actor "claude@laptop"

  Scenario: with no leases every list is empty and only the four members are there
    When I check in with "" as actor "claude@laptop"
    Then the response status is 200
    And the response members are exactly "actor,my_leases,others_leases,conflicts"
    And the response path "actor" is "claude@laptop"
    And the response path "my_leases" has 0 items
    And the response path "others_leases" has 0 items
    And the response path "conflicts" has 0 items

  Scenario: my live lease is listed with its task ref, title and expiry
    Given I start a lease with body "{'task':'aa-1','intent':'wiring','resources':['repo:x']}" as actor "claude@laptop"
    When I check in with "" as actor "claude@laptop"
    Then the response status is 200
    And the response members are exactly "actor,my_leases,others_leases,conflicts"
    And the response path "my_leases" lists the refs "L1"
    And the response path "my_leases.0.task" is "aa-1"
    And the response path "my_leases.0.title" is "wire the thing"
    And the response path "my_leases.0.intent" is "wiring"
    And the response path "my_leases.0.expires_at" is the expiry stored for the lease "L1"
    And the response path "others_leases" has 0 items
    And the response path "conflicts" has 0 items

  Scenario: a lease of another actor is listed under others only
    Given I start a lease with body "{'task':'aa-2','intent':'testing','resources':['repo:y']}" as actor "codex@laptop"
    When I check in with "" as actor "claude@laptop"
    Then the response path "my_leases" has 0 items
    And the response path "others_leases" lists the refs "L1"
    And the response path "others_leases.0.actor" is "codex@laptop"
    And the response path "others_leases.0.task" is "aa-2"
    And the response path "conflicts" has 0 items
    When I check in with "" as actor "codex@laptop"
    Then the response path "my_leases" lists the refs "L1"
    And the response path "my_leases.0.title" is "test the thing"
    And the response path "others_leases" has 0 items

  Scenario: another actor holding a resource of my lease is a conflict
    Given I start a lease with body "{'task':'aa-1','intent':'wiring','resources':['repo:x','db:main']}" as actor "claude@laptop"
    And I start a lease with body "{'task':'aa-2','intent':'testing','resources':['db:main','host:ci']}" as actor "codex@laptop"
    When I check in with "" as actor "claude@laptop"
    Then the response path "my_leases" lists the refs "L1"
    And the response path "others_leases" lists the refs "L2"
    And the response path "conflicts" has 1 items
    And the response path "conflicts.0.lease" is "L2"
    And the response path "conflicts.0.holder" is "codex@laptop"
    And the response path "conflicts.0.task" is "aa-2"
    And the response path "conflicts.0.intent" is "testing"
    And the response path "conflicts.0.resources" is "db:main"
    And the response path "conflicts.0.expires_at" is the expiry stored for the lease "L2"

  Scenario: a resource asked about conflicts with a live lease of another
    Given I start a lease with body "{'task':'aa-2','intent':'testing','resources':['db:main','host:ci']}" as actor "codex@laptop"
    When I check in with "resources=host:ci,repo:free" as actor "claude@laptop"
    Then the response status is 200
    And the response path "my_leases" has 0 items
    And the response path "conflicts.0.lease" is "L1"
    And the response path "conflicts.0.resources" is "host:ci"
    When I check in with "resources=repo:free" as actor "claude@laptop"
    Then the response path "conflicts" has 0 items
    And the response path "others_leases" lists the refs "L1"

  Scenario: my own lease does not conflict with the resources I ask about
    Given I start a lease with body "{'task':'aa-1','intent':'wiring','resources':['repo:x']}" as actor "claude@laptop"
    When I check in with "resources=repo:x" as actor "claude@laptop"
    Then the response path "my_leases" lists the refs "L1"
    And the response path "conflicts" has 0 items

  Scenario: a lease past its expiry is left out and expired once, with the sweeper off
    Given I start a lease with body "{'task':'aa-1','intent':'wiring','resources':['repo:x']}" as actor "claude@laptop"
    And I start a lease with body "{'task':'aa-2','intent':'testing','resources':['repo:x']}" as actor "codex@laptop"
    And the lease "L1" ran out 30 seconds ago
    Then the lease "L1" is "live" in the database
    When I check in with "" as actor "claude@laptop"
    Then the response path "my_leases" has 0 items
    And the response path "others_leases" lists the refs "L2"
    And the response path "conflicts" has 0 items
    And the lease "L1" is "expired" in the database
    And exactly 1 lease_expired event exists for the lease "L1"
    When I check in with "" as actor "claude@laptop"
    Then exactly 1 lease_expired event exists for the lease "L1"

  Scenario: a malformed resources list is refused with a 400 and changes nothing
    When I note the counts of tasks, dependencies, events and actors
    And I check in with "resources=a,,b" as actor "claude@laptop"
    Then the response status is 400
    And the counts of tasks, dependencies, events and actors are unchanged
