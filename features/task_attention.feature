Feature: Attention reasons on tasks
  A task document and each item of the task list say why the task needs a person: attention lists the reasons in
  the order stranded, asking, done, and is empty when there is none. A stranded task also carries the lease behind
  it as stranded_lease. GET /api/v1/tasks takes attention=true or false, which keeps the tasks with a non-empty or
  an empty list, together with the other filters, in id order.

  Background:
    Given a new database for the board commands
    When I run "vh admin migrate"
    And I run "vh admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port
    And I create the initiative "aa" titled "First" with tags "web" as actor "claude@laptop"
    And I create the initiative "bb" titled "Second" with no tags as actor "claude@laptop"
    And I create a task in "aa" titled "one" as actor "claude@laptop"
    And I create a task in "aa" titled "two" as actor "claude@laptop"
    And I create a task in "aa" titled "three" as actor "claude@laptop"
    And I create a task in "bb" titled "other" as actor "claude@laptop"

  Scenario: a task with nothing wrong has an empty list and no stranded lease
    When I request the task "aa-1" as actor "operator@laptop"
    Then the response status is 200
    And the response path "attention" has 0 items
    And the response path "stranded_lease" is absent

  Scenario: an expired lease nobody acknowledged strands the task and the document carries the lease
    Given the next API request carries the header "Board-Actor-Way-Back" with value "ssh build1"
    And I start a lease with body "{'task':'aa-1','intent':'migrate'}" as actor "claude@laptop"
    And the lease "L1" ran out 30 seconds ago
    When I request the task "aa-1" as actor "operator@laptop"
    Then the response status is 200
    And the response path "attention" has 1 items
    And the response path "attention.0.reason" is "stranded"
    And the response path "stranded_lease.ref" is "L1"
    And the response path "stranded_lease.holder.handle" is "claude@laptop"
    And the response path "stranded_lease.holder.way_back" is "ssh build1"
    And the response path "stranded_lease.intent" is "migrate"
    And the response path "stranded_lease.renewed_at" is present
    And the response path "stranded_lease.expired_at" is present
    When I list the tasks as actor "operator@laptop"
    Then the response path "items.0.ref" is "aa-1"
    And the response path "items.0.attention.0.reason" is "stranded"
    And the response path "items.0.stranded_lease.ref" is "L1"
    And the response path "items.1.attention" has 0 items
    And the response path "items.1.stranded_lease" is absent

  Scenario: acknowledging the lease clears the stranded reason
    Given I start a lease with body "{'task':'aa-1','intent':'migrate'}" as actor "claude@laptop"
    And the lease "L1" ran out 30 seconds ago
    And I request the task "aa-1" as actor "operator@laptop"
    And the response path "attention.0.reason" is "stranded"
    When I acknowledge the lease "L1" as actor "operator@laptop"
    And I request the task "aa-1" as actor "operator@laptop"
    Then the response path "attention" has 0 items
    And the response path "stranded_lease" is absent

  Scenario: a later lease clears the stranded reason
    Given I start a lease with body "{'task':'aa-1','intent':'migrate'}" as actor "claude@laptop"
    And the lease "L1" ran out 30 seconds ago
    And I request the task "aa-1" as actor "operator@laptop"
    And the response path "attention.0.reason" is "stranded"
    When I start a lease with body "{'task':'aa-1','intent':'finish the migration'}" as actor "codex@laptop"
    And I request the task "aa-1" as actor "operator@laptop"
    Then the response path "attention" has 0 items
    And the response path "stranded_lease" is absent

  Scenario: a non-blocking ask is asking without blocking
    Given I raise an ask with body "{'task':'aa-1','question':'Which one?'}" as actor "claude@laptop"
    When I request the task "aa-1" as actor "operator@laptop"
    Then the response path "attention" has 1 items
    And the response path "attention.0.reason" is "asking"
    And the response path "attention.0.blocking" is absent
    And the response path "stranded_lease" is absent

  Scenario: a blocking ask sets blocking
    Given I raise an ask with body "{'task':'aa-1','question':'Which one?','blocking':true}" as actor "claude@laptop"
    When I request the task "aa-1" as actor "operator@laptop"
    Then the response path "attention.0.reason" is "asking"
    And the response path "attention.0.blocking" is "true"

  Scenario: answering the last open ask clears the reason
    Given I raise an ask with body "{'task':'aa-1','question':'First?'}" as actor "claude@laptop"
    And I raise an ask with body "{'task':'aa-1','question':'Second?','blocking':true}" as actor "claude@laptop"
    When I answer the ask "A1" with body "{'answer':'yes'}" as actor "operator@laptop"
    And I request the task "aa-1" as actor "operator@laptop"
    Then the response path "attention.0.reason" is "asking"
    And the response path "attention.0.blocking" is "true"
    When I answer the ask "A2" with body "{'answer':'no'}" as actor "operator@laptop"
    And I request the task "aa-1" as actor "operator@laptop"
    Then the response path "attention" has 0 items

  Scenario: withdrawing the last open ask clears the reason
    Given I raise an ask with body "{'task':'aa-1','question':'Which one?','blocking':true}" as actor "claude@laptop"
    And I request the task "aa-1" as actor "operator@laptop"
    And the response path "attention.0.reason" is "asking"
    When I withdraw the ask "A1" with body "{'reason':'found it'}" as actor "claude@laptop"
    And I request the task "aa-1" as actor "operator@laptop"
    Then the response path "attention" has 0 items

  Scenario: a done task is done
    Given I mark the task "aa-1" done as actor "codex@laptop"
    When I request the task "aa-1" as actor "operator@laptop"
    Then the response path "attention" has 1 items
    And the response path "attention.0.reason" is "done"
    And the response path "attention.0.blocking" is absent

  Scenario: closing a done task clears the reason
    Given I mark the task "aa-1" done as actor "codex@laptop"
    When I close the task "aa-1" with body "{'resolution':'completed'}" as actor "operator@laptop"
    And I request the task "aa-1" as actor "operator@laptop"
    Then the response path "attention" has 0 items

  Scenario: reopening a done task clears the reason
    Given I mark the task "aa-1" done as actor "codex@laptop"
    When I reopen the task "aa-1" as actor "operator@laptop"
    And I request the task "aa-1" as actor "operator@laptop"
    Then the response path "attention" has 0 items

  Scenario: a task with all three reasons lists them stranded, asking, done
    Given I start a lease with body "{'task':'aa-1','intent':'migrate'}" as actor "claude@laptop"
    And the lease "L1" ran out 30 seconds ago
    And I raise an ask with body "{'task':'aa-1','question':'Which one?','blocking':true}" as actor "claude@laptop"
    And I mark the task "aa-1" done as actor "codex@laptop"
    When I request the task "aa-1" as actor "operator@laptop"
    Then the response path "attention" has 3 items
    And the response path "attention.0.reason" is "stranded"
    And the response path "attention.1.reason" is "asking"
    And the response path "attention.1.blocking" is "true"
    And the response path "attention.2.reason" is "done"
    And the response path "stranded_lease.ref" is "L1"

  Scenario: a closed task with an expired lease has no reason
    Given I start a lease with body "{'task':'aa-1','intent':'migrate'}" as actor "claude@laptop"
    And the lease "L1" ran out 30 seconds ago
    When I close the task "aa-1" with body "{'resolution':'dropped'}" as actor "operator@laptop"
    Then the response status is 200
    When I request the task "aa-1" as actor "operator@laptop"
    Then the response path "status" is "closed"
    And the response path "attention" has 0 items
    And the response path "stranded_lease" is absent
