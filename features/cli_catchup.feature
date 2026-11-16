Feature: Catching up from the command line, vh catchup
  vh catchup reads the notes after the caller's notes cursor, acknowledging them, and the answers to the caller's asks
  since its previous catch-up, with one request. It prints the line vh notes prints for each note, then the line
  vh ask --poll prints for each answer (ok A<n> answered by <actor>: <answer>), and nothing when there is nothing new.
  It exits 0, and 2 for a malformed command line.

  Background:
    Given a new database for the board commands
    When I run "vh admin migrate"
    And I run "vh admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port
    And I run the client 'vh init add aa "First one"' as actor "claude@laptop"
    And 3 tasks exist in the initiative "aa" as actor "claude@laptop"

  Scenario: nothing new prints nothing and exits 0
    Given I note the counts of events and cursors
    When I run the client 'vh catchup' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is empty
    And stderr is empty
    And the counts of events and cursors changed by 0 and 0

  Scenario: notes only print one vh notes line for each note and acknowledge them
    Given I run the client 'vh note "found the cause" --task aa-1' as actor "codex@laptop"
    And I run the client 'vh note "registry is slow" --res host:gw1' as actor "codex@laptop"
    And I note the counts of events and cursors
    When I run the client 'vh catchup' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is:
      """
      N1  just now  codex@laptop  aa-1  found the cause
      N2  just now  codex@laptop  broadcast  [host:gw1]  registry is slow
      """
    And stderr is empty
    And the counts of events and cursors changed by 1 and 1
    And exactly 1 catchup event exists for the actor "claude@laptop"
    When I run the client 'vh notes --since last' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is empty

  Scenario: answers only print one ok line for each answer to my asks
    Given I run the client 'vh ask aa-1 "Which database?"' as actor "claude@laptop"
    And I run the client 'vh ask aa-1 "Which port?"' as actor "claude@laptop"
    And I run the client 'vh ask aa-1 "Not mine?"' as actor "codex@laptop"
    And I run the client 'vh answer A1 "use sqlite"' as actor "nik@phone"
    And I run the client 'vh answer A2 "8080"' as actor "nik@phone"
    And I run the client 'vh answer A3 "no"' as actor "nik@phone"
    And I note the counts of events and cursors
    When I run the client 'vh catchup' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is:
      """
      ok A1 answered by nik@phone: use sqlite
      ok A2 answered by nik@phone: 8080
      """
    And the counts of events and cursors changed by 1 and 0
    And exactly 1 catchup event exists for the actor "claude@laptop"

  Scenario: both print the note lines first and then the answer lines
    Given I run the client 'vh note "registry is slow"' as actor "codex@laptop"
    And I run the client 'vh ask aa-1 "Which database?"' as actor "claude@laptop"
    And I run the client 'vh answer A1 "use sqlite"' as actor "nik@phone"
    And I note the counts of events and cursors
    When I run the client 'vh catchup' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is:
      """
      N1  just now  codex@laptop  broadcast  registry is slow
      ok A1 answered by nik@phone: use sqlite
      """
    And the counts of events and cursors changed by 1 and 1
    And exactly 1 catchup event exists for the actor "claude@laptop"

  Scenario: a second call straight after prints nothing and writes nothing
    Given I run the client 'vh note "registry is slow"' as actor "codex@laptop"
    And I run the client 'vh ask aa-1 "Which database?"' as actor "claude@laptop"
    And I run the client 'vh answer A1 "use sqlite"' as actor "nik@phone"
    And I run the client 'vh catchup' as actor "claude@laptop"
    And I note the counts of events and cursors
    When I run the client 'vh catchup' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is empty
    And the counts of events and cursors changed by 0 and 0
    And exactly 1 catchup event exists for the actor "claude@laptop"

  Scenario: --json prints the response with the notes and the answers
    Given I run the client 'vh note "registry is slow"' as actor "codex@laptop"
    And I run the client 'vh ask aa-1 "Which database?"' as actor "claude@laptop"
    And I run the client 'vh answer A1 "use sqlite"' as actor "nik@phone"
    When I run the client 'vh catchup --json' as actor "claude@laptop"
    Then the exit code is 0
    And stdout JSON path "notes.0.ref" is "N1"
    And stdout JSON path "answers.0.answer" is "use sqlite"

  Scenario Outline: a malformed command line exits 2 with one usage line and sends nothing
    Given I run the client 'vh note "registry is slow"' as actor "codex@laptop"
    And I note the counts of events and cursors
    When I run the client '<command>' as actor "claude@laptop"
    Then the exit code is 2
    And stdout is empty
    And stderr is one line ending with 'usage: vh catchup [--json]'
    And the counts of events and cursors changed by 0 and 0

    Examples:
      | command               |
      | vh catchup now        |
      | vh catchup --no-such  |
