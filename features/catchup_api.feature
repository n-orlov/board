Feature: Catching up in one request, POST /api/v1/catchup
  POST /api/v1/catchup answers the notes after the caller's notes cursor, and acknowledges them by moving the cursor to
  the last one, plus the answers to the caller's asks given since its previous catch-up. A catch-up that returns
  something appends one catchup event in the same transaction as the cursor move; one that returns nothing writes
  nothing, and a second call straight after a catch-up returns nothing.

  Background:
    Given a new database for the board commands
    When I run "vh admin migrate"
    And I run "vh admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port
    And I create the initiative "aa" titled "First" with no tags as actor "claude@laptop"
    And I create a task in "aa" titled "one" as actor "claude@laptop"

  Scenario: nothing new answers two empty lists and writes nothing
    Given I note the counts of events and cursors
    When I catch up as actor "claude@laptop"
    Then the response status is 200
    And the response path "notes" has 0 items
    And the response path "answers" has 0 items
    And the response path "notes_more" is "0"
    And the counts of events and cursors changed by 0 and 0
    And exactly 0 catchup events exist for the actor "claude@laptop"
    And the "notes" cursor of "claude@laptop" is not set

  Scenario: notes only are returned in order and acknowledged
    Given I post a note with body "{'task':'aa-1','summary':'found the cause','body':'the cache'}" as actor "codex@laptop"
    And I post a note with body "{'summary':'registry is slow'}" as actor "codex@laptop"
    And I note the counts of events and cursors
    When I catch up as actor "claude@laptop"
    Then the response status is 200
    And the response path "notes" has 2 items
    And the response path "notes.0.ref" is "N1"
    And the response path "notes.0.summary" is "found the cause"
    And the response path "notes.0.has_body" is "true"
    And the response path "notes.0.body" is absent
    And the response path "notes.1.ref" is "N2"
    And the response path "answers" has 0 items
    And the response path "notes_more" is "0"
    And the counts of events and cursors changed by 1 and 1
    And exactly 1 catchup event exists for the actor "claude@laptop"
    And the newest catchup event of "claude@laptop" says "catch-up: 2 notes, 0 answers" with 2 notes and 0 answers
    And the "notes" cursor of "claude@laptop" holds the id of "N2"

  Scenario: answers only are returned and leave the notes cursor alone
    Given I raise an ask with body "{'task':'aa-1','question':'Which database?'}" as actor "claude@laptop"
    And I raise an ask with body "{'task':'aa-1','question':'Which port?'}" as actor "codex@laptop"
    And I answer the ask "A1" with body "{'answer':'use sqlite'}" as actor "nik@phone"
    And I answer the ask "A2" with body "{'answer':'8080'}" as actor "nik@phone"
    And I note the counts of events and cursors
    When I catch up as actor "claude@laptop"
    Then the response status is 200
    And the response path "notes" has 0 items
    And the response path "answers" has 1 items
    And the response path "answers.0.ref" is "A1"
    And the response path "answers.0.state" is "answered"
    And the response path "answers.0.answer" is "use sqlite"
    And the response path "answers.0.answered_by" is "nik@phone"
    And the counts of events and cursors changed by 1 and 0
    And exactly 1 catchup event exists for the actor "claude@laptop"
    And the newest catchup event of "claude@laptop" says "catch-up: 0 notes, 1 answers" with 0 notes and 1 answers
    And the "notes" cursor of "claude@laptop" is not set

  Scenario: both are returned in one transaction with one event
    Given I post a note with body "{'summary':'registry is slow'}" as actor "codex@laptop"
    And I raise an ask with body "{'task':'aa-1','question':'Which database?'}" as actor "claude@laptop"
    And I answer the ask "A1" with body "{'answer':'use sqlite'}" as actor "nik@phone"
    And I note the counts of events and cursors
    When I catch up as actor "claude@laptop"
    Then the response status is 200
    And the response path "notes" has 1 items
    And the response path "notes.0.ref" is "N1"
    And the response path "answers" has 1 items
    And the response path "answers.0.ref" is "A1"
    And the counts of events and cursors changed by 1 and 1
    And exactly 1 catchup event exists for the actor "claude@laptop"
    And the newest catchup event of "claude@laptop" says "catch-up: 1 notes, 1 answers" with 1 notes and 1 answers
    And the "notes" cursor of "claude@laptop" holds the id of "N1"

  Scenario: a second call straight after returns nothing and writes nothing
    Given I post a note with body "{'summary':'registry is slow'}" as actor "codex@laptop"
    And I raise an ask with body "{'task':'aa-1','question':'Which database?'}" as actor "claude@laptop"
    And I answer the ask "A1" with body "{'answer':'use sqlite'}" as actor "nik@phone"
    And I catch up as actor "claude@laptop"
    And I note the counts of events and cursors
    When I catch up as actor "claude@laptop"
    Then the response status is 200
    And the response path "notes" has 0 items
    And the response path "answers" has 0 items
    And the counts of events and cursors changed by 0 and 0
    And exactly 1 catchup event exists for the actor "claude@laptop"

  Scenario: what arrives after a catch-up is returned by the next one only
    Given I post a note with body "{'summary':'first'}" as actor "codex@laptop"
    And I raise an ask with body "{'task':'aa-1','question':'Which database?'}" as actor "claude@laptop"
    And I answer the ask "A1" with body "{'answer':'use sqlite'}" as actor "nik@phone"
    And I catch up as actor "claude@laptop"
    And I post a note with body "{'summary':'second'}" as actor "codex@laptop"
    And I raise an ask with body "{'task':'aa-1','question':'Which port?'}" as actor "claude@laptop"
    And I answer the ask "A2" with body "{'answer':'8080'}" as actor "nik@phone"
    When I catch up as actor "claude@laptop"
    Then the response status is 200
    And the response path "notes" has 1 items
    And the response path "notes.0.ref" is "N2"
    And the response path "answers" has 1 items
    And the response path "answers.0.ref" is "A2"
    And exactly 2 catchup events exist for the actor "claude@laptop"

  Scenario: each actor has its own cursor and its own previous catch-up
    Given I post a note with body "{'summary':'registry is slow'}" as actor "codex@laptop"
    And I catch up as actor "claude@laptop"
    When I catch up as actor "nik@phone"
    Then the response status is 200
    And the response path "notes" has 1 items
    And the response path "notes.0.ref" is "N1"
    And the "notes" cursor of "nik@phone" holds the id of "N1"
    And exactly 1 catchup event exists for the actor "nik@phone"
