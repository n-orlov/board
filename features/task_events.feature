Feature: Watch checks, milestones and a task's events over HTTP
  POST /api/v1/tasks/{ref}/checks records a check on a watch task and moves its
  next_check_at, to the time sent or to now plus the cadence. A work task, and a
  closed watch task, answer 409. A watch task is overdue while it is not closed and
  its next_check_at is in the past. POST /api/v1/tasks/{ref}/milestones records a
  milestone, with optional data, on a task of any kind and status. GET
  /api/v1/tasks/{ref}/events lists the events that name one task in the order they
  were written.

  Background:
    Given a new database for the board commands
    When I run "board admin migrate"
    And I run "board admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port
    And I create the initiative "aa" titled "First" with no tags as actor "claude@laptop"
    And I create a task in "aa" titled "work one" as actor "claude@laptop"
    And I create a watch task in "aa" titled "watch two" with a cadence of 3600 seconds as actor "claude@laptop"
    And I create a watch task in "aa" titled "watch three" with a cadence of 7200 seconds as actor "claude@laptop"

  Scenario: a check without next_check_at moves it to now plus the cadence
    When I record a check on the task "aa-2" with body "{'summary':'build is green'}" as actor "codex@laptop"
    Then the response status is 200
    And the response path "ref" is "aa-2"
    And the response path "kind" is "watch"
    And the response next_check_at is the request time plus 3600 seconds
    And the response path "overdue" is "false"
    And exactly 1 check_recorded event exists for the task "aa-2"
    And the latest check_recorded event of "aa-2" has data.summary "build is green"
    And the latest check_recorded event of "aa-2" has data.ref "aa-2"

  Scenario: a check with next_check_at moves it to that time
    When I record a check on the task "aa-3" with body "{'summary':'still waiting','next_check_at':'2099-01-02T03:04:05Z'}" as actor "codex@laptop"
    Then the response status is 200
    And the response path "next_check_at" is "2099-01-02T03:04:05Z"
    And the response path "overdue" is "false"
    When I request the task "aa-3" as actor "claude@laptop"
    Then the response path "next_check_at" is "2099-01-02T03:04:05Z"
    And the response path "overdue" is "false"

  Scenario: a check is recorded as the actor that sent it
    When I record a check on the task "aa-2" with body "{'summary':'seen'}" as actor "codex@laptop"
    And I request the events of the task "aa-2" as actor "claude@laptop"
    Then the response items have the types "task_created,check_recorded"
    And the response items have the actors "claude@laptop,codex@laptop"

  Scenario: a watch task is overdue while next_check_at is in the past and a later check clears it
    When I record a check on the task "aa-2" with body "{'summary':'late','next_check_at':'2020-01-01T00:00:00Z'}" as actor "codex@laptop"
    Then the response status is 200
    And the response path "overdue" is "true"
    When I request the task "aa-2" as actor "claude@laptop"
    Then the response path "overdue" is "true"
    And the response path "next_check_at" is "2020-01-01T00:00:00Z"
    When I record a check on the task "aa-2" with body "{'summary':'caught up'}" as actor "codex@laptop"
    Then the response path "overdue" is "false"
    When I request the task "aa-2" as actor "claude@laptop"
    Then the response path "overdue" is "false"

  Scenario: a closed watch task is not overdue
    Given I record a check on the task "aa-2" with body "{'summary':'late','next_check_at':'2020-01-01T00:00:00Z'}" as actor "codex@laptop"
    When I close the task "aa-2" with body "{'resolution':'completed'}" as actor "claude@laptop"
    And I request the task "aa-2" as actor "claude@laptop"
    Then the response path "overdue" is "false"

  Scenario: a check on a work task is 409 and writes nothing
    Given I note the counts of tasks, dependencies, events and actors
    When I record a check on the task "aa-1" with body "{'summary':'nothing to watch'}" as actor "refused@laptop"
    Then the response status is 409
    And the problem detail contains "aa-1"
    And the problem has a next field containing "GET /api/v1/tasks"
    And exactly 0 check_recorded events exist for the task "aa-1"
    And the counts of tasks, dependencies, events and actors are unchanged

  Scenario: a check on a closed watch task is 409
    Given I close the task "aa-3" with body "{'resolution':'dropped'}" as actor "claude@laptop"
    And I note the counts of tasks, dependencies, events and actors
    When I record a check on the task "aa-3" with body "{'summary':'too late'}" as actor "refused@laptop"
    Then the response status is 409
    And the problem detail contains "closed"
    And the counts of tasks, dependencies, events and actors are unchanged

  Scenario Outline: a check whose body cannot be used is 400 and writes nothing
    Given I note the counts of tasks, dependencies, events and actors
    When I record a check on the task "aa-2" with body "<body>" as actor "refused@laptop"
    Then the response status is 400
    And the problem has a next field containing "summary"
    And the counts of tasks, dependencies, events and actors are unchanged

    Examples:
      | body                                                   |
      | {'summary':''}                                         |
      | {'summary':'<<x*201>>'}                                |
      | {'next_check_at':'2099-01-02T03:04:05Z'}               |

  Scenario: a check with no body is 400
    When I record a check on the task "aa-2" without a body as actor "refused@laptop"
    Then the response status is 400

  Scenario: a check on a task that does not exist is 404
    When I record a check on the task "aa-9" with body "{'summary':'x'}" as actor "refused@laptop"
    Then the response status is 404
    And the problem detail contains "aa-9"

  Scenario: a milestone with data keeps the data in the event
    When I record a milestone on the task "aa-1" with body "{'summary':'beta shipped','data':{'build':42,'tags':['a','b']}}" as actor "codex@laptop"
    Then the response status is 200
    And the response path "ref" is "aa-1"
    And the response path "status" is "open"
    When I request the events of the task "aa-1" as actor "claude@laptop"
    Then the response items have the types "task_created,milestone"
    And the response path "items.1.summary" is "milestone on aa-1: beta shipped"
    And the response path "items.1.actor" is "codex@laptop"
    And the response path "items.1.data.summary" is "beta shipped"
    And the response path "items.1.data.data.build" is "42"
    And the response path "items.1.data.data.tags" is "a,b"

  Scenario: a milestone without data has no data key
    When I record a milestone on the task "aa-2" with body "{'summary':'cutover'}" as actor "codex@laptop"
    Then the response status is 200
    When I request the events of the task "aa-2" as actor "claude@laptop"
    Then the response path "items.1.type" is "milestone"
    And the response path "items.1.data.summary" is "cutover"
    And the response path "items.1.data.data" is absent

  Scenario: a milestone does not touch the task
    Given I request the task "aa-2" as actor "claude@laptop"
    When I record a milestone on the task "aa-2" with body "{'summary':'cutover'}" as actor "codex@laptop"
    Then the response path "next_check_at" is present
    And exactly 0 check_recorded events exist for the task "aa-2"

  Scenario: a milestone is accepted on a done task and a closed task
    Given I mark the task "aa-1" done as actor "claude@laptop"
    When I record a milestone on the task "aa-1" with body "{'summary':'retro held'}" as actor "codex@laptop"
    Then the response status is 200
    And the response path "status" is "done"
    Given I close the task "aa-1" with body "{'resolution':'completed'}" as actor "claude@laptop"
    When I record a milestone on the task "aa-1" with body "{'summary':'archived'}" as actor "codex@laptop"
    Then the response status is 200
    And the response path "status" is "closed"
    And exactly 2 milestone events exist for the task "aa-1"

  Scenario Outline: a milestone whose body cannot be used is 400 and writes nothing
    Given I note the counts of tasks, dependencies, events and actors
    When I record a milestone on the task "aa-1" with body "<body>" as actor "refused@laptop"
    Then the response status is 400
    And the counts of tasks, dependencies, events and actors are unchanged

    Examples:
      | body                              |
      | {'summary':''}                    |
      | {'summary':'<<x*201>>'}           |
      | {'data':{'build':1}}              |
      | {'summary':'x','data':'text'}     |

  Scenario: a milestone on a task that does not exist is 404
    When I record a milestone on the task "aa-9" with body "{'summary':'x'}" as actor "refused@laptop"
    Then the response status is 404
    And the problem detail contains "aa-9"

  Scenario: a task's events are listed in order after create, edit, done, reopen and close
    Given I patch the task "aa-1" with body "{'title':'work one, renamed'}" as actor "editor@laptop"
    And I mark the task "aa-1" done as actor "finisher@laptop"
    And I reopen the task "aa-1" as actor "reopener@laptop"
    And I close the task "aa-1" with body "{'resolution':'dropped'}" as actor "closer@laptop"
    When I request the events of the task "aa-1" as actor "claude@laptop"
    Then the response status is 200
    And the response items have the types "task_created,task_updated,task_done,task_reopened,task_closed"
    And the response items have the actors "claude@laptop,editor@laptop,finisher@laptop,reopener@laptop,closer@laptop"
    And the response items are the events "2,5,6,7,8"
    And the response has no next_after
    And item 1 of the response is a complete event with "initiative,task,ref_id"

  Scenario: a task's events leave out the events of other tasks and of the initiative
    Given I mark the task "aa-2" done as actor "claude@laptop"
    When I request the events of the task "aa-1" as actor "claude@laptop"
    Then the response items have the types "task_created"
    When I request the events of the task "aa-2" as actor "claude@laptop"
    Then the response items have the types "task_created,task_done"

  Scenario: a task's events page with limit and after
    Given I patch the task "aa-1" with body "{'title':'two'}" as actor "claude@laptop"
    And I patch the task "aa-1" with body "{'title':'three'}" as actor "claude@laptop"
    When I request the events of the task "aa-1" with "limit=2" as actor "claude@laptop"
    Then the response items have the types "task_created,task_updated"
    And the response next_after is the id of the event 5
    When I request the next events page of the task "aa-1" with "limit=2" as actor "claude@laptop"
    Then the response items have the types "task_updated"
    And the response has no next_after

  Scenario: a task that does not exist and paging parameters that cannot be read are refused
    When I request the events of the task "aa-9" as actor "claude@laptop"
    Then the response status is 404
    And the problem detail contains "aa-9"
    When I request the events of the task "aa-1" with "limit=0" as actor "claude@laptop"
    Then the response status is 400
    When I request the events of the task "aa-1" with "after=nonsense" as actor "claude@laptop"
    Then the response status is 400
    And the problem has a next field containing "E<n>"
