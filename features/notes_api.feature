Feature: Notes over HTTP
  A note is a finding an actor posts on a task, on an initiative, on a resource or on the board as a whole.
  POST /api/v1/notes posts it (201) and every note, broadcasts included, writes a note_posted event. GET
  /api/v1/notes lists notes, newest last, with summaries by default, and GET /api/v1/notes/{ref} reads one with
  its body. A refused request changes nothing: no note, event or actor row.

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

  Rule: Posting a note

    Scenario: a note on a task derives the initiative and writes note_posted
      When I post a note with body "{'task':'aa-1','summary':'found the cause','body':'the cache'}" as actor "claude@laptop"
      Then the response status is 201
      And the response path "ref" is "N1"
      And the response path "task" is "aa-1"
      And the response path "initiative" is "aa"
      And the response path "author" is "claude@laptop"
      And the response path "summary" is "found the cause"
      And the response path "body" is "the cache"
      And the response path "has_body" is "true"
      And the response path "broadcast" is "false"
      And the response path "mentions" has 0 items
      And the response path "resource" is absent
      And the response path "created_at" is present
      And exactly 1 note_posted event exists for the note "N1"
      And the note_posted event of the note "N1" is on the task "aa-1" and the initiative "aa"
      And the note_posted event of the note "N1" has principal "key:workstation" and actor "claude@laptop"
      And the note_posted event of the note "N1" says "note N1 on aa-1: found the cause"

    Scenario: a note on an initiative has no task
      When I post a note with body "{'initiative':'bb','summary':'plan changed'}" as actor "codex@laptop"
      Then the response status is 201
      And the response path "initiative" is "bb"
      And the response path "task" is absent
      And the response path "has_body" is "false"
      And the response path "body" is absent
      And the response path "broadcast" is "false"
      And exactly 1 note_posted event exists for the note "N1"
      And the note_posted event of the note "N1" is on the task "" and the initiative "bb"
      And the note_posted event of the note "N1" says "note N1 on bb: plan changed"

    Scenario: a note on a resource alone is a broadcast
      When I post a note with body "{'resource':'host:build-1','summary':'build host is slow'}" as actor "claude@laptop"
      Then the response status is 201
      And the response path "resource" is "host:build-1"
      And the response path "task" is absent
      And the response path "initiative" is absent
      And the response path "broadcast" is "true"
      And exactly 1 note_posted event exists for the note "N1"
      And the note_posted event of the note "N1" is on the task "" and the initiative ""
      And the note_posted event of the note "N1" says "note N1 on host:build-1: build host is slow"

    Scenario: a note with no task, initiative or resource is a broadcast and writes note_posted
      When I post a note with body "{'summary':'deploy freeze tonight'}" as actor "claude@laptop"
      Then the response status is 201
      And the response path "broadcast" is "true"
      And the response path "task" is absent
      And the response path "initiative" is absent
      And exactly 1 note_posted event exists for the note "N1"
      And the note_posted event of the note "N1" is on the task "" and the initiative ""
      And the note_posted event of the note "N1" says "note N1 (broadcast): deploy freeze tonight"

    Scenario: mentions are kept, trimmed of repeats, in order
      When I post a note with body "{'summary':'look at this','mentions':['codex@laptop','claude@laptop','codex@laptop']}" as actor "gemini@laptop"
      Then the response status is 201
      And the response path "mentions" has 2 items
      And the response path "mentions.0" is "codex@laptop"
      And the response path "mentions.1" is "claude@laptop"

    Scenario: a task together with its own initiative is accepted
      When I post a note with body "{'task':'aa-2','initiative':'aa','summary':'both'}" as actor "claude@laptop"
      Then the response status is 201
      And the response path "task" is "aa-2"
      And the response path "initiative" is "aa"

    Scenario: the limits themselves are accepted
      When I post a note with body "{'summary':'<<s*200>>','body':'<<b*20000>>','resource':'<<r*100>>'}" as actor "claude@laptop"
      Then the response status is 201
      And the response path "has_body" is "true"

    Scenario: a note on a closed task is still posted
      Given I close the task "aa-1" with body "{'resolution':'dropped'}" as actor "claude@laptop"
      When I post a note with body "{'task':'aa-1','summary':'after the fact'}" as actor "claude@laptop"
      Then the response status is 201

    Scenario Outline: a request with a bad field is 400 and writes nothing
      Given I note the counts of notes, events and actors
      When I post a note with body "<body>" as actor "refused@laptop"
      Then the response status is 400
      And the response path "next" is present
      And there are 0 notes in the database
      And the counts of notes, events and actors are unchanged
      And the actor "refused@laptop" has no row in actors

      Examples:
        | body                                                                                        |
        | {'summary':''}                                                                              |
        | {'summary':'<<s*201>>'}                                                                     |
        | {'summary':'ok','body':'<<b*20001>>'}                                                       |
        | {'summary':'ok','resource':'<<r*101>>'}                                                     |
        | {'summary':'ok','mentions':['m1','m2','m3','m4','m5','m6','m7','m8','m9','m10','m11','m12','m13','m14','m15','m16','m17','m18','m19','m20','m21']} |
        | {'task':'aa-1','initiative':'bb','summary':'mismatch'}                                      |

    Scenario: a summary on two lines is 400 and writes nothing
      Given I note the counts of notes, events and actors
      When I post a note with body "{'summary':'first\nsecond'}" as actor "refused@laptop"
      Then the response status is 400
      And the problem detail contains "one line"
      And the response path "next" is present
      And the counts of notes, events and actors are unchanged
      And the actor "refused@laptop" has no row in actors

    Scenario: a task with an initiative it does not belong to names both
      When I post a note with body "{'task':'aa-1','initiative':'bb','summary':'mismatch'}" as actor "refused@laptop"
      Then the response status is 400
      And the problem detail contains "does not belong"

    Scenario: a request with no body is 400 and writes nothing
      Given I note the counts of notes, events and actors
      When I post a note without a body as actor "refused@laptop"
      Then the response status is 400
      And the response path "next" is present
      And the counts of notes, events and actors are unchanged

    Scenario Outline: a note on something that does not exist is 404 and writes nothing
      Given I note the counts of notes, events and actors
      When I post a note with body "<body>" as actor "refused@laptop"
      Then the response status is 404
      And the response path "next" is present
      And the counts of notes, events and actors are unchanged
      And the actor "refused@laptop" has no row in actors

      Examples:
        | body                                      |
        | {'task':'aa-9','summary':'x'}             |
        | {'initiative':'nothing','summary':'x'}    |

  Rule: Reading notes

    Background:
      Given I post a note with body "{'task':'aa-1','summary':'one','body':'body one','mentions':['codex@laptop']}" as actor "claude@laptop"
      And I post a note with body "{'initiative':'bb','summary':'two'}" as actor "codex@laptop"
      And I post a note with body "{'resource':'host:build-1','summary':'three'}" as actor "claude@laptop"
      And I post a note with body "{'summary':'four','mentions':['claude@laptop']}" as actor "codex@laptop"
      And I post a note with body "{'task':'aa-2','resource':'host:build-1','summary':'five'}" as actor "claude@laptop"
      And I post a note with body "{'task':'bb-1','resource':'repo:board','summary':'six','body':'body six'}" as actor "codex@laptop"

    Scenario: the list is every note in posting order, summaries only
      When I list the notes with "" as actor "reader@laptop"
      Then the response status is 200
      And the response path "items" lists the refs "N1,N2,N3,N4,N5,N6"
      And the response has no next_after
      And the response path "items.0.summary" is "one"
      And the response path "items.0.author" is "claude@laptop"
      And the response path "items.0.task" is "aa-1"
      And the response path "items.0.initiative" is "aa"

    Scenario Outline: a filter keeps the notes that match
      When I list the notes with "<query>" as actor "reader@laptop"
      Then the response status is 200
      And the response path "items" lists the refs "<refs>"

      Examples:
        | query                                             | refs              |
        | task=aa-1                                         | N1                |
        | task=AA-2                                         | N5                |
        | initiative=aa                                     | N1,N5             |
        | initiative=bb                                     | N2,N6             |
        | resource=host:build-1                             | N3,N5             |
        | resource=host:build-1&resource=repo:board         | N3,N5,N6          |
        | resource=nothing:here                             |                   |
        | broadcast=only                                    | N3,N4             |
        | broadcast=exclude                                 | N1,N2,N5,N6       |
        | mention=codex@laptop                              | N1                |
        | mention=claude@laptop                             | N4                |
        | mention=nobody@laptop                             |                   |
        | author=codex@laptop                               | N2,N4,N6          |
        | author=claude@laptop                              | N1,N3,N5          |
        | author=claude@laptop&broadcast=exclude            | N1,N5             |
        | initiative=aa&resource=host:build-1               | N5                |
        | task=aa-1&initiative=bb                           |                   |

    Scenario Outline: since reads a note id, an N ref or a time
      Given the note "N1" was posted at "2026-01-01T00:00:01Z"
      And the note "N2" was posted at "2026-01-01T00:00:02Z"
      And the note "N3" was posted at "2026-01-01T00:00:03Z"
      And the note "N4" was posted at "2026-01-01T00:00:04Z"
      And the note "N5" was posted at "2026-01-01T00:00:05Z"
      And the note "N6" was posted at "2026-01-01T00:00:06Z"
      When I list the notes with "<query>" as actor "reader@laptop"
      Then the response status is 200
      And the response path "items" lists the refs "<refs>"

      Examples:
        | query                                  | refs        |
        | since={id of N3}                       | N4,N5,N6    |
        | since=N3                               | N4,N5,N6    |
        | since=n3                               | N4,N5,N6    |
        | since=N6                               |             |
        | since=2026-01-01T00:00:04Z             | N4,N5,N6    |
        | since=2026-01-01T00:00:03.500Z         | N4,N5,N6    |
        | since=2026-01-01T00:00:03.001Z         | N4,N5,N6    |
        | since=2027-01-01T00:00:00Z             |             |
        | since=2020-01-01T00:00:00Z             | N1,N2,N3,N4,N5,N6 |
        | since=N3&author=codex@laptop           | N4,N6       |

    Scenario: a summary list leaves the body out and says whether there is one
      When I list the notes with "" as actor "reader@laptop"
      Then the response path "items.0.body" is absent
      And the response path "items.0.has_body" is "true"
      And the response path "items.1.has_body" is "false"
      When I list the notes with "fields=summary" as actor "reader@laptop"
      Then the response path "items.0.body" is absent
      And the response path "items.0.has_body" is "true"

    Scenario: fields=full carries the bodies
      When I list the notes with "fields=full" as actor "reader@laptop"
      Then the response status is 200
      And the response path "items.0.body" is "body one"
      And the response path "items.0.has_body" is "true"
      And the response path "items.1.body" is absent
      And the response path "items.1.has_body" is "false"
      And the response path "items.5.body" is "body six"

    Scenario: limit and after page through the notes
      When I list the notes with "limit=2" as actor "reader@laptop"
      Then the response path "items" lists the refs "N1,N2"
      And the response path "next_after" is present
      When I request the next notes page with "limit=2" as actor "reader@laptop"
      Then the response path "items" lists the refs "N3,N4"
      And the response path "next_after" is present
      When I request the next notes page with "limit=2" as actor "reader@laptop"
      Then the response path "items" lists the refs "N5,N6"
      And the response has no next_after

    Scenario: after takes an N ref or a note id
      When I list the notes with "after=N4" as actor "reader@laptop"
      Then the response path "items" lists the refs "N5,N6"
      When I list the notes with "after={id of N2}&limit=1" as actor "reader@laptop"
      Then the response path "items" lists the refs "N3"
      And the response path "next_after" is present

    Scenario Outline: a query that cannot be read is 400 with a next
      When I list the notes with "<query>" as actor "reader@laptop"
      Then the response status is 400
      And the response path "next" is present

      Examples:
        | query                 |
        | since=yesterday       |
        | after=nonsense        |
        | after=2026-01-01T00:00:00Z |
        | broadcast=maybe       |
        | fields=all            |
        | limit=0               |
        | limit=1001            |
        | limit=abc             |

    Scenario Outline: a filter naming a task or initiative that does not exist is 404
      When I list the notes with "<query>" as actor "reader@laptop"
      Then the response status is 404
      And the response path "next" is present

      Examples:
        | query                 |
        | task=aa-9             |
        | initiative=nothing    |

    Scenario: a note is read with its body by its N ref in either case and by its id in either case
      When I request the note "N1" as actor "reader@laptop"
      Then the response status is 200
      And the response path "ref" is "N1"
      And the response path "summary" is "one"
      And the response path "body" is "body one"
      And the response path "has_body" is "true"
      And the response path "task" is "aa-1"
      And the response path "initiative" is "aa"
      And the response path "mentions.0" is "codex@laptop"
      When I request the note "n1" as actor "reader@laptop"
      Then the response path "ref" is "N1"
      When I request the note "N1" by its id as actor "reader@laptop"
      Then the response path "body" is "body one"
      When I request the note "N1" by its lowercase id as actor "reader@laptop"
      Then the response path "body" is "body one"

    Scenario: a note without a body is read without one
      When I request the note "N2" as actor "reader@laptop"
      Then the response status is 200
      And the response path "body" is absent
      And the response path "has_body" is "false"
      And the response path "broadcast" is "false"

    Scenario Outline: reading a note that does not exist is 404
      When I request the note "<ref>" as actor "reader@laptop"
      Then the response status is 404
      And the response path "next" is present

      Examples:
        | ref                        |
        | N99                        |
        | nonsense                   |
        | 01ARZ3NDEKTSV4RRFFQ69G5FAV |
