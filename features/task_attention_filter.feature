Feature: The attention filter of the task list
  GET /api/v1/tasks?attention=true keeps the tasks that need a person (a non-empty attention list) and
  attention=false the others. The filter is ANDed with the other filters and the page keeps id order.

  Background:
    Given a new database for the board commands
    When I run "board admin migrate"
    And I run "board admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port
    And I create the initiative "aa" titled "First" with tags "web" as actor "claude@laptop"
    And I create the initiative "bb" titled "Second" with no tags as actor "claude@laptop"
    And I create a task in "aa" titled "one" as actor "claude@laptop"
    And I create a task in "aa" titled "two" as actor "claude@laptop"
    And I create a task in "aa" titled "three" as actor "claude@laptop"
    And I create a task in "bb" titled "other" as actor "claude@laptop"
    And I create a task in "bb" titled "another" as actor "claude@laptop"
    And I raise an ask with body "{'task':'aa-1','question':'Which one?'}" as actor "claude@laptop"
    And I mark the task "aa-3" done as actor "codex@laptop"
    And I mark the task "bb-1" done as actor "codex@laptop"
    And I start a lease with body "{'task':'aa-2','intent':'work'}" as actor "claude@laptop"

  Scenario: attention=true keeps the tasks that need a person in id order
    When I list the tasks with "attention=true" as actor "operator@laptop"
    Then the response status is 200
    And the response path "items" lists the refs "aa-1,aa-3,bb-1"

  Scenario: attention=false keeps the others
    When I list the tasks with "attention=false" as actor "operator@laptop"
    Then the response status is 200
    And the response path "items" lists the refs "aa-2,bb-2"

  Scenario: attention=true with an initiative
    When I list the tasks with "attention=true&initiative=bb" as actor "operator@laptop"
    Then the response path "items" lists the refs "bb-1"
    When I list the tasks with "attention=false&initiative=aa" as actor "operator@laptop"
    Then the response path "items" lists the refs "aa-2"

  Scenario: attention=true with status=done
    When I list the tasks with "attention=true&status=done" as actor "operator@laptop"
    Then the response path "items" lists the refs "aa-3,bb-1"
    When I list the tasks with "attention=false&status=done" as actor "operator@laptop"
    Then the response path "items" has 0 items

  Scenario: attention=true with active=true
    Given the lease "L1" ran out 30 seconds ago
    And I start a lease with body "{'task':'aa-1','intent':'look into it'}" as actor "claude@laptop"
    When I list the tasks with "attention=true&active=true" as actor "operator@laptop"
    Then the response path "items" lists the refs "aa-1"
    When I list the tasks with "attention=false&active=true" as actor "operator@laptop"
    Then the response path "items" has 0 items

  Scenario: attention=true pages in id order
    When I list the tasks with "attention=true&limit=2" as actor "operator@laptop"
    Then the response path "items" lists the refs "aa-1,aa-3"
    When I request the next tasks page with "attention=true&limit=2" as actor "operator@laptop"
    Then the response path "items" lists the refs "bb-1"

  Scenario: a value other than true or false is refused
    When I list the tasks with "attention=maybe" as actor "operator@laptop"
    Then the response status is 400
