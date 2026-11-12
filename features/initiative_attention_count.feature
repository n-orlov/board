Feature: Attention count on initiatives
  Each item of GET /api/v1/initiatives and GET /api/v1/initiatives/{slug} carries attention_count, the number of the
  initiative's tasks whose attention list is not empty. A task counts once however many reasons it has, and a
  closed task counts for nothing.

  Background:
    Given a new database for the board commands
    When I run "board admin migrate"
    And I run "board admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port
    And I create the initiative "aa" titled "First" with tags "web" as actor "claude@laptop"
    And I create the initiative "bb" titled "Second" with no tags as actor "claude@laptop"
    And I create a task in "aa" titled "stranded" as actor "claude@laptop"
    And I create a task in "aa" titled "asking" as actor "claude@laptop"
    And I create a task in "aa" titled "finished" as actor "claude@laptop"
    And I create a task in "aa" titled "quiet" as actor "claude@laptop"
    And I create a task in "bb" titled "other" as actor "claude@laptop"

  Scenario: an initiative with nothing wrong counts none
    When I request the initiative "aa" as actor "operator@laptop"
    Then the response status is 200
    And the response path "attention_count" is "0"
    When I list the initiatives as actor "operator@laptop"
    Then the response path "items.0.slug" is "inbox"
    And the response path "items.0.attention_count" is "0"
    And the response path "items.1.attention_count" is "0"
    And the response path "items.2.attention_count" is "0"

  Scenario: stranded, asking and done tasks count and a quiet task does not, on the list and on the initiative
    Given I start a lease with body "{'task':'aa-1','intent':'migrate'}" as actor "claude@laptop"
    And the lease "L1" ran out 30 seconds ago
    And I raise an ask with body "{'task':'aa-2','question':'Which one?'}" as actor "claude@laptop"
    And I mark the task "aa-3" done as actor "codex@laptop"
    When I request the initiative "aa" as actor "operator@laptop"
    Then the response status is 200
    And the response path "attention_count" is "3"
    When I list the initiatives as actor "operator@laptop"
    Then the response path "items.0.slug" is "inbox"
    And the response path "items.0.attention_count" is "0"
    And the response path "items.1.slug" is "aa"
    And the response path "items.1.attention_count" is "3"
    And the response path "items.2.slug" is "bb"
    And the response path "items.2.attention_count" is "0"

  Scenario: closing the done task lowers the count to two
    Given I start a lease with body "{'task':'aa-1','intent':'migrate'}" as actor "claude@laptop"
    And the lease "L1" ran out 30 seconds ago
    And I raise an ask with body "{'task':'aa-2','question':'Which one?'}" as actor "claude@laptop"
    And I mark the task "aa-3" done as actor "codex@laptop"
    When I close the task "aa-3" with body "{'resolution':'completed'}" as actor "operator@laptop"
    And I request the initiative "aa" as actor "operator@laptop"
    Then the response path "attention_count" is "2"
    When I list the initiatives as actor "operator@laptop"
    Then the response path "items.1.attention_count" is "2"

  Scenario: a task with several reasons counts once
    Given I start a lease with body "{'task':'aa-1','intent':'migrate'}" as actor "claude@laptop"
    And the lease "L1" ran out 30 seconds ago
    And I raise an ask with body "{'task':'aa-1','question':'Which one?'}" as actor "claude@laptop"
    And I mark the task "aa-1" done as actor "codex@laptop"
    When I request the task "aa-1" as actor "operator@laptop"
    Then the response path "attention" has 3 items
    When I request the initiative "aa" as actor "operator@laptop"
    Then the response path "attention_count" is "1"
    When I list the initiatives as actor "operator@laptop"
    Then the response path "items.1.attention_count" is "1"

  Scenario: the other initiative is counted on its own
    Given I mark the task "bb-1" done as actor "codex@laptop"
    When I request the initiative "bb" as actor "operator@laptop"
    Then the response path "attention_count" is "1"
    When I request the initiative "aa" as actor "operator@laptop"
    Then the response path "attention_count" is "0"
    When I list the initiatives as actor "operator@laptop"
    Then the response path "items.1.attention_count" is "0"
    And the response path "items.2.attention_count" is "1"

  Scenario: a closed initiative counts none
    Given I mark the task "aa-1" done as actor "codex@laptop"
    When I close the initiative "aa" with body "{'force':true}" as actor "operator@laptop"
    And I request the initiative "aa" as actor "operator@laptop"
    Then the response path "attention_count" is "0"
