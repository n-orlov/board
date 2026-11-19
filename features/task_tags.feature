Feature: A task's own tags
  A task carries its own tags in task_tags (task_id, tag), under the rule of an initiative's tags: trimmed,
  lowercased, deduplicated, at most 20, each of a-z, 0-9, . _ : and - and starting with a letter or digit. POST
  /api/v1/tasks and POST /api/v1/start with new_task take them on create, and every read of a task shows them
  sorted, an empty list when it has none. A tag that breaks the rule is 400 and creates nothing. PUT and DELETE
  /api/v1/tasks/{ref}/tags/{tag} add and remove one tag, each change a task_updated event naming the tag and the
  change, and a change that changes nothing writes nothing. Each scenario
  runs on the leg BOARD_DB_LEG names.

  Background:
    Given a new database for the board commands
    When I run "vh admin migrate"
    And I run "vh admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port
    And I create the initiative "aa" titled "First" with no tags as actor "claude@laptop"
    And I create the initiative "bb" titled "Second" with no tags as actor "claude@laptop"

  Scenario: a task created with tags answers them normalised and sorted, and every read shows them
    When I create the task with body "{'initiative':'aa','title':'tagged','tags':['Work',' home ','work','a.b_c:d-e']}" as actor "claude@laptop"
    Then the response status is 201
    And the response path "ref" is "aa-1"
    And the response path "tags" has 3 items
    And the response path "tags.0" is "a.b_c:d-e"
    And the response path "tags.1" is "home"
    And the response path "tags.2" is "work"
    And the task_tags table holds 3 rows
    When I request the task "aa-1" as actor "codex@laptop"
    Then the response status is 200
    And the response path "tags" has 3 items
    And the response path "tags.0" is "a.b_c:d-e"
    And the response path "tags.2" is "work"
    When I list the tasks with "initiative=aa" as actor "codex@laptop"
    Then the response status is 200
    And the response path "items.0.ref" is "aa-1"
    And the response path "items.0.tags" has 3 items
    And the response path "items.0.tags.1" is "home"

  Scenario: a task created without tags has an empty list, and tags belong to their own task
    Given I create the task with body "{'initiative':'aa','title':'tagged','tags':['home']}" as actor "claude@laptop"
    When I create a task in "aa" titled "plain" as actor "claude@laptop"
    Then the response status is 201
    And the response path "tags" has 0 items
    And the task_tags table holds 1 rows
    When I list the tasks with "initiative=aa" as actor "codex@laptop"
    Then the response path "items.0.tags" has 1 items
    And the response path "items.1.ref" is "aa-2"
    And the response path "items.1.tags" has 0 items

  Scenario: a start that creates a task takes its tags, and the task reads them back
    When I start with body "{'new_task':{'initiative':'bb','title':'Fresh','tags':['Ops','ops','infra']},'intent':'doing it'}" as actor "claude@laptop"
    Then the response status is 201
    And the response path "task_created" is "true"
    And the response path "lease.task" is "bb-1"
    And the task_tags table holds 2 rows
    When I request the task "bb-1" as actor "codex@laptop"
    Then the response status is 200
    And the response path "tags" has 2 items
    And the response path "tags.0" is "infra"
    And the response path "tags.1" is "ops"

  Scenario Outline: a create with a bad tag is 400 and creates nothing
    Given I note the counts of tasks, dependencies, events and actors
    When I create the task with body "<body>" as actor "refused@laptop"
    Then the response status is 400
    And the response path "next" is present
    And the counts of tasks, dependencies, events and actors are unchanged
    And the task_tags table holds 0 rows
    When I create a task in "aa" titled "after" as actor "claude@laptop"
    Then the response path "ref" is "aa-1"

    Examples:
      | body                                                      |
      | {'initiative':'aa','title':'t','tags':['has space']}      |
      | {'initiative':'aa','title':'t','tags':['ok','-leading']}  |
      | {'initiative':'aa','title':'t','tags':['']}               |
      | {'initiative':'aa','title':'t','tags':['UPPER','bad!']}   |

  Scenario: a create with more than 20 tags is 400 and creates nothing
    Given I note the counts of tasks, dependencies, events and actors
    When I create the task with body "{'initiative':'aa','title':'t','tags':['t01','t02','t03','t04','t05','t06','t07','t08','t09','t10','t11','t12','t13','t14','t15','t16','t17','t18','t19','t20','t21']}" as actor "refused@laptop"
    Then the response status is 400
    And the counts of tasks, dependencies, events and actors are unchanged
    And the task_tags table holds 0 rows

  Scenario Outline: a start whose new task has a bad tag is 400 and writes no task, lease or event
    Given I note the counts of tasks, leases and events
    When I start with body "<body>" as actor "refused@laptop"
    Then the response status is 400
    And the response path "next" is present
    And the counts of tasks, leases and events changed by 0, 0 and 0
    And the task_tags table holds 0 rows
    And the actor "refused@laptop" has no row in actors

    Examples:
      | body                                                                              |
      | {'new_task':{'initiative':'bb','title':'t','tags':['has space']},'intent':'x'}    |
      | {'new_task':{'initiative':'bb','title':'t','tags':['ok','Bad!']},'intent':'x'}    |

  Scenario: adding a tag answers the task with the tag and writes one task_updated event naming it
    Given I create a task in "aa" titled "plain" as actor "claude@laptop"
    When I add the tag " Work " to the task "aa-1" as actor "codex@laptop"
    Then the response status is 200
    And the response path "ref" is "aa-1"
    And the response path "tags" has 1 items
    And the response path "tags.0" is "work"
    And the task_tags table holds 1 rows
    And exactly 1 task_updated event exists for the task "aa-1"
    And the latest task_updated event of "aa-1" names the tag "work" as "added"
    When I request the task "aa-1" as actor "claude@laptop"
    Then the response path "tags.0" is "work"
    When I add the tag "home" to the task "aa-1" as actor "codex@laptop"
    Then the response path "tags" has 2 items
    And the response path "tags.0" is "home"
    And the response path "tags.1" is "work"
    And exactly 2 task_updated events exist for the task "aa-1"
    And the latest task_updated event of "aa-1" names the tag "home" as "added"

  Scenario: removing a tag answers the task without it and writes one task_updated event naming it
    Given I create the task with body "{'initiative':'aa','title':'tagged','tags':['work','home']}" as actor "claude@laptop"
    When I remove the tag "Work" from the task "aa-1" as actor "codex@laptop"
    Then the response status is 200
    And the response path "tags" has 1 items
    And the response path "tags.0" is "home"
    And the task_tags table holds 1 rows
    And exactly 1 task_updated event exists for the task "aa-1"
    And the latest task_updated event of "aa-1" names the tag "work" as "removed"

  Scenario: adding a tag the task already has answers 200 and writes nothing
    Given I create the task with body "{'initiative':'aa','title':'tagged','tags':['work']}" as actor "claude@laptop"
    And I note the counts of tasks, dependencies, events and actors
    When I add the tag "WORK" to the task "aa-1" as actor "someone-else@laptop"
    Then the response status is 200
    And the response path "tags" has 1 items
    And the response path "tags.0" is "work"
    And exactly 0 task_updated events exist for the task "aa-1"
    And the counts of tasks, dependencies, events and actors are unchanged
    And the task_tags table holds 1 rows

  Scenario: removing a tag the task does not have answers 200 and writes nothing
    Given I create the task with body "{'initiative':'aa','title':'tagged','tags':['work']}" as actor "claude@laptop"
    And I note the counts of tasks, dependencies, events and actors
    When I remove the tag "home" from the task "aa-1" as actor "someone-else@laptop"
    Then the response status is 200
    And the response path "tags" has 1 items
    And exactly 0 task_updated events exist for the task "aa-1"
    And the counts of tasks, dependencies, events and actors are unchanged
    And the task_tags table holds 1 rows

  Scenario Outline: a tag that breaks the rule is 400 on add and on remove, and nothing changes
    Given I create the task with body "{'initiative':'aa','title':'tagged','tags':['work']}" as actor "claude@laptop"
    And I note the counts of tasks, dependencies, events and actors
    When I add the tag "<tag>" to the task "aa-1" as actor "refused@laptop"
    Then the response status is 400
    And the response path "next" is present
    And the counts of tasks, dependencies, events and actors are unchanged
    And the task_tags table holds 1 rows
    When I remove the tag "<tag>" from the task "aa-1" as actor "refused@laptop"
    Then the response status is 400
    And the counts of tasks, dependencies, events and actors are unchanged
    And the task_tags table holds 1 rows
    And exactly 0 task_updated events exist for the task "aa-1"

    Examples:
      | tag                                       |
      | has space                                 |
      | -leading                                  |
      | bad!                                      |
      | aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa |

  Scenario: a tag of a task that does not exist is 404 and a 21st tag is 400, each changing nothing
    Given I create the task with body "{'initiative':'aa','title':'tagged','tags':['t01','t02','t03','t04','t05','t06','t07','t08','t09','t10','t11','t12','t13','t14','t15','t16','t17','t18','t19','t20']}" as actor "claude@laptop"
    And I note the counts of tasks, dependencies, events and actors
    When I add the tag "work" to the task "aa-9" as actor "refused@laptop"
    Then the response status is 404
    When I add the tag "t21" to the task "aa-1" as actor "refused@laptop"
    Then the response status is 400
    And the counts of tasks, dependencies, events and actors are unchanged
    And the task_tags table holds 20 rows
