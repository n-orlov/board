Feature: Editing, closing and reopening initiatives from the command line
  An agent changes an initiative with vh init edit, closes it with vh init
  close (with --force when tasks are left) and reopens it with vh init reopen,
  and lists the closed ones with vh init ls --status. Each write prints one
  ok line; a missing initiative exits 4 and a refused state exits 6, each with
  the command to run next.

  Background:
    Given a new database for the board commands
    When I run "vh admin migrate"
    And I run "vh admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port
    And I run the client 'vh init add aa "First one" --tag work' as actor "claude@laptop"
    And I run the client 'vh init add bb "Second one"' as actor "claude@laptop"

  Scenario: init edit changes the title, the tags and the dates and confirms in one line
    When I run the client 'vh init edit aa --title "Renamed" --tag lab --tag ops --start 2026-01-02 --end 2026-03-04' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is exactly "ok initiative aa updated"
    And stderr is empty
    When I run the client 'vh init show aa' as actor "claude@laptop"
    Then stdout line 1 matches "^aa  open  Renamed  \[lab,ops\]$"
    And stdout line 2 matches "^targets: 2026-01-02 → 2026-03-04$"
    When I run the client 'vh init edit aa --no-tags --clear-start --clear-end' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is exactly "ok initiative aa updated"
    When I run the client 'vh init show aa' as actor "claude@laptop"
    Then stdout line 1 matches "^aa  open  Renamed$"
    And stdout line 2 matches "^created by claude@laptop "
    And exactly 2 initiative_updated events exist for the initiative "aa"

  Scenario: init edit with --json prints the changed object
    When I run the client 'vh init edit aa --title "As JSON" --json' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is one JSON object whose "slug" is "aa"
    And stdout is one JSON object whose "title" is "As JSON"

  Scenario: init edit of an unknown slug exits 4 and names init ls
    When I run the client 'vh init edit nope-nope --title "X"' as actor "claude@laptop"
    Then the exit code is 4
    And stdout is empty
    And stderr is exactly one line containing "→ vh init ls"

  Scenario: a refused edit value exits 1 and a malformed command line exits 2
    When I run the client 'vh init edit aa --start 2026-02-30' as actor "claude@laptop"
    Then the exit code is 1
    And stdout is empty
    And stderr is exactly one line containing "target_start"
    When I run the client 'vh init edit aa' as actor "claude@laptop"
    Then the exit code is 2
    And stderr is exactly one line containing "usage: vh init"
    When I run the client 'vh init edit aa --tag x --no-tags' as actor "claude@laptop"
    Then the exit code is 2
    And stderr is exactly one line containing "usage: vh init"
    When I run the client 'vh init close aa --forced' as actor "claude@laptop"
    Then the exit code is 2
    And stderr is exactly one line containing "usage: vh init"

  Scenario: init close stops at an open task without --force and exits 6 naming the force option
    When I create a task in "aa" titled "one" as actor "claude@laptop"
    And I run the client 'vh init close aa' as actor "claude@laptop"
    Then the exit code is 6
    And stdout is empty
    And stderr is exactly one line containing "→ vh init close aa --force"
    When I run the client 'vh init close aa --force' as actor "op@laptop"
    Then the exit code is 0
    And stdout is exactly "ok initiative aa closed"
    When I run the client 'vh init show aa' as actor "claude@laptop"
    Then stdout line 1 matches "^aa  closed  First one  \[work\]$"
    And stdout line 2 matches "^created by claude@laptop "
    When I run the client 'vh init close aa' as actor "op@laptop"
    Then the exit code is 6
    And stderr is exactly one line containing "→ vh init reopen aa"

  Scenario: init close without tasks left closes, and init reopen reopens
    When I run the client 'vh init close bb --json' as actor "op@laptop"
    Then the exit code is 0
    And stdout is one JSON object whose "status" is "closed"
    When I run the client 'vh init reopen bb' as actor "op@laptop"
    Then the exit code is 0
    And stdout is exactly "ok initiative bb reopened"
    When I run the client 'vh init reopen bb' as actor "op@laptop"
    Then the exit code is 6
    And stdout is empty
    And stderr is exactly one line containing "→ vh init show bb"
    When I run the client 'vh init reopen nope-nope' as actor "op@laptop"
    Then the exit code is 4
    And stderr is exactly one line containing "→ vh init ls"
    When I run the client 'vh init close nope-nope' as actor "op@laptop"
    Then the exit code is 4
    When I run the client 'vh init reopen' as actor "op@laptop"
    Then the exit code is 2

  Scenario: init ls filters by status and by how recently an initiative closed
    When I run the client 'vh init add cc "Third one"' as actor "claude@laptop"
    And I run the client 'vh init close aa' as actor "op@laptop"
    And I run the client 'vh init close bb' as actor "op@laptop"
    And the initiative "bb" was closed 30 days ago
    And I run the client 'vh init ls' as actor "claude@laptop"
    Then stdout is:
      """
      inbox  open  Inbox
      cc  open  Third one
      """
    When I run the client 'vh init ls --status closed' as actor "claude@laptop"
    Then stdout is:
      """
      aa  closed  First one  [work]
      bb  closed  Second one
      """
    When I run the client 'vh init ls --status closed --closed-within 7' as actor "claude@laptop"
    Then stdout is:
      """
      aa  closed  First one  [work]
      """
    When I run the client 'vh init ls --status all --closed-within 7' as actor "claude@laptop"
    Then stdout is:
      """
      inbox  open  Inbox
      aa  closed  First one  [work]
      cc  open  Third one
      """
    When I run the client 'vh init ls --status closed --limit 1' as actor "claude@laptop"
    Then stdout is:
      """
      aa  closed  First one  [work]
      +1 more → vh init ls --status closed --all
      """
    When I run the client 'vh init ls --closed-within 7' as actor "claude@laptop"
    Then the exit code is 2
    And stderr is exactly one line containing "usage: vh init"
    When I run the client 'vh init ls --status sideways' as actor "claude@laptop"
    Then the exit code is 2
