Feature: Actors over HTTP
  GET /api/v1/actors lists every actor the board has recorded, the one seen last first, each with its handle, kind,
  way back, last_seen, its live leases (ref, task, intent, expiry, resources) and its open asks. alive=true keeps only
  the actors that hold a live lease. GET /api/v1/actors/{handle} answers one actor the same way and a handle that
  names no actor is 404. Time is moved by writing the rows, never by waiting.

  Background:
    Given a new database for the board commands
    When I run "vh admin migrate"
    And I run "vh admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port with BOARD_SWEEP_INTERVAL=off
    And I create the initiative "aa" titled "First" with no tags as actor "claude@laptop"
    And I create a task in "aa" titled "one" as actor "claude@laptop"
    And I create a task in "aa" titled "two" as actor "claude@laptop"
    And I create a task in "aa" titled "three" as actor "claude@laptop"
    And I create a task in "aa" titled "four" as actor "claude@laptop"
    And the next API request carries the header "Board-Actor-Way-Back" with value "tmux attach -t ws1"
    And I start a lease with body "{'task':'aa-1','intent':'wiring','resources':['repo:x','repo:a']}" as actor "claude@laptop"
    And I raise an ask with body "{'task':'aa-1','question':'Which database?','mode':'poll','blocking':true}" as actor "claude@laptop"
    And I start a lease with body "{'task':'aa-2','intent':'testing'}" as actor "codex@laptop"
    And I start a lease with body "{'task':'aa-3','intent':'old work'}" as actor "idle@laptop"
    And the lease "L3" ran out 60 seconds ago
    And I start a lease with body "{'task':'aa-4','intent':'short'}" as actor "gone@laptop"
    And I end the lease "L4" without a body as actor "gone@laptop"
    And the operator actor was last seen 90 minutes ago
    And the actor "idle@laptop" was last seen 30 minutes ago
    And the actor "gone@laptop" was last seen 20 minutes ago
    And the actor "claude@laptop" was last seen 5 minutes ago
    And the actor "codex@laptop" was last seen 1 minutes ago

  Scenario: the list answers every actor newest seen first
    When I request the actors with "" as actor "operator@laptop"
    Then the response status is 200
    And the response list "items" has handle "codex@laptop,claude@laptop,gone@laptop,idle@laptop,operator"

  Scenario: an actor carries its way back, last seen, live leases and open asks
    When I request the actors with "" as actor "operator@laptop"
    Then the response path "items.1.handle" is "claude@laptop"
    And the response path "items.1.kind" is "agent"
    And the response path "items.1.way_back" is "tmux attach -t ws1"
    And the response path "items.1.last_seen" is present
    And the response path "items.1.leases" has 1 items
    And the response path "items.1.leases.0.ref" is "L1"
    And the response path "items.1.leases.0.task" is "aa-1"
    And the response path "items.1.leases.0.intent" is "wiring"
    And the response path "items.1.leases.0.expires_at" is present
    And the response path "items.1.leases.0.resources.0" is "repo:a"
    And the response path "items.1.leases.0.resources.1" is "repo:x"
    And the response path "items.1.asks" has 1 items
    And the response path "items.1.asks.0.ref" is "A1"
    And the response path "items.1.asks.0.task" is "aa-1"
    And the response path "items.1.asks.0.question" is "Which database?"
    And the response path "items.1.asks.0.blocking" is "true"
    And the response path "items.0.way_back" is absent
    And the response path "items.0.asks" has 0 items

  Scenario: an expired or ended lease is not live, so it is not listed
    When I request the actors with "" as actor "operator@laptop"
    Then the response path "items.2.handle" is "gone@laptop"
    And the response path "items.2.leases" has 0 items
    And the response path "items.3.handle" is "idle@laptop"
    And the response path "items.3.leases" has 0 items

  Scenario: alive=true keeps the actors that hold a live lease, still newest seen first
    When I request the actors with "alive=true" as actor "operator@laptop"
    Then the response status is 200
    And the response list "items" has handle "codex@laptop,claude@laptop"
    When I request the actors with "alive=false" as actor "operator@laptop"
    Then the response list "items" has handle "codex@laptop,claude@laptop,gone@laptop,idle@laptop,operator"

  Scenario: an answered ask leaves the actor's open asks
    When I answer the ask "A1" with body "{'answer':'Postgres'}" as actor "operator@laptop"
    And I request the actor "claude@laptop" as actor "operator@laptop"
    Then the response status is 200
    And the response path "asks" has 0 items
    And the response path "leases" has 1 items

  Scenario: one actor is read by handle with the same fields
    When I request the actor "claude@laptop" as actor "operator@laptop"
    Then the response status is 200
    And the response path "handle" is "claude@laptop"
    And the response path "way_back" is "tmux attach -t ws1"
    And the response path "last_seen" is present
    And the response path "leases.0.ref" is "L1"
    And the response path "leases.0.task" is "aa-1"
    And the response path "leases.0.intent" is "wiring"
    And the response path "leases.0.expires_at" is present
    And the response path "leases.0.resources.1" is "repo:x"
    And the response path "asks.0.ref" is "A1"

  Scenario: a handle that names no actor is 404 with a next line
    When I request the actor "nobody@laptop" as actor "operator@laptop"
    Then the response status is 404
    And the response path "next" is present

  Scenario: a lease that ran out is expired by the read and the actor is no longer alive
    When the lease "L1" ran out 10 seconds ago
    And I request the actors with "alive=true" as actor "operator@laptop"
    Then the response list "items" has handle "codex@laptop"
    And the lease "L1" is "expired" in the database

  Scenario: alive that is not a boolean is refused
    When I request the actors with "alive=maybe" as actor "operator@laptop"
    Then the response status is 400
