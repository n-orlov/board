Feature: Initiatives over HTTP
  An authenticated caller creates, lists and reads initiatives through
  /api/v1/initiatives. A slug matches [a-z0-9-]{2,40} and is unique. A list
  takes tags, a limit and an after cursor (an id or a slug). Every creation
  appends an event that records the principal (the key) and the actor (the
  Board-Actor handle) in separate columns, and upserts the actor. Every error is
  a problem document with a next field.

  Background:
    Given a new database for the board commands
    When I run "vh admin migrate"
    And I run "vh admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port

  Scenario: create an initiative
    When I create the initiative "phone-onboard" titled "Onboard the new phone" with tags "Work, homelab" as actor "claude@laptop"
    Then the response status is 201
    And the response field "slug" is "phone-onboard"
    And the response field "ref" is "phone-onboard"
    And the response field "title" is "Onboard the new phone"
    And the response field "status" is "open"
    And the response field "tags" is "homelab,work"
    And the response field "created_by" is "claude@laptop"
    And the response has an "id" of 26 characters

  Scenario: a creation appends an event with the principal and the actor
    When I create the initiative "phone-onboard" titled "Onboard the new phone" with no tags as actor "claude@laptop"
    Then the response status is 201
    And the initiative_created event for "phone-onboard" has principal "key:workstation" and actor "claude@laptop"
    And the actor "claude@laptop" has exactly one row in actors
    When I create the initiative "second-one" titled "Another" with no tags as actor "claude@laptop"
    Then the response status is 201
    And the actor "claude@laptop" has exactly one row in actors

  Scenario: a write stores the actor's way back and meta headers, and a read stores nothing
    Given the next API request carries the header "Board-Actor-Way-Back" with value "tmux attach -t ws1"
    And the next API request carries the header "Board-Actor-Meta" with value "{'cwd':'/src'}"
    When I create the initiative "with-way-back" titled "Way back" with no tags as actor "claude@laptop"
    Then the response status is 201
    And the actor "claude@laptop" stores the way back "tmux attach -t ws1" and the meta "{'cwd':'/src'}"
    When I create the initiative "no-headers" titled "No headers" with no tags as actor "claude@laptop"
    Then the actor "claude@laptop" stores the way back "tmux attach -t ws1" and the meta "{'cwd':'/src'}"
    Given the next API request carries the header "Board-Actor-Way-Back" with value "somewhere"
    When I request the initiative "with-way-back" as actor "reader@laptop"
    Then there is no actor row for "reader@laptop"

  Scenario: get an initiative by slug and by id
    When I create the initiative "phone-onboard" titled "Onboard the new phone" with tags "work" as actor "claude@laptop"
    And I request the initiative "phone-onboard" as actor "claude@laptop"
    Then the response status is 200
    And the response field "title" is "Onboard the new phone"
    And the response field "tags" is "work"
    When I request the initiative "phone-onboard" by its id as actor "claude@laptop"
    Then the response status is 200
    And the response field "slug" is "phone-onboard"

  Scenario: an unknown initiative is 404 with a next field
    When I request the initiative "nothing-here" as actor "claude@laptop"
    Then the response status is 404
    And the problem has a next field containing "GET /api/v1/initiatives"

  Scenario: list initiatives filtered by tags
    When I create the initiative "aa-one" titled "One" with tags "work,x" as actor "claude@laptop"
    And I create the initiative "bb-two" titled "Two" with tags "work" as actor "claude@laptop"
    And I create the initiative "cc-three" titled "Three" with no tags as actor "claude@laptop"
    And I request the initiatives list "" as actor "claude@laptop"
    Then the response status is 200
    And the response lists the initiatives "inbox,aa-one,bb-two,cc-three"
    When I request the initiatives list "tag=work" as actor "claude@laptop"
    Then the response lists the initiatives "aa-one,bb-two"
    When I request the initiatives list "tag=work&tag=x" as actor "claude@laptop"
    Then the response lists the initiatives "aa-one"
    When I request the initiatives list "tag=absent" as actor "claude@laptop"
    Then the response lists the initiatives ""

  Scenario Outline: a slug outside [a-z0-9-]{2,40} is 400 with a next field
    When I create the initiative "<slug>" titled "Whatever" with no tags as actor "claude@laptop"
    Then the response status is 400
    And the problem has a next field containing "slug"
    And the problem detail contains "slug"
    And no initiative_created event exists besides the inbox's

    Examples:
      | slug                                      |
      | x                                         |
      | UPPER                                     |
      | under_score                               |
      | has space                                 |
      | aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa |

  Scenario: the longest and the shortest slugs are accepted
    When I create the initiative "ab" titled "Shortest" with no tags as actor "claude@laptop"
    Then the response status is 201
    When I create the initiative "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" titled "Longest" with no tags as actor "claude@laptop"
    Then the response status is 201

  Scenario: a duplicate slug is 409 with a next field and no second event
    When I create the initiative "phone-onboard" titled "First" with no tags as actor "claude@laptop"
    And I create the initiative "phone-onboard" titled "Second" with no tags as actor "claude@laptop"
    Then the response status is 409
    And the problem has a next field containing "GET /api/v1/initiatives/phone-onboard"
    And exactly 1 initiative_created event exists besides the inbox's
    When I request the initiative "phone-onboard" as actor "claude@laptop"
    Then the response field "title" is "First"

  Scenario: pagination ends exactly at the boundary
    When I create the initiative "init-1" titled "One" with no tags as actor "claude@laptop"
    And I create the initiative "init-2" titled "Two" with no tags as actor "claude@laptop"
    And I create the initiative "init-3" titled "Three" with no tags as actor "claude@laptop"
    And I request the initiatives list "limit=4" as actor "claude@laptop"
    Then the response lists the initiatives "inbox,init-1,init-2,init-3"
    And the response has no next_after
    When I request the initiatives list "limit=2" as actor "claude@laptop"
    Then the response lists the initiatives "inbox,init-1"
    And the response next_after is the id of "init-1"
    When I request the next page with "limit=2" as actor "claude@laptop"
    Then the response lists the initiatives "init-2,init-3"
    And the response has no next_after
    When I request the initiatives list "limit=1&after=init-1" as actor "claude@laptop"
    Then the response lists the initiatives "init-2"
    And the response next_after is the id of "init-2"
    When I request the initiatives list "after=init-3" as actor "claude@laptop"
    Then the response lists the initiatives ""
    And the response has no next_after

  Scenario: a slug whose text is also a ULID stays a usable reference
    When I create the initiative "01234567890123456789012345" titled "Digits" with no tags as actor "claude@laptop"
    Then the response status is 201
    And the response field "slug" is "01234567890123456789012345"
    When I create the initiative "01arz3ndektsv4rrffq69g5fav" titled "Lower" with no tags as actor "claude@laptop"
    Then the response status is 201
    And the response field "slug" is "01arz3ndektsv4rrffq69g5fav"
    When I create the initiative "after-both" titled "Last" with no tags as actor "claude@laptop"
    Then the response status is 201
    When I request the initiative "01234567890123456789012345" as actor "claude@laptop"
    Then the response status is 200
    And the response field "title" is "Digits"
    When I request the initiative "01arz3ndektsv4rrffq69g5fav" as actor "claude@laptop"
    Then the response status is 200
    And the response field "title" is "Lower"
    When I request the initiative "01ARZ3NDEKTSV4RRFFQ69G5FAV" as actor "claude@laptop"
    Then the response status is 200
    And the response field "title" is "Lower"
    When I request the initiative "01234567890123456789012345" by its id as actor "claude@laptop"
    Then the response status is 200
    And the response field "title" is "Digits"
    When I request the initiatives list "after=01234567890123456789012345" as actor "claude@laptop"
    Then the response lists the initiatives "01arz3ndektsv4rrffq69g5fav,after-both"
    When I request the initiatives list "after=01arz3ndektsv4rrffq69g5fav" as actor "claude@laptop"
    Then the response lists the initiatives "after-both"

  Scenario: references are case-insensitive on input
    When I create the initiative "phone-onboard" titled "Phone" with no tags as actor "claude@laptop"
    And I create the initiative "next-one" titled "Next" with no tags as actor "claude@laptop"
    And I request the initiative "Phone-Onboard" as actor "claude@laptop"
    Then the response status is 200
    And the response field "slug" is "phone-onboard"
    When I request the initiative "phone-onboard" by its lowercase id as actor "claude@laptop"
    Then the response status is 200
    And the response field "slug" is "phone-onboard"
    When I request the initiatives list "after=PHONE-ONBOARD" as actor "claude@laptop"
    Then the response lists the initiatives "next-one"
    When I request the initiatives list "after=Phone-Onboard&limit=1" as actor "claude@laptop"
    Then the response lists the initiatives "next-one"

  Scenario Outline: a bad limit or cursor is 400 with a next field
    When I request the initiatives list "<query>" as actor "claude@laptop"
    Then the response status is 400
    And the problem has a next field containing "<next>"

    Examples:
      | query           | next        |
      | limit=0         | limit       |
      | limit=1001      | limit       |
      | limit=many      | Fix the     |
      | after=Not_A_Ref | after       |
      | after=no-such   | after       |
      | tag=no%20way    | tags        |

  Scenario: a request with a valid key and no actor is 400
    When I request GET "/api/v1/initiatives" with the remembered key and no Board-Actor header
    Then the response status is 400
