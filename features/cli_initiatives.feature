Feature: Initiatives from the command line
  An agent creates, lists and reads initiatives, and checks who it is, with the
  board client commands. The client reads BOARD_URL, BOARD_API_KEY, BOARD_ACTOR
  and BOARD_WAY_BACK from the environment (no flag takes the key), and with
  BOARD_ACTOR unset it acts as agent:<hostname>:<ppid>.

  Background:
    Given a new database for the board commands
    When I run "board admin migrate"
    And I run "board admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port

  Scenario: init add creates an initiative and confirms it in one line
    When I run the client 'board init add phone-onboard "Onboard the new phone" --tag Work --tag homelab' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is exactly "ok initiative phone-onboard created"
    And stderr is empty
    And neither stdout nor stderr contains the remembered key
    And the initiative_created event for "phone-onboard" has principal "key:workstation" and actor "claude@laptop"
    When I run the client 'board init add phone-onboard "Again"' as actor "claude@laptop"
    Then the exit code is 1
    And stdout is empty
    And stderr is exactly one line containing "→"
    When I run the client 'board init add X "Bad slug"' as actor "claude@laptop"
    Then the exit code is 1
    And stderr is exactly one line containing "slug"

  Scenario: init ls lists in creation order, filters by tag and caps the list
    When I run the client 'board init add alpha "First one" --tag work' as actor "claude@laptop"
    And I run the client 'board init add beta "Second one" --tag work --tag homelab' as actor "claude@laptop"
    And I run the client 'board init add gamma "Third one"' as actor "claude@laptop"
    And I run the client 'board init ls' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is:
      """
      alpha  open  First one  [work]
      beta  open  Second one  [homelab,work]
      gamma  open  Third one
      """
    When I run the client 'board init ls --tag work --tag homelab' as actor "claude@laptop"
    Then stdout is:
      """
      beta  open  Second one  [homelab,work]
      """
    When I run the client 'board init ls --limit 2' as actor "claude@laptop"
    Then stdout is:
      """
      alpha  open  First one  [work]
      beta  open  Second one  [homelab,work]
      +1 more → board init ls --all
      """
    When I run the client 'board init ls --tag nothing' as actor "claude@laptop"
    Then the exit code is 0
    And stdout is empty

  Scenario: init show prints one initiative and exits 4 for an unknown slug
    When I run the client 'board init add phone-onboard "Onboard the new phone" --tag work' as actor "claude@laptop"
    And I run the client 'board init show phone-onboard' as actor "claude@laptop"
    Then the exit code is 0
    And stdout line 1 matches "^phone-onboard  open  Onboard the new phone  \[work\]$"
    And stdout line 2 matches "^created by claude@laptop (just now|[0-9]+m ago)$"
    When I run the client 'board init show nope-nope' as actor "claude@laptop"
    Then the exit code is 4
    And stdout is empty
    And stderr is exactly one line containing "→"
    When I run the client 'board init show phone-onboard --api-key brd_nope' as actor "claude@laptop"
    Then the exit code is 2
    And stderr is exactly one line containing "usage: board init"
    And neither stdout nor stderr contains the remembered key

  Scenario: whoami prints the handle, the way back, the key name and server ok
    When I run the client 'board whoami' as actor "claude@laptop" with way back "claude --resume abc"
    Then the exit code is 0
    And stdout is exactly "you: claude@laptop (key workstation)  way back: claude --resume abc  server ok"
    And neither stdout nor stderr contains the remembered key
    When I run the client 'board whoami' with no actor
    Then the exit code is 0
    And stdout line 1 matches "^you: agent:[^:]+:[0-9]+ \(key workstation\)  way back: pid [0-9]+ on .+, cwd .+  server ok$"
