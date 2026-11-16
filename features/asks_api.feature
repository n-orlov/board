Feature: Asks over HTTP
  An ask is a question an actor raises for the operator, on a task, on an initiative or on the board as a
  whole. POST /api/v1/asks raises it (201, ask_raised), GET reads and lists asks, and answer and withdraw move
  an open ask to answered or withdrawn. An ask that is not open cannot be answered or withdrawn again (409, naming
  its state). A refused request changes nothing: no ask, event or actor row.

  Background:
    Given a new database for the board commands
    When I run "vh admin migrate"
    And I run "vh admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port
    And I create the initiative "aa" titled "First" with no tags as actor "claude@laptop"
    And I create the initiative "bb" titled "Second" with no tags as actor "claude@laptop"
    And I create a task in "aa" titled "one" as actor "claude@laptop"
    And I create a task in "aa" titled "two" as actor "claude@laptop"
    And I create a task in "bb" titled "other" as actor "claude@laptop"

  Scenario: raising an ask on a task answers 201 with the open ask
    When I raise an ask with body "{'task':'aa-1','question':'Which database?'}" as actor "claude@laptop"
    Then the response status is 201
    And the response path "ref" is "A1"
    And the response path "task" is "aa-1"
    And the response path "initiative" is "aa"
    And the response path "asker" is "claude@laptop"
    And the response path "question" is "Which database?"
    And the response path "details" is ""
    And the response path "options" has 0 items
    And the response path "mode" is "stop"
    And the response path "blocking" is "false"
    And the response path "state" is "open"
    And the response path "asker_waiting" is "false"
    And the response path "created_at" is present
    And the response path "answer" is absent
    And the response path "answered_by" is absent
    And the response path "channel" is absent
    And exactly 1 ask_raised event exists for the ask "A1"
    And the latest ask_raised event of the ask "A1" is on the task "aa-1" and the initiative "aa"
    And the latest ask_raised event of the ask "A1" has principal "key:workstation" and actor "claude@laptop"

  Scenario: every optional field is kept
    When I raise an ask with body "{'task':'aa-1','question':'Which one?','details':'Some **markdown**.','options':['red','green'],'mode':'poll','blocking':true}" as actor "claude@laptop"
    Then the response status is 201
    And the response path "details" is "Some **markdown**."
    And the response path "options" has 2 items
    And the response path "options.1" is "green"
    And the response path "mode" is "poll"
    And the response path "blocking" is "true"

  Scenario: raising an ask on an initiative has no task
    When I raise an ask with body "{'initiative':'aa','question':'Ship it?'}" as actor "claude@laptop"
    Then the response status is 201
    And the response path "initiative" is "aa"
    And the response path "task" is absent
    And the latest ask_raised event of the ask "A1" is on the task "" and the initiative "aa"

  Scenario: raising a global ask has neither task nor initiative
    When I raise an ask with body "{'question':'Anything to add?'}" as actor "claude@laptop"
    Then the response status is 201
    And the response path "task" is absent
    And the response path "initiative" is absent
    And the latest ask_raised event of the ask "A1" is on the task "" and the initiative ""

  Scenario: a task together with its own initiative is accepted
    When I raise an ask with body "{'task':'aa-2','initiative':'aa','question':'Both?'}" as actor "claude@laptop"
    Then the response status is 201
    And the response path "task" is "aa-2"
    And the response path "initiative" is "aa"

  Scenario: a task with an initiative it does not belong to is 400 and writes nothing
    Given I note the counts of asks, events and actors
    When I raise an ask with body "{'task':'aa-1','initiative':'bb','question':'Mismatch?'}" as actor "refused@laptop"
    Then the response status is 400
    And the problem detail contains "does not belong"
    And the response path "next" is present
    And there are 0 asks in the database
    And the counts of asks, events and actors are unchanged
    And the actor "refused@laptop" has no row in actors

  Scenario Outline: a request with a bad field is 400 and writes nothing
    Given I note the counts of asks, events and actors
    When I raise an ask with body "<body>" as actor "refused@laptop"
    Then the response status is 400
    And the response path "next" is present
    And there are 0 asks in the database
    And the counts of asks, events and actors are unchanged
    And the actor "refused@laptop" has no row in actors

    Examples:
      | body                                                          |
      | {'question':''}                                               |
      | {'question':'<<q*201>>'}                                      |
      | {'question':'ok','details':'<<d*20001>>'}                     |
      | {'question':'ok','options':['1','2','3','4','5','6','7','8','9','10','11']} |
      | {'question':'ok','options':['']}                              |
      | {'question':'ok','options':['<<o*81>>']}                      |
      | {'question':'ok','mode':'shout'}                              |

  Scenario: a question on two lines is 400 and writes nothing
    Given I note the counts of asks, events and actors
    When I raise an ask with body "{'question':'first\nsecond'}" as actor "refused@laptop"
    Then the response status is 400
    And the problem detail contains "one line"
    And the response path "next" is present
    And the counts of asks, events and actors are unchanged
    And the actor "refused@laptop" has no row in actors

  Scenario: the limits themselves are accepted
    When I raise an ask with body "{'question':'<<q*200>>','details':'<<d*20000>>','options':['<<o*80>>','2','3','4','5','6','7','8','9','10']}" as actor "claude@laptop"
    Then the response status is 201
    And the response path "options" has 10 items

  Scenario: a request with no body is 400 and writes nothing
    Given I note the counts of asks, events and actors
    When I raise an ask without a body as actor "refused@laptop"
    Then the response status is 400
    And the response path "next" is present
    And the counts of asks, events and actors are unchanged

  Scenario Outline: an ask on something that does not exist is 404 and writes nothing
    Given I note the counts of asks, events and actors
    When I raise an ask with body "<body>" as actor "refused@laptop"
    Then the response status is 404
    And the response path "next" is present
    And the counts of asks, events and actors are unchanged
    And the actor "refused@laptop" has no row in actors

    Examples:
      | body                                       |
      | {'task':'aa-9','question':'x'}             |
      | {'initiative':'nothing','question':'x'}    |

  Scenario: an ask on a closed task is 409 and writes nothing
    Given I close the task "aa-1" with body "{'resolution':'dropped'}" as actor "claude@laptop"
    And I note the counts of asks, events and actors
    When I raise an ask with body "{'task':'aa-1','question':'Still there?'}" as actor "refused@laptop"
    Then the response status is 409
    And the response path "detail" is "cannot raise an ask on task aa-1: it is closed"
    And the response path "next" is present
    And there are 0 asks in the database
    And the counts of asks, events and actors are unchanged
    And the actor "refused@laptop" has no row in actors

  Scenario: an ask on a closed initiative is 409 and writes nothing
    Given I close the task "bb-1" with body "{'resolution':'dropped'}" as actor "claude@laptop"
    And I close the initiative "bb" without a body as actor "claude@laptop"
    And I note the counts of asks, events and actors
    When I raise an ask with body "{'initiative':'bb','question':'Still there?'}" as actor "refused@laptop"
    Then the response status is 409
    And the response path "detail" is "cannot raise an ask on initiative bb: it is closed"
    And the response path "next" is present
    And the counts of asks, events and actors are unchanged
    And the actor "refused@laptop" has no row in actors

  Scenario: an ask is read by its A ref in either case and by its id in either case
    Given I raise an ask with body "{'task':'aa-1','question':'Which database?','options':['pg','sqlite']}" as actor "claude@laptop"
    When I request the ask "A1" as actor "reader@laptop"
    Then the response status is 200
    And the response path "ref" is "A1"
    And the response path "task" is "aa-1"
    And the response path "initiative" is "aa"
    And the response path "asker" is "claude@laptop"
    And the response path "options" has 2 items
    And the response path "state" is "open"
    And the response path "asker_waiting" is "false"
    When I request the ask "a1" as actor "reader@laptop"
    Then the response status is 200
    And the response path "ref" is "A1"
    When I request the ask "A1" by its id as actor "reader@laptop"
    Then the response status is 200
    And the response path "ref" is "A1"
    When I request the ask "A1" by its lowercase id as actor "reader@laptop"
    Then the response status is 200
    And the response path "ref" is "A1"

  Scenario Outline: reading an ask that does not exist is 404
    When I request the ask "<ref>" as actor "reader@laptop"
    Then the response status is 404
    And the response path "next" is present

    Examples:
      | ref                        |
      | A99                        |
      | nonsense                   |
      | 01ARZ3NDEKTSV4RRFFQ69G5FAV |

  Scenario: the list is open asks by default, in raising order
    Given I raise an ask with body "{'task':'aa-1','question':'first'}" as actor "claude@laptop"
    And I raise an ask with body "{'initiative':'bb','question':'second'}" as actor "codex@laptop"
    And I raise an ask with body "{'question':'third'}" as actor "claude@laptop"
    And I withdraw the ask "A2" without a body as actor "claude@laptop"
    When I list the asks with "" as actor "reader@laptop"
    Then the response status is 200
    And the response path "items" lists the refs "A1,A3"
    And the response has no next_after
    When I list the asks with "state=all" as actor "reader@laptop"
    Then the response path "items" lists the refs "A1,A2,A3"

  Scenario: the state filter keeps open, answered, withdrawn or all
    Given I raise an ask with body "{'question':'one'}" as actor "claude@laptop"
    And I raise an ask with body "{'question':'two'}" as actor "claude@laptop"
    And I raise an ask with body "{'question':'three'}" as actor "claude@laptop"
    And I answer the ask "A2" with body "{'answer':'yes'}" as actor "claude@laptop"
    And I withdraw the ask "A3" without a body as actor "claude@laptop"
    When I list the asks with "state=open" as actor "reader@laptop"
    Then the response path "items" lists the refs "A1"
    When I list the asks with "state=answered" as actor "reader@laptop"
    Then the response path "items" lists the refs "A2"
    When I list the asks with "state=withdrawn" as actor "reader@laptop"
    Then the response path "items" lists the refs "A3"
    When I list the asks with "state=all" as actor "reader@laptop"
    Then the response path "items" lists the refs "A1,A2,A3"

  Scenario: the actor, task and initiative filters narrow the list and combine
    Given I raise an ask with body "{'task':'aa-1','question':'one'}" as actor "claude@laptop"
    And I raise an ask with body "{'task':'aa-2','question':'two'}" as actor "codex@laptop"
    And I raise an ask with body "{'task':'bb-1','question':'three'}" as actor "claude@laptop"
    And I raise an ask with body "{'initiative':'aa','question':'four'}" as actor "claude@laptop"
    And I raise an ask with body "{'question':'five'}" as actor "codex@laptop"
    When I list the asks with "actor=claude@laptop" as actor "reader@laptop"
    Then the response path "items" lists the refs "A1,A3,A4"
    When I list the asks with "task=aa-2" as actor "reader@laptop"
    Then the response path "items" lists the refs "A2"
    When I list the asks with "initiative=aa" as actor "reader@laptop"
    Then the response path "items" lists the refs "A1,A2,A4"
    When I list the asks with "initiative=aa&actor=codex@laptop" as actor "reader@laptop"
    Then the response path "items" lists the refs "A2"
    When I list the asks with "actor=nobody@laptop" as actor "reader@laptop"
    Then the response path "items" has 0 items

  Scenario Outline: a list filter that is wrong is refused
    When I list the asks with "<query>" as actor "reader@laptop"
    Then the response status is <status>
    And the response path "next" is present

    Examples:
      | query           | status |
      | state=pending   | 400    |
      | limit=0         | 400    |
      | limit=1001      | 400    |
      | after=A99       | 400    |
      | after=not-an-id | 400    |
      | task=aa-9       | 404    |
      | initiative=zzz  | 404    |

  Scenario: limit and after page through the asks
    Given I raise an ask with body "{'question':'one'}" as actor "claude@laptop"
    And I raise an ask with body "{'question':'two'}" as actor "claude@laptop"
    And I raise an ask with body "{'question':'three'}" as actor "claude@laptop"
    When I list the asks with "limit=2" as actor "reader@laptop"
    Then the response path "items" lists the refs "A1,A2"
    And the response path "next_after" is present
    When I request the next asks page with "limit=2" as actor "reader@laptop"
    Then the response path "items" lists the refs "A3"
    And the response has no next_after
    When I list the asks with "limit=1&after=A1" as actor "reader@laptop"
    Then the response path "items" lists the refs "A2"

  Scenario Outline: answering an ask records the channel
    Given I raise an ask with body "{'task':'aa-1','question':'Which?'}" as actor "claude@laptop"
    When I answer the ask "A1" with body "<body>" as actor "operator@laptop"
    Then the response status is 200
    And the response path "state" is "answered"
    And the response path "answer" is "<answer>"
    And the response path "channel" is "<channel>"
    And the response path "answered_by" is "operator@laptop"
    And the response path "answered_at" is present
    And the ask "A1" is "answered" in the database
    And exactly 1 ask_answered event exists for the ask "A1"
    And the latest ask_answered event of the ask "A1" is on the task "aa-1" and the initiative "aa"
    And the latest ask_answered event of the ask "A1" has principal "key:workstation" and actor "operator@laptop"

    Examples:
      | body                                  | answer | channel |
      | {'answer':'pg','channel':'ui'}        | pg     | ui      |
      | {'answer':'pg','channel':'api'}       | pg     | api     |
      | {'answer':'pg','channel':'relay'}     | pg     | relay   |
      | {'answer':'pg'}                       | pg     | api     |

  Scenario: a relayed answer names the person it came from
    Given I raise an ask with body "{'task':'aa-1','question':'Which?'}" as actor "claude@laptop"
    When I answer the ask "A1" with body "{'answer':'the second one','channel':'relay','on_behalf_of':'nik@phone'}" as actor "relay@laptop"
    Then the response status is 200
    And the response path "channel" is "relay"
    And the response path "answered_by" is "relay@laptop"
    And the response path "on_behalf_of" is "nik@phone"
    And the latest ask_answered event of the ask "A1" is on behalf of "nik@phone"
    When I request the ask "A1" as actor "reader@laptop"
    Then the response path "on_behalf_of" is "nik@phone"
    And the response path "answer" is "the second one"

  Scenario: an answer is read back with its asker still named
    Given I raise an ask with body "{'question':'Which?'}" as actor "claude@laptop"
    And I answer the ask "a1" with body "{'answer':'pg'}" as actor "operator@laptop"
    When I request the ask "A1" as actor "reader@laptop"
    Then the response path "state" is "answered"
    And the response path "asker" is "claude@laptop"
    And the response path "asker_waiting" is "false"

  Scenario Outline: an answer that is wrong is 400 and writes nothing
    Given I raise an ask with body "{'question':'Which?'}" as actor "claude@laptop"
    And I note the counts of asks, events and actors
    When I answer the ask "A1" with body "<body>" as actor "refused@laptop"
    Then the response status is 400
    And the response path "next" is present
    And the ask "A1" is "open" in the database
    And the counts of asks, events and actors are unchanged
    And the actor "refused@laptop" has no row in actors

    Examples:
      | body                                              |
      | {'answer':''}                                     |
      | {'answer':'<<a*20001>>'}                          |
      | {'answer':'x','channel':'smoke'}                  |
      | {'answer':'x','on_behalf_of':'nik@phone'}         |
      | {'answer':'x','channel':'api','on_behalf_of':'n'} |

  Scenario: an answer of 20000 characters is accepted
    Given I raise an ask with body "{'question':'Which?'}" as actor "claude@laptop"
    When I answer the ask "A1" with body "{'answer':'<<a*20000>>'}" as actor "operator@laptop"
    Then the response status is 200

  Scenario: an answer with no body is 400
    Given I raise an ask with body "{'question':'Which?'}" as actor "claude@laptop"
    When I answer the ask "A1" without a body as actor "refused@laptop"
    Then the response status is 400
    And the response path "next" is present

  Scenario: withdrawing an ask records the reason
    Given I raise an ask with body "{'task':'aa-1','question':'Which?'}" as actor "claude@laptop"
    When I withdraw the ask "A1" with body "{'reason':'solved it myself'}" as actor "claude@laptop"
    Then the response status is 200
    And the response path "state" is "withdrawn"
    And the response path "withdraw_reason" is "solved it myself"
    And the response path "withdrawn_at" is present
    And the response path "answer" is absent
    And the ask "A1" is "withdrawn" in the database
    And exactly 1 ask_withdrawn event exists for the ask "A1"
    And the latest ask_withdrawn event of the ask "A1" is on the task "aa-1" and the initiative "aa"

  Scenario: withdrawing with no body is allowed
    Given I raise an ask with body "{'question':'Which?'}" as actor "claude@laptop"
    When I withdraw the ask "a1" without a body as actor "claude@laptop"
    Then the response status is 200
    And the response path "state" is "withdrawn"
    And the response path "withdraw_reason" is absent

  Scenario: a withdraw reason of over 200 characters is 400 and writes nothing
    Given I raise an ask with body "{'question':'Which?'}" as actor "claude@laptop"
    And I note the counts of asks, events and actors
    When I withdraw the ask "A1" with body "{'reason':'<<r*201>>'}" as actor "refused@laptop"
    Then the response status is 400
    And the response path "next" is present
    And the ask "A1" is "open" in the database
    And the counts of asks, events and actors are unchanged

  Scenario Outline: answering or withdrawing an ask that is not open is 409 naming its state
    Given I raise an ask with body "{'question':'Which?'}" as actor "claude@laptop"
    And I <first> the ask "A1" <firstbody> as actor "claude@laptop"
    And I note the counts of asks, events and actors
    When I <second> the ask "A1" <secondbody> as actor "refused@laptop"
    Then the response status is 409
    And the response path "detail" is "cannot <verb> ask A1: it is <state>"
    And the response path "next" is present
    And the ask "A1" is "<state>" in the database
    And the counts of asks, events and actors are unchanged
    And the actor "refused@laptop" has no row in actors

    Examples:
      | first    | firstbody                 | second   | secondbody                | verb     | state     |
      | answer   | with body "{'answer':'x'}" | answer   | with body "{'answer':'y'}" | answer   | answered  |
      | answer   | with body "{'answer':'x'}" | withdraw | without a body            | withdraw | answered  |
      | withdraw | without a body            | answer   | with body "{'answer':'y'}" | answer   | withdrawn |
      | withdraw | without a body            | withdraw | without a body            | withdraw | withdrawn |

  Scenario Outline: answering or withdrawing an ask that does not exist is 404
    Given I note the counts of asks, events and actors
    When I <action> the ask "A99" <body> as actor "refused@laptop"
    Then the response status is 404
    And the response path "next" is present
    And the counts of asks, events and actors are unchanged

    Examples:
      | action   | body                      |
      | answer   | with body "{'answer':'x'}" |
      | withdraw | without a body            |
