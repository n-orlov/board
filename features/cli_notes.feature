Feature: Notes from the command line
  vh note posts a note on a task, an initiative, a resource or the whole
  board, vh notes reads them one line each with a + marker on a note that
  has a body, and vh note show prints the body. Every command takes --json,
  and an unknown note exits 4 with the next action.

  Background:
    Given a new database for the board commands
    When I run "vh admin migrate"
    And I run "vh admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port
    And I run the client 'vh init add aa "First one"' as actor "claude@laptop"
    And I run the client 'vh init add bb "Second one"' as actor "claude@laptop"
    And 2 tasks exist in the initiative "aa" as actor "claude@laptop"

  Scenario: note prints one ok line and posts a broadcast
    When I run the client 'vh note "disk is full"' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is exactly "ok note N1 posted"
    And stderr is empty
    And exactly 1 note_posted event exists for the note "N1"
    When I run the client 'vh note show N1 --json' as actor "claude@laptop"
    Then stdout JSON path "summary" is "disk is full"
    And stdout JSON path "author" is "claude@laptop"
    And stdout JSON path "broadcast" is "true"
    And stdout JSON path "has_body" is "false"

  Scenario: note --task posts on a task and takes the task's initiative
    When I run the client 'vh note "found it" --task aa-1' as actor "claude@laptop"
    Then stdout is exactly "ok note N1 posted"
    When I run the client 'vh note show N1 --json' as actor "claude@laptop"
    Then stdout JSON path "task" is "aa-1"
    And stdout JSON path "initiative" is "aa"
    And stdout JSON path "broadcast" is "false"

  Scenario: note --init posts on an initiative, --res on a resource and --mention names actors
    When I run the client 'vh note "freeze the deploys" --init bb --res host:build-1 --mention nik@phone --mention other@laptop' as actor "claude@laptop"
    Then stdout is exactly "ok note N1 posted"
    When I run the client 'vh note show N1 --json' as actor "claude@laptop"
    Then stdout JSON path "initiative" is "bb"
    And stdout JSON path "resource" is "host:build-1"
    And stdout JSON path "mentions.0" is "nik@phone"
    And stdout JSON path "mentions.1" is "other@laptop"
    And stdout JSON path "broadcast" is "false"

  Scenario: a note on a resource alone is still a broadcast
    When I run the client 'vh note "build box is rebooting" --res host:build-1' as actor "claude@laptop"
    Then stdout is exactly "ok note N1 posted"
    When I run the client 'vh note show n1 --json' as actor "claude@laptop"
    Then stdout JSON path "resource" is "host:build-1"
    And stdout JSON path "broadcast" is "true"

  Scenario: --body reads a file
    Given a text file holds "the long version"
    When I run the client 'vh note "short" --body <file>' as actor "claude@laptop"
    Then stdout is exactly "ok note N1 posted"
    When I run the client 'vh note show N1 --json' as actor "claude@laptop"
    Then stdout JSON path "body" is "the long version"
    And stdout JSON path "has_body" is "true"

  Scenario: --body - reads standard input
    Given the standard input holds "from the pipe"
    When I run the client 'vh note "short" --body -' as actor "claude@laptop"
    Then stdout is exactly "ok note N1 posted"
    When I run the client 'vh note show N1 --json' as actor "claude@laptop"
    Then stdout JSON path "body" is "from the pipe"

  Scenario: a --body file that cannot be read is an error and posts nothing
    When I run the client 'vh note "short" --body /nonexistent/body.txt' as actor "claude@laptop"
    Then the exit code is 1
    And stderr is exactly "cannot read the body file /nonexistent/body.txt"
    And there are 0 notes in the database

  Scenario: note show prints the one-line form, the facts and the body
    Given the standard input holds "line one\nline two"
    And I run the client 'vh note "found it" --task aa-1 --res host:x --mention nik@phone --body -' as actor "claude@laptop"
    When I run the client 'vh note show N1' as actor "claude@laptop"
    Then the exit code is 0
    And stdout has 5 lines
    And stdout line 1 matches '^N1  just now  claude@laptop  aa-1  \[host:x\]  found it  \+$'
    And stdout line 2 matches "^initiative: aa$"
    And stdout line 3 matches "^mentions: nik@phone$"
    And stdout line 4 matches "^line one$"
    And stdout line 5 matches "^line two$"

  Scenario: note show of a note with no body prints only the line
    Given I run the client 'vh note "just a line"' as actor "claude@laptop"
    When I run the client 'vh note show N1' as actor "claude@laptop"
    Then stdout has 1 line
    And stdout line 1 matches '^N1  just now  claude@laptop  broadcast  just a line$'

  Scenario: an unknown note exits 4 with the next action
    When I run the client 'vh note show N99' as actor "claude@laptop"
    Then the exit code is 4
    And stderr has 1 line
    And stderr is one line ending with "→ vh notes"
    When I run the client 'vh note show N99 --json' as actor "claude@laptop"
    Then the exit code is 4
    And stderr is one line ending with "→ vh notes"

  Scenario: notes prints one line per note, oldest first, with + on a note with a body
    Given I run the client 'vh note "to everyone"' as actor "claude@laptop"
    And the standard input holds "details"
    And I run the client 'vh note "on a task" --task aa-1 --body -' as actor "other@laptop"
    And I run the client 'vh note "on an initiative" --init bb --res host:x' as actor "claude@laptop"
    When I run the client 'vh notes' as actor "nik@phone"
    Then the exit code is 0
    And stdout has 3 lines
    And stdout line 1 matches '^N1  just now  claude@laptop  broadcast  to everyone$'
    And stdout line 2 matches '^N2  just now  other@laptop  aa-1  on a task  \+$'
    And stdout line 3 matches '^N3  just now  claude@laptop  bb  \[host:x\]  on an initiative$'

  Scenario: notes filters by task, initiative, broadcast, resource and mention
    Given I run the client 'vh note "one" --task aa-1' as actor "claude@laptop"
    And I run the client 'vh note "two" --task aa-2 --res host:x' as actor "claude@laptop"
    And I run the client 'vh note "three" --init bb --res host:y --mention nik@phone' as actor "claude@laptop"
    And I run the client 'vh note "four"' as actor "claude@laptop"
    And I run the client 'vh note "five" --res host:z --mention nik@phone' as actor "claude@laptop"
    When I run the client 'vh notes --task aa-1 --json' as actor "nik@phone"
    Then stdout is a JSON array whose refs are "N1"
    When I run the client 'vh notes --init aa --json' as actor "nik@phone"
    Then stdout is a JSON array whose refs are "N1,N2"
    When I run the client 'vh notes --init bb --json' as actor "nik@phone"
    Then stdout is a JSON array whose refs are "N3"
    When I run the client 'vh notes --broadcast --json' as actor "nik@phone"
    Then stdout is a JSON array whose refs are "N4,N5"
    When I run the client 'vh notes --res host:x --json' as actor "nik@phone"
    Then stdout is a JSON array whose refs are "N2"
    When I run the client 'vh notes --res host:x --res host:y --json' as actor "nik@phone"
    Then stdout is a JSON array whose refs are "N2,N3"
    When I run the client 'vh notes --mention nik@phone --json' as actor "claude@laptop"
    Then stdout is a JSON array whose refs are "N3,N5"
    When I run the client 'vh notes --broadcast --mention nik@phone --json' as actor "claude@laptop"
    Then stdout is a JSON array whose refs are "N5"
    When I run the client 'vh notes --task aa-1 --broadcast' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is empty

  Scenario: notes --since takes an N ref, a time and a date
    Given I run the client 'vh note "one"' as actor "claude@laptop"
    And I run the client 'vh note "two"' as actor "claude@laptop"
    And I run the client 'vh note "three"' as actor "claude@laptop"
    When I run the client 'vh notes --since N1 --json' as actor "nik@phone"
    Then stdout is a JSON array whose refs are "N2,N3"
    When I run the client 'vh notes --since n3 --json' as actor "nik@phone"
    Then stdout is a JSON array whose refs are ""
    When I run the client 'vh notes --since 2000-01-01T00:00:00Z --json' as actor "nik@phone"
    Then stdout is a JSON array whose refs are "N1,N2,N3"
    When I run the client 'vh notes --since 2000-01-01 --json' as actor "nik@phone"
    Then stdout is a JSON array whose refs are "N1,N2,N3"
    When I run the client 'vh notes --since 2999-01-01T00:00:00Z --json' as actor "nik@phone"
    Then stdout is a JSON array whose refs are ""

  Scenario: notes --since last reads from the cursor and --ack moves it
    Given I run the client 'vh note "one"' as actor "claude@laptop"
    And I run the client 'vh note "two"' as actor "claude@laptop"
    When I run the client 'vh notes --since last' as actor "nik@phone"
    Then stdout has 2 lines
    When I run the client 'vh notes --since last' as actor "nik@phone"
    Then stdout has 2 lines
    When I run the client 'vh notes --since last --ack --json' as actor "nik@phone"
    Then stdout is a JSON array whose refs are "N1,N2"
    When I run the client 'vh notes --since last' as actor "nik@phone"
    Then the exit code is 0
    And stdout is empty
    When I run the client 'vh note "three"' as actor "claude@laptop"
    And I run the client 'vh notes --since last' as actor "nik@phone"
    Then stdout has 1 line
    And stdout line 1 matches '^N3  '
    When I run the client 'vh notes --since last' as actor "claude@laptop"
    Then stdout has 3 lines

  Scenario: the cursor of --ack is per actor and an empty page leaves it where it is
    Given I run the client 'vh note "one"' as actor "claude@laptop"
    And I run the client 'vh notes --since last --ack' as actor "nik@phone"
    And I run the client 'vh notes --since last --ack' as actor "nik@phone"
    And I run the client 'vh note "two"' as actor "claude@laptop"
    When I run the client 'vh notes --since last --json' as actor "nik@phone"
    Then stdout is a JSON array whose refs are "N2"

  Scenario: notes shows 5 by default and --ack moves the cursor only past the notes shown
    Given I run the client 'vh note "n1"' as actor "claude@laptop"
    And I run the client 'vh note "n2"' as actor "claude@laptop"
    And I run the client 'vh note "n3"' as actor "claude@laptop"
    And I run the client 'vh note "n4"' as actor "claude@laptop"
    And I run the client 'vh note "n5"' as actor "claude@laptop"
    And I run the client 'vh note "n6"' as actor "claude@laptop"
    And I run the client 'vh note "n7"' as actor "claude@laptop"
    When I run the client 'vh notes --since last --ack' as actor "nik@phone"
    Then stdout has 6 lines
    And stdout line 1 matches '^N1  '
    And stdout line 5 matches '^N5  '
    And stdout line 6 matches '^\+2 more → vh notes --since last --ack --all$'
    When I run the client 'vh notes --since last' as actor "nik@phone"
    Then stdout has 2 lines
    And stdout line 1 matches '^N6  '
    And stdout line 2 matches '^N7  '
    When I run the client 'vh notes --limit 2 --json' as actor "nik@phone"
    Then stdout is a JSON array whose refs are "N1,N2"
    When I run the client 'vh notes --task aa-1 --limit 2' as actor "nik@phone"
    Then stdout is empty
    When I run the client 'vh notes --all --json' as actor "nik@phone"
    Then stdout is a JSON array whose refs are "N1,N2,N3,N4,N5,N6,N7"
    When I run the client 'vh notes --limit 3' as actor "nik@phone"
    Then stdout has 4 lines
    And stdout line 4 matches '^\+4 more → vh notes --all$'

  Scenario: the +K more line repeats every filter
    Given I run the client 'vh note "n1" --init aa --res host:x --mention nik@phone' as actor "claude@laptop"
    And I run the client 'vh note "n2" --init aa --res host:x --mention nik@phone' as actor "claude@laptop"
    And I run the client 'vh note "n3" --init aa --res host:x --mention nik@phone' as actor "claude@laptop"
    When I run the client 'vh notes --since n1 --init aa --res host:x --mention nik@phone --limit 1' as actor "nik@phone"
    Then stdout has 2 lines
    And stdout line 2 matches '^\+1 more → vh notes --since N1 --init aa --mention nik@phone --res host:x --all$'

  Scenario: an unknown task or initiative exits 4 with the next action
    When I run the client 'vh note "x" --task zz-1' as actor "claude@laptop"
    Then the exit code is 4
    And stderr is one line ending with "→ vh task ls"
    When I run the client 'vh note "x" --init nope' as actor "claude@laptop"
    Then the exit code is 4
    And stderr is one line ending with "→ vh init ls"
    And there are 0 notes in the database
    When I run the client 'vh notes --task zz-1' as actor "claude@laptop"
    Then the exit code is 4
    And stderr is one line ending with "→ vh task ls"
    When I run the client 'vh notes --init nope' as actor "claude@laptop"
    Then the exit code is 4
    And stderr is one line ending with "→ vh init ls"

  Scenario: a summary the server refuses exits 1 with its message
    When I run the client 'vh note ""' as actor "claude@laptop"
    Then the exit code is 1
    And stderr has 1 line
    And there are 0 notes in the database

  Scenario: --json prints the posted note, the shown notes and the note
    Given the standard input holds "details"
    When I run the client 'vh note "with a body" --task aa-1 --body - --json' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is a JSON object with a ULID id
    And stdout JSON path "ref" is "N1"
    And stdout JSON path "summary" is "with a body"
    And stdout JSON path "task" is "aa-1"
    And stdout JSON path "has_body" is "true"
    When I run the client 'vh notes --json' as actor "claude@laptop"
    Then stdout is a JSON array of 1 objects with ULID ids
    When I run the client 'vh note show N1 --json' as actor "claude@laptop"
    Then stdout is a JSON object with a ULID id
    And stdout JSON path "body" is "details"

  Scenario: a wrong command line exits 2 with one usage line
    When I run the client 'vh note' as actor "claude@laptop"
    Then the exit code is 2
    And stderr has 1 line
    When I run the client 'vh note "a" "b"' as actor "claude@laptop"
    Then the exit code is 2
    When I run the client 'vh note show' as actor "claude@laptop"
    Then the exit code is 2
    When I run the client 'vh note "a" --bogus' as actor "claude@laptop"
    Then the exit code is 2
    When I run the client 'vh notes extra' as actor "claude@laptop"
    Then the exit code is 2
    And stderr has 1 line
    When I run the client 'vh notes --limit 0' as actor "claude@laptop"
    Then the exit code is 2
    When I run the client 'vh notes --all --limit 2' as actor "claude@laptop"
    Then the exit code is 2
