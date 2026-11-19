Feature: Derived actor-kind tags
  A task carries a derived tag for each of ralph, claude, pi, codex and deck that is the handle prefix, before the
  first colon, of any holder of a live or ended lease on it. The tags are computed when the task is read, never
  stored as task tags, and sit in tag_marks beside the task's own and its initiative's tags, each marked with its
  source: task, initiative or actor. Each scenario runs on the leg BOARD_DB_LEG names.

  Background:
    Given a new database for the board commands
    When I run "vh admin migrate"
    And I run "vh admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port
    And I create the initiative "aa" titled "First" with no tags as actor "claude@laptop"
    And I create the task with body "{'initiative':'aa','title':'one','tags':['work']}" as actor "claude@laptop"

  Scenario: a task nobody leased has only the tags it owns
    When I request the task "aa-1" as actor "codex@laptop"
    Then the response status is 200
    And the response path "tag_marks" has 1 items
    And the response path "tag_marks.0.tag" is "work"
    And the response path "tag_marks.0.source" is "task"

  Scenario: a lease held by a ralph: handle adds the ralph tag with source actor
    Given I start a lease with body "{'task':'aa-1','intent':'loop'}" as actor "ralph:board-7"
    When I request the task "aa-1" as actor "codex@laptop"
    Then the response status is 200
    And the response path "tag_marks" has 2 items
    And the response path "tag_marks.1.tag" is "ralph"
    And the response path "tag_marks.1.source" is "actor"
    And the response path "tags" has 1 items
    And the response path "tags.0" is "work"
    And the task_tags table holds 1 rows
    When I list the tasks with "initiative=aa" as actor "codex@laptop"
    Then the response path "items.0.tag_marks.1.tag" is "ralph"
    And the response path "items.0.tag_marks.1.source" is "actor"

  Scenario: a handle with an unknown prefix or no colon adds nothing
    Given I start a lease with body "{'task':'aa-1','intent':'x'}" as actor "foo:x"
    And I start a lease with body "{'task':'aa-1','intent':'y'}" as actor "ralph"
    When I request the task "aa-1" as actor "codex@laptop"
    Then the response status is 200
    And the response path "tag_marks" has 1 items
    And the response path "tag_marks.0.source" is "task"

  Scenario: an ended lease still counts
    Given I start a lease with body "{'task':'aa-1','intent':'x'}" as actor "codex:a:b"
    And I end the lease "L1" without a body as actor "codex:a:b"
    When I request the task "aa-1" as actor "claude@laptop"
    Then the response status is 200
    And the response path "tag_marks" has 2 items
    And the response path "tag_marks.1.tag" is "codex"
    And the response path "tag_marks.1.source" is "actor"

  Scenario: a tag with two sources is marked once for each, and the sources keep their order
    Given I create the initiative "bb" titled "Second" with tags "pi,work" as actor "claude@laptop"
    And I create a task in "bb" titled "two" as actor "claude@laptop"
    And I start a lease with body "{'task':'bb-1','intent':'x'}" as actor "pi:session"
    When I request the task "bb-1" as actor "claude@laptop"
    Then the response status is 200
    And the response path "tag_marks" has 3 items
    And the response path "tag_marks.0.tag" is "pi"
    And the response path "tag_marks.0.source" is "initiative"
    And the response path "tag_marks.1.tag" is "work"
    And the response path "tag_marks.1.source" is "initiative"
    And the response path "tag_marks.2.tag" is "pi"
    And the response path "tag_marks.2.source" is "actor"
