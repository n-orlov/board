Feature: Tasks over HTTP
  An authenticated caller creates, reads and edits tasks through /api/v1/tasks.
  A task is numbered <initiative slug>-<seq> from 1 within its initiative and
  is found by that ref in either letter case or by its ULID. A refused request
  changes nothing: no task, dependency, event or actor. A patch that changes
  nothing writes no event, and a change writes task_updated naming the fields.

  Background:
    Given a new database for the board commands
    When I run "board admin migrate"
    And I run "board admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port
    And I create the initiative "aa" titled "First" with no tags as actor "claude@laptop"
    And I create the initiative "bb" titled "Second" with no tags as actor "claude@laptop"

  Scenario: create a work task
    When I create the task with body "{'initiative':'aa','title':'  Wire the lights  ','description':'Use the **hub**','planned_start':'2026-12-01T09:00:00Z','planned_end':'2026-12-03T17:00:00Z','external_refs':[{'label':'PR 12','url':'https://example.com/pr/12'}]}" as actor "claude@laptop"
    Then the response status is 201
    And the response path "ref" is "aa-1"
    And the response path "initiative" is "aa"
    And the response path "seq" is "1"
    And the response path "title" is "Wire the lights"
    And the response path "description" is "Use the **hub**"
    And the response path "kind" is "work"
    And the response path "status" is "open"
    And the response path "planned_start" is "2026-12-01T09:00:00Z"
    And the response path "planned_end" is "2026-12-03T17:00:00Z"
    And the response path "external_refs.0.label" is "PR 12"
    And the response path "external_refs.0.url" is "https://example.com/pr/12"
    And the response path "created_by" is "claude@laptop"
    And the response path "cadence_seconds" is absent
    And the response path "next_check_at" is absent
    And the response has an "id" of 26 characters
    And exactly 1 task_created event exists for the task "aa-1"
    And the latest task_created event of "aa-1" has data.kind "work"

  Scenario: create a watch task whose next_check_at defaults to now plus the cadence
    When I create a watch task in "aa" titled "Watch the build" with a cadence of 3600 seconds as actor "claude@laptop"
    Then the response status is 201
    And the response path "kind" is "watch"
    And the response path "cadence_seconds" is "3600"
    And the response next_check_at is the request time plus 3600 seconds
    And the response path "overdue" is "false"

  Scenario: a watch task may name its own next_check_at
    When I create the task with body "{'initiative':'aa','title':'Watch','kind':'watch','cadence_seconds':60,'next_check_at':'2027-01-02T03:04:05Z'}" as actor "claude@laptop"
    Then the response status is 201
    And the response path "next_check_at" is "2027-01-02T03:04:05Z"

  Scenario: a watch task needs a cadence
    When I create the task with body "{'initiative':'aa','title':'Watch','kind':'watch'}" as actor "claude@laptop"
    Then the response status is 400
    And the problem detail contains "cadence"

  Scenario Outline: the largest and smallest accepted values create a task
    When I create the task with body "<body>" as actor "claude@laptop"
    Then the response status is 201

    Examples:
      | body                                                                                                            |
      | {'initiative':'aa','title':'<<t*200>>'}                                                                         |
      | {'initiative':'aa','title':'t'}                                                                                 |
      | {'initiative':'aa','title':'t','description':'<<d*20000>>'}                                                     |
      | {'initiative':'aa','title':'t','kind':'watch','cadence_seconds':60}                                             |
      | {'initiative':'aa','title':'t','kind':'watch','cadence_seconds':31536000}                                       |
      | {'initiative':'aa','title':'t','external_refs':[{'label':'l','url':'http://example.com'}]}                      |
      | {'initiative':'aa','title':'t','external_refs':[{'label':'<<l*80>>','url':'https://example.com'}]}              |
      | {'initiative':'aa','title':'t','external_refs':[<<{'label':'l','url':'https://e.example'}*20;,>>]}             |
      | {'initiative':'aa','title':'t','planned_start':'2026-12-01T00:00:00Z','planned_end':'2026-12-01T00:00:00Z'}     |

  Scenario Outline: a bad field is 400 and writes no row, event or actor
    Given I note the counts of tasks, dependencies, events and actors
    When I create the task with body "<body>" as actor "refused@laptop"
    Then the response status is 400
    And the problem detail contains "<detail>"
    And the problem has a next field containing "<next>"
    And the counts of tasks, dependencies, events and actors are unchanged
    And there is no actor row for "refused@laptop"

    Examples:
      | body                                                                                                        | detail          | next            |
      | {'initiative':'aa','title':''}                                                                              | title           | title           |
      | {'initiative':'aa','title':'   '}                                                                           | title           | title           |
      | {'initiative':'aa'}                                                                                         | title           | title           |
      | {'initiative':'aa','title':'<<t*201>>'}                                                                     | title           | title           |
      | {'initiative':'aa','title':'t','description':'<<d*20001>>'}                                                 | description     | description     |
      | {'initiative':'aa','title':'t','cadence_seconds':3600}                                                      | cadence_seconds | cadence_seconds |
      | {'initiative':'aa','title':'t','next_check_at':'2027-01-02T03:04:05Z'}                                      | next_check_at   | cadence_seconds |
      | {'initiative':'aa','title':'t','kind':'watch','cadence_seconds':59}                                         | cadence_seconds | cadence_seconds |
      | {'initiative':'aa','title':'t','kind':'watch','cadence_seconds':31536001}                                   | cadence_seconds | cadence_seconds |
      | {'initiative':'aa','title':'t','kind':'other'}                                                              | kind            | work or watch   |
      | {'initiative':'aa','title':'t','external_refs':[{'label':'','url':'https://example.com'}]}                  | label           | label           |
      | {'initiative':'aa','title':'t','external_refs':[{'label':'<<l*81>>','url':'https://example.com'}]}          | label           | label           |
      | {'initiative':'aa','title':'t','external_refs':[{'label':'l','url':'ftp://example.com/x'}]}                 | url             | https           |
      | {'initiative':'aa','title':'t','external_refs':[{'label':'l','url':'javascript:alert(1)'}]}                 | url             | https           |
      | {'initiative':'aa','title':'t','external_refs':[<<{'label':'l','url':'https://e.example'}*21;,>>]}         | external_refs   | external_refs   |
      | {'initiative':'aa','title':'t','planned_start':'2026-12-02T00:00:00Z','planned_end':'2026-12-01T00:00:00Z'} | planned_end     | planned_end     |

  Scenario: a task in an unknown initiative is 404 and writes nothing
    Given I note the counts of tasks, dependencies, events and actors
    When I create the task with body "{'initiative':'nothing-here','title':'t'}" as actor "refused@laptop"
    Then the response status is 404
    And the problem has a next field containing "GET /api/v1/initiatives"
    And the counts of tasks, dependencies, events and actors are unchanged
    And there is no actor row for "refused@laptop"

  Scenario: a task can be created in an initiative named by its ULID in either case
    When I create a task in the initiative "aa" by its id titled "By id" as actor "claude@laptop"
    Then the response status is 201
    And the response path "ref" is "aa-1"
    When I create a task in the initiative "aa" by its lowercase id titled "By lowercase id" as actor "claude@laptop"
    Then the response path "ref" is "aa-2"

  Scenario: a task starts out blocked by the tasks blocked_by names
    When I create a task in "aa" titled "First" as actor "claude@laptop"
    And I create a task in "bb" titled "Other" as actor "claude@laptop"
    And I create the task with body "{'initiative':'aa','title':'Third','blocked_by':['AA-1','bb-1','aa-1']}" as actor "claude@laptop"
    Then the response status is 201
    And the response path "ref" is "aa-2"
    And the response path "blocked" is "true"
    And the response path "deps" lists the refs "aa-1,bb-1"
    And the response path "deps.0.title" is "First"
    And the response path "deps.0.status" is "open"
    And exactly 2 dep_added events exist for the task "aa-2"
    And exactly 0 dep_added events exist for the task "aa-1"
    And the latest dep_added event of "aa-2" has data.blocked_by "bb-1"
    When I request the task "aa-1" as actor "claude@laptop"
    Then the response path "blocked" is "false"
    And the response path "blocking" lists the refs "aa-2"
    And the response path "blocking.0.title" is "Third"

  Scenario: an unknown blocker is 404 and the task is not created
    Given I create a task in "aa" titled "First" as actor "claude@laptop"
    And I note the counts of tasks, dependencies, events and actors
    When I create the task with body "{'initiative':'aa','title':'t','blocked_by':['aa-1','aa-9']}" as actor "refused@laptop"
    Then the response status is 404
    And the problem detail contains "aa-9"
    And the counts of tasks, dependencies, events and actors are unchanged
    And there is no actor row for "refused@laptop"
    When I create a task in "aa" titled "Next" as actor "claude@laptop"
    Then the response path "ref" is "aa-2"

  Scenario: refs count per initiative from 1
    When I create a task in "aa" titled "one" as actor "claude@laptop"
    And I create a task in "aa" titled "two" as actor "claude@laptop"
    And I create a task in "bb" titled "three" as actor "claude@laptop"
    Then the response path "ref" is "bb-1"
    When I request the task "aa-2" as actor "claude@laptop"
    Then the response path "title" is "two"
    When I request the task "bb-1" as actor "claude@laptop"
    Then the response path "title" is "three"

  Scenario: get a task by upper-case ref and by ULID
    When I create a task in "aa" titled "one" as actor "claude@laptop"
    And I request the task "AA-1" as actor "claude@laptop"
    Then the response status is 200
    And the response path "ref" is "aa-1"
    When I request the task "aa-1" by its id as actor "claude@laptop"
    Then the response status is 200
    And the response path "ref" is "aa-1"
    When I request the task "aa-1" by its lowercase id as actor "claude@laptop"
    Then the response status is 200
    And the response path "ref" is "aa-1"

  Scenario: the task carries its links, leases and derived state
    When I create a task in "aa" titled "one" as actor "claude@laptop"
    And I request the task "aa-1" as actor "claude@laptop"
    Then the response status is 200
    And the response path "initiative" is "aa"
    And the response path "deps" has 0 items
    And the response path "blocking" has 0 items
    And the response path "live_leases" has 0 items
    And the response path "active" is "false"
    And the response path "blocked" is "false"
    And the response path "overdue" is "false"

  Scenario Outline: an unknown or malformed ref is 404 with a next field
    When I request the task "<ref>" as actor "claude@laptop"
    Then the response status is 404
    And the problem has a next field containing "GET /api/v1/tasks"

    Examples:
      | ref                        |
      | aa-1                       |
      | zz-1                       |
      | aa-0                       |
      | nonsense                   |
      | 01JZZZZZZZZZZZZZZZZZZZZZZZ |

  Scenario Outline: patch one field
    Given I create a task in "aa" titled "work task" as actor "claude@laptop"
    And I create a watch task in "aa" titled "watch task" with a cadence of 3600 seconds as actor "claude@laptop"
    When I patch the task "<ref>" with body "<body>" as actor "claude@laptop"
    Then the response status is 200
    And the response path "<path>" is "<value>"
    And exactly 1 task_updated event exists for the task "<ref>"
    And the latest task_updated event of "<ref>" has data.changed "<field>"
    When I request the task "<ref>" as actor "claude@laptop"
    Then the response path "<path>" is "<value>"

    Examples:
      | ref  | body                                                                      | field           | path                  | value                     |
      | aa-1 | {'title':'  New title  '}                                                 | title           | title                 | New title                 |
      | aa-1 | {'description':'New **text**'}                                            | description     | description           | New **text**              |
      | aa-1 | {'planned_start':'2026-12-01T10:00:00Z'}                                  | planned_start   | planned_start         | 2026-12-01T10:00:00Z      |
      | aa-1 | {'planned_end':'2026-12-02T10:00:00Z'}                                    | planned_end     | planned_end           | 2026-12-02T10:00:00Z      |
      | aa-2 | {'cadence_seconds':7200}                                                  | cadence_seconds | cadence_seconds       | 7200                      |
      | aa-2 | {'next_check_at':'2027-01-01T00:00:00Z'}                                  | next_check_at   | next_check_at         | 2027-01-01T00:00:00Z      |
      | aa-1 | {'external_refs':[{'label':'PR','url':'https://example.com/pr/1'}]}       | external_refs   | external_refs.0.label | PR                        |

  Scenario: a patch of several fields names each of them in column order
    Given I create a task in "aa" titled "one" as actor "claude@laptop"
    When I patch the task "aa-1" with body "{'planned_end':'2026-12-02T10:00:00Z','description':'d','title':'two'}" as actor "claude@laptop"
    Then the response status is 200
    And the latest task_updated event of "aa-1" has data.changed "title,description,planned_end"

  Scenario: a patch replaces the external refs and null clears a date
    Given I create the task with body "{'initiative':'aa','title':'one','planned_start':'2026-12-01T10:00:00Z','external_refs':[{'label':'a','url':'https://a.example'},{'label':'b','url':'https://b.example'}]}" as actor "claude@laptop"
    When I patch the task "aa-1" with body "{'planned_start':null,'external_refs':[{'label':'c','url':'https://c.example'}]}" as actor "claude@laptop"
    Then the response status is 200
    And the response path "planned_start" is absent
    And the response path "external_refs" has 1 items
    And the latest task_updated event of "aa-1" has data.changed "planned_start,external_refs"
    When I patch the task "aa-1" with body "{'external_refs':[]}" as actor "claude@laptop"
    Then the response path "external_refs" has 0 items

  Scenario: a patch is found by upper-case ref and ULID
    Given I create a task in "aa" titled "one" as actor "claude@laptop"
    When I patch the task "AA-1" with body "{'title':'two'}" as actor "claude@laptop"
    Then the response path "title" is "two"
    When I request the task "aa-1" by its id as actor "claude@laptop"
    Then the response path "title" is "two"

  Scenario Outline: kind and initiative cannot be patched
    Given I create a task in "aa" titled "one" as actor "claude@laptop"
    And I note the counts of tasks, dependencies, events and actors
    When I patch the task "aa-1" with body "<body>" as actor "refused@laptop"
    Then the response status is 400
    And the problem detail contains "<field>"
    And the counts of tasks, dependencies, events and actors are unchanged
    And there is no actor row for "refused@laptop"

    Examples:
      | body                                  | field      |
      | {'kind':'watch'}                      | kind       |
      | {'kind':'work'}                       | kind       |
      | {'initiative':'bb'}                   | initiative |
      | {'initiative':'aa','title':'new'}     | initiative |

  Scenario Outline: a patch with a bad value is 400 and changes nothing
    Given I create a task in "aa" titled "one" as actor "claude@laptop"
    And I note the counts of tasks, dependencies, events and actors
    When I patch the task "aa-1" with body "<body>" as actor "refused@laptop"
    Then the response status is 400
    And the problem detail contains "<detail>"
    And the counts of tasks, dependencies, events and actors are unchanged
    And there is no actor row for "refused@laptop"

    Examples:
      | body                                                            | detail          |
      | {'title':''}                                                    | title           |
      | {'title':null}                                                  | title           |
      | {'title':'<<t*201>>'}                                           | title           |
      | {'description':'<<d*20001>>'}                                   | description     |
      | {'description':null}                                            | description     |
      | {'cadence_seconds':3600}                                        | cadence_seconds |
      | {'next_check_at':'2027-01-01T00:00:00Z'}                        | next_check_at   |
      | {'external_refs':null}                                          | external_refs   |
      | {'external_refs':[{'label':'l','url':'mailto:a@example.com'}]}  | url             |
      | {'planned_start':'2026-12-02T00:00:00Z','planned_end':'2026-12-01T00:00:00Z'} | planned_end |

  Scenario: a watch task keeps its cadence within the bounds when patched
    Given I create a watch task in "aa" titled "watch" with a cadence of 3600 seconds as actor "claude@laptop"
    When I patch the task "aa-1" with body "{'cadence_seconds':59}" as actor "claude@laptop"
    Then the response status is 400
    When I patch the task "aa-1" with body "{'cadence_seconds':null}" as actor "claude@laptop"
    Then the response status is 400
    When I patch the task "aa-1" with body "{'cadence_seconds':31536001}" as actor "claude@laptop"
    Then the response status is 400
    And exactly 0 task_updated events exist for the task "aa-1"

  Scenario: a patch that changes nothing is 200 and writes no event, row or actor
    Given I create a task in "aa" titled "one" as actor "claude@laptop"
    And I create a watch task in "aa" titled "watch" with a cadence of 3600 seconds as actor "claude@laptop"
    And I note the counts of tasks, dependencies, events and actors
    When I patch the task "aa-1" with body "{}" as actor "quiet@laptop"
    Then the response status is 200
    And the response path "title" is "one"
    When I patch the task "aa-1" with body "{'title':'one','description':'','external_refs':[]}" as actor "quiet@laptop"
    Then the response status is 200
    When I patch the task "aa-2" with body "{'cadence_seconds':3600}" as actor "quiet@laptop"
    Then the response status is 200
    And the counts of tasks, dependencies, events and actors are unchanged
    And exactly 0 task_updated events exist for the task "aa-1"
    And there is no actor row for "quiet@laptop"

  Scenario: a second identical patch writes no second event
    Given I create a task in "aa" titled "one" as actor "claude@laptop"
    When I patch the task "aa-1" with body "{'title':'two'}" as actor "claude@laptop"
    And I patch the task "aa-1" with body "{'title':'two'}" as actor "claude@laptop"
    Then the response status is 200
    And exactly 1 task_updated event exists for the task "aa-1"

  Scenario: a change records the principal and the actor of the patch
    Given I create a task in "aa" titled "one" as actor "claude@laptop"
    When I patch the task "aa-1" with body "{'title':'two'}" as actor "codex@laptop"
    Then the response status is 200
    And the actor "codex@laptop" has exactly one row in actors
    And the response path "created_by" is "claude@laptop"

  Scenario: patching a closed task is 409 and a missing one 404
    Given I create a task in "aa" titled "one" as actor "claude@laptop"
    And I close the task "aa-1" with body "{'resolution':'dropped'}" as actor "claude@laptop"
    And I note the counts of tasks, dependencies, events and actors
    When I patch the task "aa-1" with body "{'title':'two'}" as actor "refused@laptop"
    Then the response status is 409
    And the problem detail contains "aa-1"
    And the problem has a next field containing "reopen"
    And the counts of tasks, dependencies, events and actors are unchanged
    When I patch the task "aa-7" with body "{'title':'two'}" as actor "refused@laptop"
    Then the response status is 404
    And the problem has a next field containing "GET /api/v1/tasks"
