Feature: Task transitions over HTTP
  A task is open, done or closed. POST /api/v1/tasks/{ref}/done, /reopen and
  /close move it between those, and each move writes one event. A move the
  current status does not allow is 409 and the problem names that status. A
  close names a resolution: completed, dropped, superseded or duplicate. A
  close made for someone else, with a Board-On-Behalf-Of header, records who
  on its event. A refused request changes nothing.

  Background:
    Given a new database for the board commands
    When I run "vh admin migrate"
    And I run "vh admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port
    And I create the initiative "aa" titled "First" with no tags as actor "claude@laptop"
    And I create a task in "aa" titled "one" as actor "claude@laptop"

  Scenario: done moves an open task to done and records who and when
    When I mark the task "aa-1" done as actor "codex@laptop"
    Then the response status is 200
    And the response path "status" is "done"
    And the response path "done_by" is "codex@laptop"
    And the response path "done_at" is present
    And the response path "resolution" is absent
    And the response path "created_by" is "claude@laptop"
    And exactly 1 task_done event exists for the task "aa-1"
    And the latest task_done event of "aa-1" has data.from "open"

  Scenario: reopen moves a done task back to open and clears what done set
    Given I mark the task "aa-1" done as actor "claude@laptop"
    When I reopen the task "aa-1" as actor "codex@laptop"
    Then the response status is 200
    And the response path "status" is "open"
    And the response path "done_at" is absent
    And the response path "done_by" is absent
    And exactly 1 task_reopened event exists for the task "aa-1"
    And the latest task_reopened event of "aa-1" has data.from "done"

  Scenario: close moves an open task to closed with its resolution
    When I close the task "aa-1" with body "{'resolution':'dropped'}" as actor "codex@laptop"
    Then the response status is 200
    And the response path "status" is "closed"
    And the response path "resolution" is "dropped"
    And the response path "closed_by" is "codex@laptop"
    And the response path "closed_at" is present
    And exactly 1 task_closed event exists for the task "aa-1"
    And the latest task_closed event of "aa-1" has data.resolution "dropped"
    And the latest task_closed event of "aa-1" has data.from "open"

  Scenario: close moves a done task to closed and keeps who marked it done
    Given I mark the task "aa-1" done as actor "claude@laptop"
    When I close the task "aa-1" with body "{'resolution':'completed'}" as actor "codex@laptop"
    Then the response status is 200
    And the response path "status" is "closed"
    And the response path "done_by" is "claude@laptop"
    And the response path "closed_by" is "codex@laptop"
    And the latest task_closed event of "aa-1" has data.from "done"

  Scenario: reopen moves a closed task back to open and clears the resolution
    Given I close the task "aa-1" with body "{'resolution':'duplicate'}" as actor "claude@laptop"
    When I reopen the task "aa-1" as actor "codex@laptop"
    Then the response status is 200
    And the response path "status" is "open"
    And the response path "resolution" is absent
    And the response path "closed_at" is absent
    And the response path "closed_by" is absent
    And the latest task_reopened event of "aa-1" has data.from "closed"

  Scenario: done on a done task is refused with its status
    Given I mark the task "aa-1" done as actor "claude@laptop"
    And I note the counts of tasks, dependencies, events and actors
    When I mark the task "aa-1" done as actor "refused@laptop"
    Then the response status is 409
    And the problem detail contains "it is done"
    And the problem detail contains "aa-1"
    And the problem has a next field containing "GET /api/v1/tasks"
    And the counts of tasks, dependencies, events and actors are unchanged

  Scenario: done on a closed task is refused with its status
    Given I close the task "aa-1" with body "{'resolution':'dropped'}" as actor "claude@laptop"
    And I note the counts of tasks, dependencies, events and actors
    When I mark the task "aa-1" done as actor "refused@laptop"
    Then the response status is 409
    And the problem detail contains "it is closed"
    And the counts of tasks, dependencies, events and actors are unchanged

  Scenario: reopen on an open task is refused with its status
    Given I note the counts of tasks, dependencies, events and actors
    When I reopen the task "aa-1" as actor "refused@laptop"
    Then the response status is 409
    And the problem detail contains "it is open"
    And the counts of tasks, dependencies, events and actors are unchanged

  Scenario: close on a closed task is refused with its status
    Given I close the task "aa-1" with body "{'resolution':'dropped'}" as actor "claude@laptop"
    And I note the counts of tasks, dependencies, events and actors
    When I close the task "aa-1" with body "{'resolution':'completed'}" as actor "refused@laptop"
    Then the response status is 409
    And the problem detail contains "it is closed"
    And the response status is 409
    And the counts of tasks, dependencies, events and actors are unchanged
    When I request the task "aa-1" as actor "claude@laptop"
    Then the response path "resolution" is "dropped"

  Scenario Outline: close accepts the resolution <resolution>
    When I close the task "aa-1" with body "{'resolution':'<resolution>'}" as actor "claude@laptop"
    Then the response status is 200
    And the response path "resolution" is "<resolution>"
    And the latest task_closed event of "aa-1" has data.resolution "<resolution>"

    Examples:
      | resolution |
      | completed  |
      | dropped    |
      | superseded |
      | duplicate  |

  Scenario Outline: close refuses the body <body> and changes nothing
    Given I note the counts of tasks, dependencies, events and actors
    When I close the task "aa-1" with body "<body>" as actor "refused@laptop"
    Then the response status is 400
    And the problem detail contains "resolution"
    And the problem has a next field containing "dropped"
    And the counts of tasks, dependencies, events and actors are unchanged
    When I request the task "aa-1" as actor "claude@laptop"
    Then the response path "status" is "open"

    Examples:
      | body                      |
      | {}                        |
      | {'resolution':''}         |
      | {'resolution':'wontfix'}  |
      | {'resolution':'Dropped'}  |

  Scenario: close with no body at all is 400
    When I close the task "aa-1" without a body as actor "refused@laptop"
    Then the response status is 400
    And the problem detail contains "resolution"

  Scenario Outline: <verb> on an unknown task is 404
    When I <request> as actor "refused@laptop"
    Then the response status is 404
    And the problem has a next field containing "GET /api/v1/tasks"

    Examples:
      | verb   | request                                                                  |
      | done   | mark the task "aa-9" done                                                |
      | reopen | reopen the task "aa-9"                                                   |
      | close  | close the task "aa-9" with body "{'resolution':'dropped'}"                |

  Scenario: a task is found by its ref in either letter case for a transition
    When I mark the task "AA-1" done as actor "claude@laptop"
    Then the response status is 200
    And the response path "ref" is "aa-1"

  Scenario: a closed task cannot be patched until it is reopened
    Given I close the task "aa-1" with body "{'resolution':'dropped'}" as actor "claude@laptop"
    When I patch the task "aa-1" with body "{'title':'two'}" as actor "claude@laptop"
    Then the response status is 409
    And the problem has a next field containing "reopen"
    When I reopen the task "aa-1" as actor "claude@laptop"
    And I patch the task "aa-1" with body "{'title':'two'}" as actor "claude@laptop"
    Then the response status is 200
    And the response path "title" is "two"

  Scenario: a close made on behalf of the operator records the operator on its event
    Given the next API request carries the header "Board-On-Behalf-Of" with value "operator"
    When I close the task "aa-1" with body "{'resolution':'completed'}" as actor "claude@laptop"
    Then the response status is 200
    And the latest task_closed event of "aa-1" is on behalf of "operator"
    And the actor "operator" has exactly one row in actors
    And the actor "claude@laptop" has exactly one row in actors

  Scenario: a close with no Board-On-Behalf-Of header is on behalf of nobody
    When I close the task "aa-1" with body "{'resolution':'completed'}" as actor "claude@laptop"
    Then the response status is 200
    And the latest task_closed event of "aa-1" is on behalf of nobody

  Scenario: a close refused for its resolution records no actor for the header
    Given the next API request carries the header "Board-On-Behalf-Of" with value "delegate"
    When I close the task "aa-1" with body "{'resolution':'wontfix'}" as actor "claude@laptop"
    Then the response status is 400
    And the actor "delegate" has no row in actors
