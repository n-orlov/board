Feature: Starting work in one request, POST /api/v1/start
  POST /api/v1/start starts the caller's lease on a task, or on a task it creates (new_task), and answers the
  lease, the conflicts, the caller's check-in as now, the server's protocol version and the caller's
  protocol_seen. The task, the lease and their events are written in one transaction, and a refused request
  writes nothing: no task, no lease, no event.

  Background:
    Given a new database for the board commands
    When I run "vh admin migrate"
    And I run "vh admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port
    And I create the initiative "aa" titled "First" with no tags as actor "claude@laptop"
    And I create the initiative "bb" titled "Second" with no tags as actor "claude@laptop"
    And I create a task in "aa" titled "one" as actor "claude@laptop"
    And I create a task in "aa" titled "two" as actor "claude@laptop"

  Scenario: a new lease on an existing task answers 201 with the lease, the now summary and the protocol
    Given I note the counts of tasks, leases and events
    When I start with body "{'task':'aa-1','intent':'wire the thing','resources':['db:x']}" as actor "claude@laptop"
    Then the response status is 201
    And the response path "lease.ref" is "L1"
    And the response path "lease.task" is "aa-1"
    And the response path "lease.intent" is "wire the thing"
    And the response path "lease.ttl_seconds" is "7200"
    And the response path "lease.resources" is "db:x"
    And the response path "conflicts" has 0 items
    And the response path "task_created" is "false"
    And the response path "now.actor" is "claude@laptop"
    And the response path "now.my_leases" has 1 items
    And the response path "now.my_leases.0.ref" is "L1"
    And the response path "now.my_leases.0.title" is "one"
    And the response path "protocol" is the protocol version the client embeds
    And the response path "protocol_seen" is "0"
    And the counts of tasks, leases and events changed by 0, 1 and 1
    And exactly 1 lease_started event exists for the lease "L1"

  Scenario: the caller's protocol_seen is answered
    Given I record protocol version 3 as read as actor "claude@laptop" through the API
    When I start with body "{'task':'aa-1','intent':'x'}" as actor "claude@laptop"
    Then the response status is 201
    And the response path "protocol_seen" is "3"

  Scenario: starting again on the same task renews the caller's live lease and answers 200
    Given I start with body "{'task':'aa-1','intent':'first','resources':['db:x']}" as actor "claude@laptop"
    And I note the counts of tasks, leases and events
    When I start with body "{'task':'aa-1','intent':'second','ttl_seconds':3600}" as actor "claude@laptop"
    Then the response status is 200
    And the response path "lease.ref" is "L1"
    And the response path "lease.intent" is "second"
    And the response path "lease.ttl_seconds" is "3600"
    And the response path "lease.resources" is "db:x"
    And the response path "now.my_leases" has 1 items
    And the counts of tasks, leases and events changed by 0, 0 and 1
    And exactly 1 lease_started event exists for the lease "L1"
    And exactly 1 lease_renewed event exists for the lease "L1"

  Scenario: a shared resource writes the lease and lists the conflict
    Given I start with body "{'task':'aa-1','intent':'migrate','resources':['db:x','repo:y']}" as actor "claude@laptop"
    And I note the counts of tasks, leases and events
    When I start with body "{'task':'aa-2','intent':'reindex','resources':['db:x']}" as actor "codex@laptop"
    Then the response status is 201
    And the response path "lease.ref" is "L2"
    And the response path "conflicts" has 1 items
    And the response path "conflicts.0.lease" is "L1"
    And the response path "conflicts.0.holder" is "claude@laptop"
    And the response path "conflicts.0.task" is "aa-1"
    And the response path "conflicts.0.resources" is "db:x"
    And the response path "now.conflicts" has 1 items
    And the counts of tasks, leases and events changed by 0, 1 and 1
    And exactly 1 lease_started event exists for the lease "L2"

  Scenario: a done task is 409 and writes nothing
    Given I mark the task "aa-1" done as actor "claude@laptop"
    And I note the counts of tasks, leases and events
    When I start with body "{'task':'aa-1','intent':'x'}" as actor "codex@laptop"
    Then the response status is 409
    And the response path "detail" is "cannot start a lease on task aa-1: it is done"
    And the response path "next" is present
    And the counts of tasks, leases and events changed by 0, 0 and 0
    And the actor "codex@laptop" has no row in actors

  Scenario: a closed task is 409 and writes nothing
    Given I close the task "aa-1" with body "{'resolution':'dropped'}" as actor "claude@laptop"
    And I note the counts of tasks, leases and events
    When I start with body "{'task':'aa-1','intent':'x'}" as actor "codex@laptop"
    Then the response status is 409
    And the response path "detail" is "cannot start a lease on task aa-1: it is closed"
    And the counts of tasks, leases and events changed by 0, 0 and 0
    And the actor "codex@laptop" has no row in actors

  Scenario: an unknown task is 404 and writes nothing
    Given I note the counts of tasks, leases and events
    When I start with body "{'task':'aa-9','intent':'x'}" as actor "codex@laptop"
    Then the response status is 404
    And the response path "detail" is "there is no task aa-9"
    And the response path "next" is present
    And the counts of tasks, leases and events changed by 0, 0 and 0
    And the actor "codex@laptop" has no row in actors

  Scenario: a new task is created and leased together
    Given I note the counts of tasks, leases and events
    When I start with body "{'new_task':{'initiative':'bb','title':'Fresh work'},'intent':'doing it','resources':['repo:z']}" as actor "claude@laptop"
    Then the response status is 201
    And the response path "task_created" is "true"
    And the response path "lease.ref" is "L1"
    And the response path "lease.task" is "bb-1"
    And the response path "now.my_leases.0.title" is "Fresh work"
    And the counts of tasks, leases and events changed by 1, 1 and 2
    And exactly 1 task_created event exists for the task "bb-1"
    And exactly 1 lease_started event exists for the task "bb-1"

  Scenario: a new task in a closed initiative is 409 and creates no task
    Given I close the initiative "bb" without a body as actor "claude@laptop"
    And I note the counts of tasks, leases and events
    When I start with body "{'new_task':{'initiative':'bb','title':'Late'},'intent':'x'}" as actor "codex@laptop"
    Then the response status is 409
    And the response path "next" is present
    And the counts of tasks, leases and events changed by 0, 0 and 0
    And the actor "codex@laptop" has no row in actors

  Scenario: a new task in an unknown initiative is 404 and creates no task
    Given I note the counts of tasks, leases and events
    When I start with body "{'new_task':{'initiative':'zz','title':'Lost'},'intent':'x'}" as actor "codex@laptop"
    Then the response status is 404
    And the response path "detail" is "there is no initiative zz"
    And the counts of tasks, leases and events changed by 0, 0 and 0
    And the actor "codex@laptop" has no row in actors

  Scenario Outline: a start that is malformed or breaks a rule is refused and writes nothing
    Given I note the counts of tasks, leases and events
    When I start with body "<body>" as actor "codex@laptop"
    Then the response status is 400
    And the response path "next" is present
    And the counts of tasks, leases and events changed by 0, 0 and 0
    And the actor "codex@laptop" has no row in actors

    Examples:
      | body                                                                    |
      | {'intent':'x'}                                                          |
      | {'task':'aa-1','new_task':{'initiative':'bb','title':'t'},'intent':'x'} |
      | {'task':'aa-1','intent':''}                                             |
      | {'task':'aa-1','intent':'x','resources':['']}                           |
      | {'new_task':{'initiative':'bb','title':'t'},'intent':''}                |
      | {'new_task':{'initiative':'bb','title':''},'intent':'x'}                |

  Scenario: a start with no body is 400
    When I start with body "" as actor "claude@laptop"
    Then the response status is 400
    And the response path "next" is present
