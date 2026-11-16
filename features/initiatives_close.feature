Feature: Closing and reopening an initiative over HTTP
  POST /api/v1/initiatives/{slug}/close closes an open initiative and writes
  initiative_closed. With a task that is open or done and no force it answers 409
  whose next names the force option and changes nothing. With force it first
  closes each task that is not closed as dropped, each with its task_closed event,
  and then the initiative. Closing a closed initiative is 409. POST .../reopen
  reopens a closed initiative with initiative_reopened and leaves its tasks closed;
  reopening an open one is 409. A closed initiative takes no new task, no edit to
  a task and no task reopened.

  Background:
    Given a new database for the board commands
    When I run "vh admin migrate"
    And I run "vh admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port
    And I create the initiative "aa" titled "First" with no tags as actor "claude@laptop"
    And I create the initiative "bb" titled "Second" with no tags as actor "claude@laptop"

  Scenario: an initiative with no tasks closes
    When I close the initiative "aa" without a body as actor "op@laptop"
    Then the response status is 200
    And the response path "status" is "closed"
    And the response path "closed_by" is "op@laptop"
    And the response path "closed_at" is present
    And exactly 1 initiative_closed event exists for the initiative "aa"
    When I request the initiative "aa" as actor "op@laptop"
    Then the response path "status" is "closed"
    And the response path "closed_by" is "op@laptop"

  Scenario: an open task stops the close without force
    Given I create a task in "aa" titled "one" as actor "claude@laptop"
    And I note the counts of tasks, dependencies, events and actors
    When I close the initiative "aa" without a body as actor "op@laptop"
    Then the response status is 409
    And the problem has a next field containing "force"
    And the counts of tasks, dependencies, events and actors are unchanged
    And exactly 0 initiative_closed event exists for the initiative "aa"
    When I request the initiative "aa" as actor "op@laptop"
    Then the response path "status" is "open"
    When I request the task "aa-1" as actor "op@laptop"
    Then the response path "status" is "open"

  Scenario: force false is the same as no force
    Given I create a task in "aa" titled "one" as actor "claude@laptop"
    When I close the initiative "aa" with body "{'force':false}" as actor "op@laptop"
    Then the response status is 409
    And the problem has a next field containing "force"

  Scenario: a done task stops the close without force
    Given I create a task in "aa" titled "one" as actor "claude@laptop"
    And I mark the task "aa-1" done as actor "claude@laptop"
    When I close the initiative "aa" with body "{}" as actor "op@laptop"
    Then the response status is 409
    And the problem has a next field containing "force"
    When I request the task "aa-1" as actor "op@laptop"
    Then the response path "status" is "done"

  Scenario: closed tasks do not stop the close
    Given I create a task in "aa" titled "one" as actor "claude@laptop"
    And I close the task "aa-1" with body "{'resolution':'completed'}" as actor "claude@laptop"
    When I close the initiative "aa" without a body as actor "op@laptop"
    Then the response status is 200
    And the response path "status" is "closed"
    When I request the task "aa-1" as actor "op@laptop"
    Then the response path "resolution" is "completed"

  Scenario: force drops the tasks that are not closed and then closes the initiative
    Given I create a task in "aa" titled "open one" as actor "claude@laptop"
    And I create a task in "aa" titled "done one" as actor "claude@laptop"
    And I create a task in "aa" titled "closed one" as actor "claude@laptop"
    And I create a task in "bb" titled "elsewhere" as actor "claude@laptop"
    And I mark the task "aa-2" done as actor "claude@laptop"
    And I close the task "aa-3" with body "{'resolution':'completed'}" as actor "claude@laptop"
    When I close the initiative "aa" with body "{'force':true}" as actor "op@laptop"
    Then the response status is 200
    And the response path "status" is "closed"
    When I request the events list "initiative=aa&type=task_closed&type=initiative_closed" as actor "op@laptop"
    Then the response items have the types "task_closed,task_closed,task_closed,initiative_closed"
    And the response items have the actors "claude@laptop,op@laptop,op@laptop,op@laptop"
    When I request the task "aa-1" as actor "op@laptop"
    Then the response path "status" is "closed"
    And the response path "resolution" is "dropped"
    And the response path "closed_by" is "op@laptop"
    When I request the task "aa-2" as actor "op@laptop"
    Then the response path "status" is "closed"
    And the response path "resolution" is "dropped"
    And the response path "done_by" is "claude@laptop"
    When I request the task "aa-3" as actor "op@laptop"
    Then the response path "resolution" is "completed"
    And the response path "closed_by" is "claude@laptop"
    When I request the task "bb-1" as actor "op@laptop"
    Then the response path "status" is "open"
    And the latest initiative_closed event of the initiative "aa" has data.dropped "2"
    And the latest initiative_closed event of the initiative "aa" has data.force "true"
    And the latest task_closed event of "aa-1" has data.resolution "dropped"
    And the latest task_closed event of "aa-2" has data.from "done"

  Scenario: closing a closed initiative is 409
    When I close the initiative "aa" without a body as actor "op@laptop"
    Then the response status is 200
    When I close the initiative "aa" with body "{'force':true}" as actor "op@laptop"
    Then the response status is 409
    And the problem has a next field containing "reopen"
    And exactly 1 initiative_closed event exists for the initiative "aa"

  Scenario: closing an unknown initiative is 404
    When I close the initiative "zz" without a body as actor "op@laptop"
    Then the response status is 404
    And the problem has a next field containing "POST /api/v1/initiatives"

  Scenario: reopen writes initiative_reopened and the tasks stay closed
    Given I create a task in "aa" titled "one" as actor "claude@laptop"
    When I close the initiative "aa" with body "{'force':true}" as actor "op@laptop"
    And I reopen the initiative "aa" as actor "op@laptop"
    Then the response status is 200
    And the response path "status" is "open"
    And the response path "closed_at" is absent
    And the response path "closed_by" is absent
    And exactly 1 initiative_reopened event exists for the initiative "aa"
    When I request the task "aa-1" as actor "op@laptop"
    Then the response path "status" is "closed"
    And the response path "resolution" is "dropped"
    When I request the events list "initiative=aa&type=initiative_closed&type=initiative_reopened" as actor "op@laptop"
    Then the response items have the types "initiative_closed,initiative_reopened"

  Scenario: a reopened initiative takes tasks again and can be closed again
    When I close the initiative "aa" without a body as actor "op@laptop"
    And I reopen the initiative "aa" as actor "op@laptop"
    And I create a task in "aa" titled "again" as actor "claude@laptop"
    Then the response status is 201
    When I close the initiative "aa" without a body as actor "op@laptop"
    Then the response status is 409
    When I close the initiative "aa" with body "{'force':true}" as actor "op@laptop"
    Then the response status is 200
    And exactly 2 initiative_closed event exists for the initiative "aa"

  Scenario: reopening an open initiative is 409
    When I reopen the initiative "aa" as actor "op@laptop"
    Then the response status is 409
    And the problem has a next field containing "closed"
    And exactly 0 initiative_reopened event exists for the initiative "aa"

  Scenario: reopening an unknown initiative is 404
    When I reopen the initiative "zz" as actor "op@laptop"
    Then the response status is 404

  Scenario: a closed initiative takes no new task
    Given I close the initiative "aa" without a body as actor "op@laptop"
    And I note the counts of tasks, dependencies, events and actors
    When I create a task in "aa" titled "late" as actor "claude@laptop"
    Then the response status is 409
    And the counts of tasks, dependencies, events and actors are unchanged

  Scenario: a task of a closed initiative cannot be patched or reopened
    Given I create a task in "aa" titled "one" as actor "claude@laptop"
    And I create a task in "aa" titled "two" as actor "claude@laptop"
    And I close the task "aa-2" with body "{'resolution':'completed'}" as actor "claude@laptop"
    And I close the initiative "aa" with body "{'force':true}" as actor "op@laptop"
    And I note the counts of tasks, dependencies, events and actors
    When I patch the task "aa-1" with body "{'title':'renamed'}" as actor "claude@laptop"
    Then the response status is 409
    When I reopen the task "aa-1" as actor "claude@laptop"
    Then the response status is 409
    When I reopen the task "aa-2" as actor "claude@laptop"
    Then the response status is 409
    And the counts of tasks, dependencies, events and actors are unchanged
    When I request the task "aa-1" as actor "op@laptop"
    Then the response path "title" is "one"
    And the response path "status" is "closed"
