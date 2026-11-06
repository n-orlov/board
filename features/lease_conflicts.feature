Feature: Lease conflicts and the lease list
  Two live leases that hold the same resource conflict, whoever holds them: a lease never conflicts with
  itself, but one actor's leases on two tasks do conflict. Starting and renewing a lease answer the
  conflicts, each naming the other lease, its holder, task, intent, expiry and the shared resources only.
  An ended or expired lease conflicts with nothing. GET /api/v1/leases lists leases by state, actor, task and
  resource.

  Background:
    Given a new database for the board commands
    When I run "board admin migrate"
    And I run "board admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port
    And I create the initiative "aa" titled "First" with no tags as actor "claude@laptop"
    And I create a task in "aa" titled "one" as actor "claude@laptop"
    And I create a task in "aa" titled "two" as actor "claude@laptop"
    And I create a task in "aa" titled "three" as actor "claude@laptop"

  Scenario: a start with no other holder has no conflicts
    When I start a lease with body "{'task':'aa-1','intent':'one','resources':['db:x']}" as actor "claude@laptop"
    Then the response status is 201
    And the response path "conflicts" has 0 items

  Scenario: a start that shares a resource with one holder names it
    Given I start a lease with body "{'task':'aa-1','intent':'migrate','resources':['db:x','repo:y']}" as actor "claude@laptop"
    When I start a lease with body "{'task':'aa-2','intent':'reindex','resources':['db:x','db:z']}" as actor "codex@laptop"
    Then the response status is 201
    And the response path "conflicts" has 1 items
    And the response path "conflicts.0.lease" is "L1"
    And the response path "conflicts.0.holder" is "claude@laptop"
    And the response path "conflicts.0.task" is "aa-1"
    And the response path "conflicts.0.intent" is "migrate"
    And the response path "conflicts.0.expires_at" is present
    And the response path "conflicts.0.resources" is "db:x"

  Scenario: a start that shares resources with two holders names both
    Given I start a lease with body "{'task':'aa-1','intent':'one','resources':['db:x']}" as actor "claude@laptop"
    And I start a lease with body "{'task':'aa-2','intent':'two','resources':['db:y','db:q']}" as actor "gemini@laptop"
    When I start a lease with body "{'task':'aa-3','intent':'three','resources':['db:x','db:y']}" as actor "codex@laptop"
    Then the response status is 201
    And the response path "conflicts" has 2 items
    And the response path "conflicts.0.lease" is "L1"
    And the response path "conflicts.0.holder" is "claude@laptop"
    And the response path "conflicts.0.resources" is "db:x"
    And the response path "conflicts.1.lease" is "L2"
    And the response path "conflicts.1.holder" is "gemini@laptop"
    And the response path "conflicts.1.resources" is "db:y"

  Scenario: one actor's leases on two tasks conflict with each other
    Given I start a lease with body "{'task':'aa-1','intent':'one','resources':['db:x']}" as actor "claude@laptop"
    When I start a lease with body "{'task':'aa-2','intent':'two','resources':['db:x']}" as actor "claude@laptop"
    Then the response status is 201
    And the response path "lease.ref" is "L2"
    And the response path "conflicts" has 1 items
    And the response path "conflicts.0.lease" is "L1"
    And the response path "conflicts.0.holder" is "claude@laptop"
    And the response path "conflicts.0.task" is "aa-1"

  Scenario: a lease renewed by a second start does not conflict with itself
    Given I start a lease with body "{'task':'aa-1','intent':'one','resources':['db:x']}" as actor "claude@laptop"
    When I start a lease with body "{'task':'aa-1','intent':'again','resources':['db:x']}" as actor "claude@laptop"
    Then the response status is 200
    And the response path "lease.ref" is "L1"
    And the response path "conflicts" has 0 items

  Scenario: a second start that renews still answers the conflicts
    Given I start a lease with body "{'task':'aa-1','intent':'one','resources':['db:x']}" as actor "claude@laptop"
    And I start a lease with body "{'task':'aa-2','intent':'two','resources':['db:y']}" as actor "codex@laptop"
    When I start a lease with body "{'task':'aa-2','intent':'two again','resources':['db:x','db:y']}" as actor "codex@laptop"
    Then the response status is 200
    And the response path "lease.ref" is "L2"
    And the response path "conflicts" has 1 items
    And the response path "conflicts.0.lease" is "L1"

  Scenario: an ended lease and an expired lease conflict with nothing
    Given I start a lease with body "{'task':'aa-1','intent':'one','resources':['db:x']}" as actor "claude@laptop"
    And I start a lease with body "{'task':'aa-2','intent':'two','resources':['db:x']}" as actor "codex@laptop"
    And I end the lease "L1" without a body as actor "claude@laptop"
    And the lease "L2" ran out 30 seconds ago
    When I start a lease with body "{'task':'aa-3','intent':'three','resources':['db:x']}" as actor "gemini@laptop"
    Then the response status is 201
    And the response path "conflicts" has 0 items

  Scenario: a renewal with no other holder has no conflicts
    Given I start a lease with body "{'task':'aa-1','intent':'one','resources':['db:x']}" as actor "claude@laptop"
    When I renew the lease "L1" without a body as actor "claude@laptop"
    Then the response status is 200
    And the response path "conflicts" has 0 items

  Scenario: a renewal that takes a resource one holder has names that holder
    Given I start a lease with body "{'task':'aa-1','intent':'one','resources':['db:x']}" as actor "claude@laptop"
    And I start a lease with body "{'task':'aa-2','intent':'two','resources':['db:y']}" as actor "codex@laptop"
    When I renew the lease "L2" with body "{'resources':['db:x','db:y']}" as actor "codex@laptop"
    Then the response status is 200
    And the response path "lease.resources" is "db:x,db:y"
    And the response path "conflicts" has 1 items
    And the response path "conflicts.0.lease" is "L1"
    And the response path "conflicts.0.holder" is "claude@laptop"
    And the response path "conflicts.0.resources" is "db:x"

  Scenario: a renewal that shares resources with two holders names both
    Given I start a lease with body "{'task':'aa-1','intent':'one','resources':['db:x']}" as actor "claude@laptop"
    And I start a lease with body "{'task':'aa-2','intent':'two','resources':['db:y']}" as actor "gemini@laptop"
    And I start a lease with body "{'task':'aa-3','intent':'three','resources':['db:z']}" as actor "codex@laptop"
    When I renew the lease "L3" with body "{'resources':['db:x','db:y','db:z']}" as actor "codex@laptop"
    Then the response status is 200
    And the response path "conflicts" has 2 items
    And the response path "conflicts.0.lease" is "L1"
    And the response path "conflicts.1.lease" is "L2"

  Scenario: a renewal conflicts with the same actor's lease on another task
    Given I start a lease with body "{'task':'aa-1','intent':'one','resources':['db:x']}" as actor "claude@laptop"
    And I start a lease with body "{'task':'aa-2','intent':'two','resources':['db:y']}" as actor "claude@laptop"
    When I renew the lease "L2" with body "{'resources':['db:x']}" as actor "claude@laptop"
    Then the response status is 200
    And the response path "conflicts" has 1 items
    And the response path "conflicts.0.lease" is "L1"
    And the response path "conflicts.0.holder" is "claude@laptop"
    And the response path "conflicts.0.task" is "aa-1"

  Scenario: a lease with no resources conflicts with nothing
    Given I start a lease with body "{'task':'aa-1','intent':'one','resources':['db:x']}" as actor "claude@laptop"
    When I start a lease with body "{'task':'aa-2','intent':'two'}" as actor "codex@laptop"
    Then the response status is 201
    And the response path "conflicts" has 0 items

  # L1 claude aa-1 [db:x] live; L2 codex aa-2 [db:x, repo:y] live; L3 claude aa-3 [repo:y] ended;
  # L4 codex aa-1 [db:z] expired.
  Rule: the list filters

    Background:
      Given I start a lease with body "{'task':'aa-1','intent':'one','resources':['db:x']}" as actor "claude@laptop"
      And I start a lease with body "{'task':'aa-2','intent':'two','resources':['db:x','repo:y']}" as actor "codex@laptop"
      And I start a lease with body "{'task':'aa-3','intent':'three','resources':['repo:y']}" as actor "claude@laptop"
      And I end the lease "L3" without a body as actor "claude@laptop"
      And I start a lease with body "{'task':'aa-1','intent':'four','resources':['db:z']}" as actor "codex@laptop"
      And the lease "L4" ran out 30 seconds ago

    Scenario Outline: the filters combine
      When I list the leases with "<query>" as actor "claude@laptop"
      Then the response status is 200
      And the response path "items" lists the refs "<refs>"

      Examples:
        | query                                | refs        |
        |                                      | L1,L2       |
        | state=live                           | L1,L2       |
        | state=ended                          | L3          |
        | state=expired                        | L4          |
        | state=all                            | L1,L2,L3,L4 |
        | actor=claude@laptop                  | L1          |
        | actor=claude@laptop&state=all        | L1,L3       |
        | actor=codex@laptop&state=expired     | L4          |
        | actor=nobody@laptop&state=all        |             |
        | task=aa-1                            | L1          |
        | task=aa-1&state=all                  | L1,L4       |
        | task=aa-3                            |             |
        | task=aa-3&state=ended                | L3          |
        | resource=db:x                        | L1,L2       |
        | resource=repo:y                      | L2          |
        | resource=repo:y&state=all            | L2,L3       |
        | resource=db:nothing&state=all        |             |
        | resource=db:x&actor=codex@laptop     | L2          |
        | resource=db:x&task=aa-1              | L1          |
        | state=all&actor=codex@laptop&task=aa-1&resource=db:z | L4 |

    Scenario: the list is read after the expiry, so a lease past its time is never live
      When I list the leases with "state=live" as actor "claude@laptop"
      Then the response path "items" lists the refs "L1,L2"
      Given the lease "L2" ran out 30 seconds ago
      When I list the leases with "state=live" as actor "claude@laptop"
      Then the response path "items" lists the refs "L1"
      When I list the leases with "state=expired" as actor "claude@laptop"
      Then the response path "items" lists the refs "L2,L4"
      And exactly 1 lease_expired event exists for the lease "L2"

    Scenario: an item is a lease with its resources
      When I list the leases with "task=aa-2" as actor "claude@laptop"
      Then the response path "items.0.ref" is "L2"
      And the response path "items.0.actor" is "codex@laptop"
      And the response path "items.0.task" is "aa-2"
      And the response path "items.0.resources" is "db:x,repo:y"
      And the response path "next_after" is absent

    Scenario: a page holds the limit and next_after fetches the rest
      When I list the leases with "state=all&limit=3" as actor "claude@laptop"
      Then the response path "items" lists the refs "L1,L2,L3"
      And the response path "next_after" is present
      When I request the next leases page with "state=all&limit=3" as actor "claude@laptop"
      Then the response path "items" lists the refs "L4"
      And the response path "next_after" is absent

    Scenario: a limit that equals the number left is the last page
      When I list the leases with "state=all&limit=4" as actor "claude@laptop"
      Then the response path "items" lists the refs "L1,L2,L3,L4"
      And the response path "next_after" is absent

    Scenario: after takes a lease ref
      When I list the leases with "state=all&after=L2" as actor "claude@laptop"
      Then the response path "items" lists the refs "L3,L4"

    Scenario Outline: a query that cannot be used is refused
      When I list the leases with "<query>" as actor "claude@laptop"
      Then the response status is <status>
      And the response path "next" is present

      Examples:
        | query              | status |
        | state=gone         | 400    |
        | limit=0            | 400    |
        | limit=1001         | 400    |
        | limit=abc          | 400    |
        | after=nothing      | 400    |
        | task=aa-9          | 404    |
