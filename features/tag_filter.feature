Feature: Tag filters match a task's own, initiative and derived tags
  On GET /api/v1/tasks (the Archive list is status=closed), GET /api/v1/timeline and GET /api/v1/attention the
  repeatable tag filter keeps a task that matches every tag given (AND), and the repeatable initiative filter
  keeps a task in any initiative given (OR). A task matches a tag when it is one of its own tags, one of its
  initiative's tags or a derived actor-kind tag. A single value means what it meant when only initiative tags
  counted. Each scenario runs on the leg BOARD_DB_LEG names.

  Background:
    Given a new database for the board commands
    When I run "vh admin migrate"
    And I run "vh admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port
    And I create the initiative "aa" titled "First" with tags "web,team" as actor "claude@laptop"
    And I create the initiative "bb" titled "Second" with tags "web" as actor "claude@laptop"
    And I create the initiative "cc" titled "Third" with no tags as actor "claude@laptop"
    And I create the task with body "{'initiative':'aa','title':'one','tags':['work','urgent']}" as actor "claude@laptop"
    And I create the task with body "{'initiative':'aa','title':'two','tags':['work']}" as actor "claude@laptop"
    And I create the task with body "{'initiative':'bb','title':'three','tags':['urgent']}" as actor "claude@laptop"
    And I create the task with body "{'initiative':'cc','title':'four'}" as actor "claude@laptop"
    And I start a lease with body "{'task':'bb-1','intent':'loop'}" as actor "ralph:board-7"

  Scenario: a single tag keeps meaning an initiative tag, and now also a task's own
    When I list the tasks with "tag=team" as actor "operator@laptop"
    Then the response path "items" lists the refs "aa-1,aa-2"
    When I list the tasks with "tag=web" as actor "operator@laptop"
    Then the response path "items" lists the refs "aa-1,aa-2,bb-1"
    When I list the tasks with "tag=urgent" as actor "operator@laptop"
    Then the response path "items" lists the refs "aa-1,bb-1"

  Scenario: two tags on the task list must both match, each from any source
    When I list the tasks with "tag=work&tag=urgent" as actor "operator@laptop"
    Then the response path "items" lists the refs "aa-1"
    When I list the tasks with "tag=web&tag=urgent" as actor "operator@laptop"
    Then the response path "items" lists the refs "aa-1,bb-1"
    When I list the tasks with "tag=team&tag=urgent" as actor "operator@laptop"
    Then the response path "items" lists the refs "aa-1"
    When I list the tasks with "tag=work&tag=nothing" as actor "operator@laptop"
    Then the response path "items" has 0 items

  Scenario: two initiatives keep a task in either of them
    When I list the tasks with "initiative=aa&initiative=cc" as actor "operator@laptop"
    Then the response path "items" lists the refs "aa-1,aa-2,cc-1"
    When I list the tasks with "initiative=aa&initiative=bb&tag=urgent" as actor "operator@laptop"
    Then the response path "items" lists the refs "aa-1,bb-1"

  Scenario: a task is matched by a derived actor-kind tag
    When I list the tasks with "tag=ralph" as actor "operator@laptop"
    Then the response path "items" lists the refs "bb-1"
    When I list the tasks with "tag=ralph&tag=urgent" as actor "operator@laptop"
    Then the response path "items" lists the refs "bb-1"
    When I list the tasks with "tag=ralph&tag=work" as actor "operator@laptop"
    Then the response path "items" has 0 items
    When I list the tasks with "tag=claude" as actor "operator@laptop"
    Then the response path "items" has 0 items

  Scenario: the Archive list filters closed tasks by two tags
    Given I close the task "aa-1" with body "{'resolution':'dropped'}" as actor "claude@laptop"
    And I close the task "aa-2" with body "{'resolution':'dropped'}" as actor "claude@laptop"
    And I close the task "bb-1" with body "{'resolution':'dropped'}" as actor "claude@laptop"
    When I list the tasks with "status=closed&tag=work&tag=web" as actor "operator@laptop"
    Then the response path "items" lists the refs "aa-1,aa-2"
    When I list the tasks with "status=closed&tag=work&tag=urgent" as actor "operator@laptop"
    Then the response path "items" lists the refs "aa-1"
    When I list the tasks with "status=closed&tag=ralph&tag=urgent" as actor "operator@laptop"
    Then the response path "items" lists the refs "bb-1"
    When I list the tasks with "status=closed&initiative=bb&initiative=cc&tag=urgent" as actor "operator@laptop"
    Then the response path "items" lists the refs "bb-1"

  Scenario: the timeline matches the same way
    When I request the timeline with "rows=all&tag=work&tag=urgent" as actor "operator@laptop"
    Then the response status is 200
    And the timeline tasks have ref "aa-1"
    When I request the timeline with "rows=all&tag=web&tag=urgent" as actor "operator@laptop"
    Then the timeline tasks have ref "aa-1,bb-1"
    When I request the timeline with "rows=all&tag=ralph" as actor "operator@laptop"
    Then the timeline tasks have ref "bb-1"
    When I request the timeline with "rows=all&initiative=aa&initiative=cc&tag=work" as actor "operator@laptop"
    Then the timeline tasks have ref "aa-1,aa-2"

  Scenario: attention matches a task's own, initiative and derived tags
    Given I raise an ask with body "{'task':'aa-1','question':'Which?','mode':'poll'}" as actor "claude@laptop"
    And I raise an ask with body "{'task':'bb-1','question':'Where?','mode':'poll'}" as actor "claude@laptop"
    When I request the attention with "tag=urgent" as actor "operator@laptop"
    Then the attention counts are "2,0,0,0,0,2"
    When I request the attention with "tag=work&tag=urgent" as actor "operator@laptop"
    Then the attention counts are "1,0,0,0,0,1"
    And the response path "open_asks.0.task" is "aa-1"
    When I request the attention with "tag=team" as actor "operator@laptop"
    Then the attention counts are "1,0,0,0,0,1"
    When I request the attention with "tag=ralph" as actor "operator@laptop"
    Then the attention counts are "1,0,0,0,0,1"
    And the response path "open_asks.0.task" is "bb-1"
    When I request the attention with "initiative=aa&initiative=bb&tag=urgent" as actor "operator@laptop"
    Then the attention counts are "2,0,0,0,0,2"
