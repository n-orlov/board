Feature: Read cursors over HTTP
  An actor keeps one cursor per stream, notes and events, so it can ask for what is new since its last read
  without keeping state itself. PUT /api/v1/cursors/{stream} sets the caller's cursor to a note or an event
  (its ULID or its N<n> or E<n> ref). GET /api/v1/notes?since=cursor reads the notes after the caller's notes
  cursor, from the start when none is set, and ack=true moves that cursor to the last note returned. GET
  /api/v1/events?since=cursor follows the events cursor the same way. Cursors belong to the actor that sets them.

  Background:
    Given a new database for the board commands
    When I run "board admin migrate"
    And I run "board admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port
    And I create the initiative "aa" titled "First" with no tags as actor "claude@laptop"
    And I create the initiative "bb" titled "Second" with no tags as actor "claude@laptop"
    And I create a task in "aa" titled "one" as actor "claude@laptop"
    And I create a task in "aa" titled "two" as actor "claude@laptop"
    And I create a task in "bb" titled "other" as actor "claude@laptop"
    And I post a note with body "{'task':'aa-1','summary':'one'}" as actor "claude@laptop"
    And I post a note with body "{'initiative':'bb','summary':'two'}" as actor "codex@laptop"
    And I post a note with body "{'resource':'host:build-1','summary':'three'}" as actor "claude@laptop"
    And I post a note with body "{'summary':'four'}" as actor "codex@laptop"

  Rule: Setting a cursor

    Scenario Outline: the notes cursor takes a note id or an N ref in either case
      When I put the "notes" cursor with body "{'position':'<position>'}" as actor "reader@laptop"
      Then the response status is 200
      And the response path "stream" is "notes"
      And the response path "ref" is "N2"
      And the response path "position" is present
      And the "notes" cursor of "reader@laptop" holds the id of "N2"

      Examples:
        | position       |
        | {id of N2}     |
        | N2             |
        | n2             |

    Scenario Outline: the events cursor takes an event id or an E ref in either case
      When I put the "events" cursor with body "{'position':'<position>'}" as actor "reader@laptop"
      Then the response status is 200
      And the response path "stream" is "events"
      And the response path "ref" is "E7"
      And the "events" cursor of "reader@laptop" holds the id of "E7"

      Examples:
        | position       |
        | {id of E7}     |
        | E7             |
        | e7             |

    Scenario: setting a cursor again replaces its position and keeps one row per stream
      When I put the "notes" cursor with body "{'position':'N1'}" as actor "reader@laptop"
      And I put the "notes" cursor with body "{'position':'N3'}" as actor "reader@laptop"
      Then the response status is 200
      And the "notes" cursor of "reader@laptop" holds the id of "N3"
      And there are 1 cursors in the database

    Scenario: the two streams and two actors keep separate cursors
      When I put the "notes" cursor with body "{'position':'N1'}" as actor "reader@laptop"
      And I put the "events" cursor with body "{'position':'E3'}" as actor "reader@laptop"
      And I put the "notes" cursor with body "{'position':'N4'}" as actor "other@laptop"
      Then the "notes" cursor of "reader@laptop" holds the id of "N1"
      And the "events" cursor of "reader@laptop" holds the id of "E3"
      And the "notes" cursor of "other@laptop" holds the id of "N4"
      And the "events" cursor of "other@laptop" is not set
      And there are 3 cursors in the database

    Scenario Outline: a stream that is not notes or events is 404
      When I put the "<stream>" cursor with body "{'position':'N1'}" as actor "reader@laptop"
      Then the response status is 404
      And the problem detail contains "<stream>"
      And the response path "next" is present
      And there are 0 cursors in the database

      Examples:
        | stream  |
        | tasks   |
        | Notes   |
        | cursor  |

    Scenario Outline: a position that names no note or event is 400
      When I put the "<stream>" cursor with body "{'position':'<position>'}" as actor "reader@laptop"
      Then the response status is 400
      And the response path "next" is present
      And there are 0 cursors in the database

      Examples:
        | stream | position                   |
        | notes  | N99                        |
        | notes  | E1                         |
        | notes  |                            |
        | notes  | yesterday                  |
        | notes  | 01ARZ3NDEKTSV4RRFFQ69G5FAV |
        | events | E99                        |
        | events | N1                         |
        | events | 01ARZ3NDEKTSV4RRFFQ69G5FAV |
        | events | 2026-11-09T12:00:00Z       |

    Scenario: a note id is not an event position
      When I put the "events" cursor with body "{'position':'{id of N1}'}" as actor "reader@laptop"
      Then the response status is 400
      And the problem detail contains "names no event"

    Scenario: a refused position leaves the cursor where it was
      Given I put the "notes" cursor with body "{'position':'N2'}" as actor "reader@laptop"
      When I put the "notes" cursor with body "{'position':'N99'}" as actor "reader@laptop"
      Then the response status is 400
      And the "notes" cursor of "reader@laptop" holds the id of "N2"

    Scenario: a request with no body is 400
      When I put the "notes" cursor without a body as actor "reader@laptop"
      Then the response status is 400
      And the response path "next" is present

  Rule: since=cursor on the notes

    Scenario: with no cursor set since=cursor reads from the start and moves nothing
      When I list the notes with "since=cursor" as actor "reader@laptop"
      Then the response status is 200
      And the response path "items" lists the refs "N1,N2,N3,N4"
      And the "notes" cursor of "reader@laptop" is not set
      And there are 0 cursors in the database

    Scenario: ack=true then since=cursor returns only the newer notes
      When I list the notes with "limit=2&ack=true" as actor "reader@laptop"
      Then the response path "items" lists the refs "N1,N2"
      And the "notes" cursor of "reader@laptop" holds the id of "N2"
      When I list the notes with "since=cursor" as actor "reader@laptop"
      Then the response path "items" lists the refs "N3,N4"
      And the "notes" cursor of "reader@laptop" holds the id of "N2"
      When I list the notes with "since=cursor&ack=true" as actor "reader@laptop"
      Then the response path "items" lists the refs "N3,N4"
      And the "notes" cursor of "reader@laptop" holds the id of "N4"
      When I list the notes with "since=cursor" as actor "reader@laptop"
      Then the response status is 200
      And the response path "items" lists the refs ""
      When I post a note with body "{'summary':'five'}" as actor "claude@laptop"
      And I list the notes with "since=cursor&ack=true" as actor "reader@laptop"
      Then the response path "items" lists the refs "N5"
      And the "notes" cursor of "reader@laptop" holds the id of "N5"

    Scenario: an empty page leaves the cursor where it was
      Given I put the "notes" cursor with body "{'position':'N4'}" as actor "reader@laptop"
      When I list the notes with "since=cursor&ack=true" as actor "reader@laptop"
      Then the response path "items" lists the refs ""
      And the "notes" cursor of "reader@laptop" holds the id of "N4"

    Scenario: a cursor set with PUT is where since=cursor reads from
      Given I put the "notes" cursor with body "{'position':'N3'}" as actor "reader@laptop"
      When I list the notes with "since=cursor" as actor "reader@laptop"
      Then the response path "items" lists the refs "N4"
      When I list the notes with "since=cursor&author=claude@laptop" as actor "reader@laptop"
      Then the response path "items" lists the refs ""

    Scenario: ack=true with a filter moves the cursor to the last note returned
      When I list the notes with "author=claude@laptop&ack=true" as actor "reader@laptop"
      Then the response path "items" lists the refs "N1,N3"
      And the "notes" cursor of "reader@laptop" holds the id of "N3"

    Scenario: ack=false moves nothing
      When I list the notes with "ack=false" as actor "reader@laptop"
      Then the response path "items" lists the refs "N1,N2,N3,N4"
      And the "notes" cursor of "reader@laptop" is not set

    Scenario: another actor's cursor does not move this actor's reads
      Given I put the "notes" cursor with body "{'position':'N4'}" as actor "other@laptop"
      When I list the notes with "since=cursor" as actor "reader@laptop"
      Then the response path "items" lists the refs "N1,N2,N3,N4"
      When I list the notes with "since=cursor" as actor "other@laptop"
      Then the response path "items" lists the refs ""

    Scenario: ack=true with something other than a boolean is 400
      When I list the notes with "ack=maybe" as actor "reader@laptop"
      Then the response status is 400
      And there are 0 cursors in the database

  Rule: since=cursor on the events

    Scenario: with no cursor set since=cursor reads every event
      When I request the events list "since=cursor" as actor "reader@laptop"
      Then the response status is 200
      And the response items are the events "1,2,3,4,5,6,7,8,9,10"

    Scenario: since=cursor follows the events cursor
      Given I put the "events" cursor with body "{'position':'E8'}" as actor "reader@laptop"
      When I request the events list "since=cursor" as actor "reader@laptop"
      Then the response items are the events "9,10"
      When I put the "events" cursor with body "{'position':'{id of E10}'}" as actor "reader@laptop"
      And I request the events list "since=cursor" as actor "reader@laptop"
      Then the response items are the events ""
      When I post a note with body "{'summary':'five'}" as actor "claude@laptop"
      And I request the events list "since=cursor" as actor "reader@laptop"
      Then the response items are the events "11"

    Scenario: reading the events moves no cursor and the notes cursor is not the events cursor
      Given I put the "notes" cursor with body "{'position':'N4'}" as actor "reader@laptop"
      When I request the events list "since=cursor" as actor "reader@laptop"
      Then the response items are the events "1,2,3,4,5,6,7,8,9,10"
      And the "events" cursor of "reader@laptop" is not set

    Scenario: since=cursor combines with the other event filters
      Given I put the "events" cursor with body "{'position':'E4'}" as actor "reader@laptop"
      When I request the events list "since=cursor&type=note_posted" as actor "reader@laptop"
      Then the response items are the events "7,8,9,10"
