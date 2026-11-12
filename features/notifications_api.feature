Feature: Listing the alert outbox over HTTP
  GET /api/v1/notifications lists the rows of the notifications outbox, newest first by
  creation time and then id, so the operator can see what was delivered and what failed.
  state keeps the pending, sent or failed rows; with no state every row is listed. limit
  and after page the list, and next_after names the cursor of the following page. Each item
  is {id, kind, state, event, attempts, next_attempt_at?, last_error?, created_at, sent_at?,
  payload}. An unknown state, a limit out of range or an after that names no row is 400.

  Background:
    Given a new database for the board commands
    When I run "board admin migrate"
    And I run "board admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port
    And I create the initiative "aa" titled "First" with no tags as actor "claude@laptop"
    And I create a task in "aa" titled "one" as actor "claude@laptop"
    And I create a task in "aa" titled "two" as actor "claude@laptop"
    And I create a task in "aa" titled "three" as actor "claude@laptop"
    And I create a task in "aa" titled "four" as actor "claude@laptop"
    And I create a task in "aa" titled "five" as actor "claude@laptop"
    And I create a task in "aa" titled "six" as actor "claude@laptop"

  Scenario: an empty outbox lists no rows
    When I request the notifications list "" as actor "claude@laptop"
    Then the response status is 200
    And the response path "items" has 0 items
    And the response has no next_after

  Scenario Outline: the list is newest first, whole or by state
    Given the outbox holds these notifications:
      | n | kind     | state   | seconds | attempts | last_error    |
      | 1 | stranded | pending | 10      | 0        |               |
      | 2 | asking   | sent    | 20      | 1        |               |
      | 3 | done     | failed  | 30      | 6        | http 500      |
      | 4 | asking   | pending | 30      | 2        | timeout       |
      | 5 | done     | sent    | 40      | 3        | network error |
      | 6 | stranded | failed  | 50      | 6        | http 502      |
    When I request the notifications list "<query>" as actor "claude@laptop"
    Then the response status is 200
    And the response items are the notifications "<rows>"
    And the response has no next_after

    Examples:
      | query          | rows        |
      |                | 6,5,4,3,2,1 |
      | state=pending  | 4,1         |
      | state=sent     | 5,2         |
      | state=failed   | 6,3         |

  Scenario: an item carries its state, attempts, times, error and payload
    Given the outbox holds these notifications:
      | n | kind     | state   | seconds | attempts | last_error |
      | 1 | stranded | pending | 10      | 2        | timeout    |
      | 2 | asking   | sent    | 20      | 1        |            |
      | 3 | done     | failed  | 30      | 6        | http 500   |
    When I request the notifications list "" as actor "claude@laptop"
    Then item 1 of the response is a complete notification
    And the response path "items.2.kind" is "stranded"
    And the response path "items.2.state" is "pending"
    And the response path "items.2.attempts" is "2"
    And the response path "items.2.last_error" is "timeout"
    And the response path "items.2.next_attempt_at" is present
    And the response path "items.2.sent_at" is absent
    And the response path "items.2.payload.kind" is "stranded"
    And the response path "items.1.state" is "sent"
    And the response path "items.1.sent_at" is present
    And the response path "items.1.next_attempt_at" is absent
    And the response path "items.1.last_error" is absent
    And the response path "items.0.state" is "failed"
    And the response path "items.0.last_error" is "http 500"
    And the response path "items.0.attempts" is "6"
    And the response path "items.0.sent_at" is absent
    And the response path "items.0.next_attempt_at" is absent
    And the response path "items.0.payload.kind" is "done"
    And the response path "items.0.event" is the ref of the event of notification 3

  Scenario: limit and after page the list newest first
    Given the outbox holds these notifications:
      | n | kind     | state   | seconds | attempts | last_error |
      | 1 | stranded | pending | 10      | 0        |            |
      | 2 | asking   | sent    | 20      | 1        |            |
      | 3 | done     | failed  | 30      | 6        | http 500   |
      | 4 | asking   | pending | 40      | 0        |            |
      | 5 | done     | sent    | 50      | 1        |            |
    When I request the notifications list "limit=2" as actor "claude@laptop"
    Then the response items are the notifications "5,4"
    And the response next_after is the id of the notification 4
    When I request the next notifications page with "limit=2" as actor "claude@laptop"
    Then the response items are the notifications "3,2"
    And the response next_after is the id of the notification 2
    When I request the next notifications page with "limit=2" as actor "claude@laptop"
    Then the response items are the notifications "1"
    And the response has no next_after
    When I request the notifications list "limit=5" as actor "claude@laptop"
    Then the response items are the notifications "5,4,3,2,1"
    And the response has no next_after
    When I request the notifications list "state=sent&limit=1" as actor "claude@laptop"
    Then the response items are the notifications "5"
    When I request the next notifications page with "state=sent&limit=1" as actor "claude@laptop"
    Then the response items are the notifications "2"
    And the response has no next_after

  Scenario Outline: a request the list cannot read is 400 with a next
    Given the outbox holds these notifications:
      | n | kind     | state   | seconds | attempts | last_error |
      | 1 | stranded | pending | 10      | 0        |            |
    When I request the notifications list "<query>" as actor "claude@laptop"
    Then the response status is 400
    And the response path "next" is present

    Examples:
      | query                                  |
      | state=queued                           |
      | state=Sent                             |
      | state=                                 |
      | limit=0                                |
      | limit=1001                             |
      | limit=abc                              |
      | after=nope                             |
      | after=01JC0000000000000000000099       |
