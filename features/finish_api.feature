Feature: Finishing work in one request, POST /api/v1/finish
  POST /api/v1/finish ends the caller's live lease on a task, writes the outcome as a note on the task and, with
  done, marks the task done. The lease end, the note, the status and their events are written in one transaction,
  and a refused request writes nothing: no lease end, no note, no event.

  Background:
    Given a new database for the board commands
    When I run "vh admin migrate"
    And I run "vh admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port
    And I create the initiative "aa" titled "First" with no tags as actor "claude@laptop"
    And I create a task in "aa" titled "one" as actor "claude@laptop"
    And I create a task in "aa" titled "two" as actor "claude@laptop"

  Scenario: the task is given and the caller holds a lease on it
    Given I start with body "{'task':'aa-1','intent':'wire the thing'}" as actor "claude@laptop"
    And I note the counts of events, notes and live leases
    When I finish with body "{'task':'aa-1','outcome':'wired and tested'}" as actor "claude@laptop"
    Then the response status is 200
    And the response path "lease.ref" is "L1"
    And the response path "lease.state" is "ended"
    And the response path "lease.outcome" is "wired and tested"
    And the response path "note.summary" is "wired and tested"
    And the response path "note.task" is "aa-1"
    And the response path "note.author" is "claude@laptop"
    And the response path "done" is "false"
    And the counts of events, notes and live leases changed by 2, 1 and -1
    And exactly 1 lease_ended event exists for the lease "L1"
    And exactly 1 note_posted event exists for the task "aa-1"
    And exactly 0 task_done events exist for the task "aa-1"
    And the task "aa-1" is open

  Scenario: done also marks the task done
    Given I start with body "{'task':'aa-1','intent':'wire the thing'}" as actor "claude@laptop"
    And I note the counts of events, notes and live leases
    When I finish with body "{'task':'aa-1','outcome':'all of it','done':true}" as actor "claude@laptop"
    Then the response status is 200
    And the response path "lease.state" is "ended"
    And the response path "lease.outcome" is "all of it"
    And the response path "note.summary" is "all of it"
    And the response path "done" is "true"
    And the counts of events, notes and live leases changed by 3, 1 and -1
    And exactly 1 lease_ended event exists for the lease "L1"
    And exactly 1 task_done event exists for the task "aa-1"
    And exactly 1 note_posted event exists for the task "aa-1"
    And the task "aa-1" is done

  Scenario: the task is omitted and the caller holds exactly one live lease
    Given I start with body "{'task':'aa-2','intent':'work'}" as actor "claude@laptop"
    And I note the counts of events, notes and live leases
    When I finish with body "{'outcome':'left it tidy'}" as actor "claude@laptop"
    Then the response status is 200
    And the response path "lease.task" is "aa-2"
    And the response path "note.task" is "aa-2"
    And the counts of events, notes and live leases changed by 2, 1 and -1
    And exactly 1 lease_ended event exists for the lease "L1"

  Scenario: the task is omitted and the caller holds no live lease
    Given I start with body "{'task':'aa-1','intent':'theirs'}" as actor "claude@laptop"
    And I note the counts of events, notes and live leases
    When I finish with body "{'outcome':'x'}" as actor "codex@laptop"
    Then the response status is 409
    And the response path "detail" is "you hold no live lease"
    And the response path "next" is present
    And the counts of events, notes and live leases changed by 0, 0 and 0
    And the actor "codex@laptop" has no row in actors

  Scenario: the task is omitted and the caller holds more than one live lease
    Given I start with body "{'task':'aa-1','intent':'one'}" as actor "claude@laptop"
    And I start with body "{'task':'aa-2','intent':'two'}" as actor "claude@laptop"
    And I note the counts of events, notes and live leases
    When I finish with body "{'outcome':'x'}" as actor "claude@laptop"
    Then the response status is 409
    And the response path "detail" is "you hold 2 live leases"
    And the response path "next" is "Name the task to finish: aa-1 (L1), aa-2 (L2)."
    And the counts of events, notes and live leases changed by 0, 0 and 0

  Scenario: the caller holds no live lease on the given task
    Given I start with body "{'task':'aa-1','intent':'theirs'}" as actor "codex@laptop"
    And I start with body "{'task':'aa-2','intent':'mine'}" as actor "claude@laptop"
    And I note the counts of events, notes and live leases
    When I finish with body "{'task':'aa-1','outcome':'x','done':true}" as actor "claude@laptop"
    Then the response status is 409
    And the response path "detail" is "you hold no live lease on task aa-1"
    And the response path "next" is present
    And the counts of events, notes and live leases changed by 0, 0 and 0
    And the task "aa-1" is open

  Scenario: done while another actor holds a live lease changes nothing
    Given I start with body "{'task':'aa-1','intent':'mine'}" as actor "claude@laptop"
    And I start with body "{'task':'aa-1','intent':'theirs'}" as actor "codex@laptop"
    And I note the counts of events, notes and live leases
    When I finish with body "{'task':'aa-1','outcome':'x','done':true}" as actor "claude@laptop"
    Then the response status is 409
    And the response path "detail" is "cannot mark task aa-1 done: codex@laptop holds lease L2 (theirs)"
    And the response path "next" is present
    And the counts of events, notes and live leases changed by 0, 0 and 0
    And the lease "L1" is "live" in the database
    And exactly 0 lease_ended events exist for the lease "L1"
    And the task "aa-1" is open

  Scenario: an unknown task is 404 and writes nothing
    Given I note the counts of events, notes and live leases
    When I finish with body "{'task':'aa-9','outcome':'x'}" as actor "claude@laptop"
    Then the response status is 404
    And the response path "detail" is "there is no task aa-9"
    And the response path "next" is present
    And the counts of events, notes and live leases changed by 0, 0 and 0

  Scenario Outline: an outcome that is missing or wrong is 400 and writes nothing
    Given I start with body "{'task':'aa-1','intent':'mine'}" as actor "claude@laptop"
    And I note the counts of events, notes and live leases
    When I finish with body "<body>" as actor "claude@laptop"
    Then the response status is 400
    And the response path "next" is present
    And the counts of events, notes and live leases changed by 0, 0 and 0

    Examples:
      | body                                  |
      | {'task':'aa-1'}                       |
      | {'task':'aa-1','outcome':''}          |
      | {'task':'aa-1','outcome':'   '}       |
      | {'outcome':'<<x*201>>'}               |

  Scenario: a finish with no body is 400
    When I finish with body "" as actor "claude@laptop"
    Then the response status is 400
    And the response path "next" is present
