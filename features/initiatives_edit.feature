Feature: Editing an initiative over HTTP
  PATCH /api/v1/initiatives/{slug} changes the title, description, tags (which
  replace the set) and target dates of an initiative; null clears a date. The
  same validation as a create applies to the initiative as it would be after the
  patch, and the slug is immutable. A patch that changes nothing, such as {},
  answers 200 and writes no event. A change writes one initiative_updated event
  whose data.changed names the changed fields. A refusal changes nothing.

  Background:
    Given a new database for the board commands
    When I run "vh admin migrate"
    And I run "vh admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port
    And I create the initiative with body "{'slug':'aa','title':'First','description':'one','tags':['work','home'],'target_start':'2026-03-01','target_end':'2026-04-01'}" as actor "claude@laptop"

  Scenario: patch the title
    When I patch the initiative "aa" with body "{'title':'  Renamed  '}" as actor "editor@laptop"
    Then the response status is 200
    And the response field "title" is "Renamed"
    And the response field "description" is "one"
    And the response field "tags" is "home,work"
    And the response field "slug" is "aa"
    And the response field "created_by" is "claude@laptop"
    And the latest initiative_updated event of the initiative "aa" has data.changed "title"

  Scenario: patch the description
    When I patch the initiative "aa" with body "{'description':'# Plan'}" as actor "editor@laptop"
    Then the response status is 200
    And the response field "description" is "# Plan"
    And the latest initiative_updated event of the initiative "aa" has data.changed "description"
    When I patch the initiative "aa" with body "{'description':''}" as actor "editor@laptop"
    Then the response status is 200
    And the response field "description" is ""

  Scenario: patch the tags replaces the set
    When I patch the initiative "aa" with body "{'tags':['Alpha',' beta','alpha']}" as actor "editor@laptop"
    Then the response status is 200
    And the response field "tags" is "alpha,beta"
    And the latest initiative_updated event of the initiative "aa" has data.changed "tags"
    When I patch the initiative "aa" with body "{'tags':[]}" as actor "editor@laptop"
    Then the response status is 200
    And the response path "tags" has 0 items
    When I request the initiative "aa" as actor "editor@laptop"
    Then the response path "tags" has 0 items
    And exactly 2 initiative_updated events exist for the initiative "aa"

  Scenario: patch the target dates
    When I patch the initiative "aa" with body "{'target_start':'2026-05-01','target_end':'2026-06-01'}" as actor "editor@laptop"
    Then the response status is 200
    And the response field "target_start" is "2026-05-01"
    And the response field "target_end" is "2026-06-01"
    And the latest initiative_updated event of the initiative "aa" has data.changed "target_start,target_end"
    When I request the initiative "aa" as actor "editor@laptop"
    Then the response field "target_start" is "2026-05-01"

  Scenario: a change to the end is checked against the stored start
    When I patch the initiative "aa" with body "{'target_end':'2026-02-01'}" as actor "editor@laptop"
    Then the response status is 400
    And the problem has a next field containing "YYYY-MM-DD"
    When I patch the initiative "aa" with body "{'target_end':'2026-05-01'}" as actor "editor@laptop"
    Then the response status is 200

  Scenario: a date is cleared with null
    When I patch the initiative "aa" with body "{'target_start':null}" as actor "editor@laptop"
    Then the response status is 200
    And the response path "target_start" is absent
    And the response field "target_end" is "2026-04-01"
    And the latest initiative_updated event of the initiative "aa" has data.changed "target_start"
    When I patch the initiative "aa" with body "{'target_end':null}" as actor "editor@laptop"
    Then the response status is 200
    And the response path "target_end" is absent
    When I request the initiative "aa" as actor "editor@laptop"
    Then the response path "target_start" is absent
    And the response path "target_end" is absent

  Scenario: every field at once names the changed fields in order
    When I patch the initiative "aa" with body "{'target_end':null,'tags':['x'],'title':'New','description':'d','target_start':'2026-01-01'}" as actor "editor@laptop"
    Then the response status is 200
    And the latest initiative_updated event of the initiative "aa" has data.changed "title,description,tags,target_start,target_end"

  Scenario: a change writes one event with the principal, the actor and the slug
    When I patch the initiative "aa" with body "{'title':'Renamed'}" as actor "editor@laptop"
    Then the response status is 200
    And exactly 1 initiative_updated event exists for the initiative "aa"
    And the latest initiative_updated event of the initiative "aa" has data.slug "aa"
    And the actor "editor@laptop" has exactly one row in actors

  Scenario: a slug in the body is 400 even when unchanged
    When I note the counts of initiatives, tags, events and actors
    And I patch the initiative "aa" with body "{'slug':'aa'}" as actor "refused@laptop"
    Then the response status is 400
    And the problem detail contains "slug"
    And the problem has a next field containing "Leave slug out"
    When I patch the initiative "aa" with body "{'slug':'bb','title':'Renamed'}" as actor "refused@laptop"
    Then the response status is 400
    And the counts of initiatives, tags, events and actors are unchanged
    And there is no actor row for "refused@laptop"
    When I request the initiative "aa" as actor "refused@laptop"
    Then the response field "title" is "First"
    And the response field "slug" is "aa"

  Scenario: a bad title is 400 and writes nothing
    When I note the counts of initiatives, tags, events and actors
    And I patch the initiative "aa" with body "{'title':'   '}" as actor "refused@laptop"
    Then the response status is 400
    And the problem has a next field containing "title"
    When I patch the initiative "aa" with body "{'title':'<<t*201>>'}" as actor "refused@laptop"
    Then the response status is 400
    When I patch the initiative "aa" with body "{'title':null}" as actor "refused@laptop"
    Then the response status is 400
    And the counts of initiatives, tags, events and actors are unchanged
    And there is no actor row for "refused@laptop"

  Scenario: a bad tag is 400 and writes nothing
    When I note the counts of initiatives, tags, events and actors
    And I patch the initiative "aa" with body "{'title':'Renamed','tags':['fine','no way']}" as actor "refused@laptop"
    Then the response status is 400
    And the problem has a next field containing "tags"
    When I patch the initiative "aa" with body "{'tags':null}" as actor "refused@laptop"
    Then the response status is 400
    When I patch the initiative "aa" with body "{'tags':['a1','a2','a3','a4','a5','a6','a7','a8','a9','b1','b2','b3','b4','b5','b6','b7','b8','b9','c1','c2','c3']}" as actor "refused@laptop"
    Then the response status is 400
    And the counts of initiatives, tags, events and actors are unchanged
    When I request the initiative "aa" as actor "refused@laptop"
    Then the response field "title" is "First"
    And the response field "tags" is "home,work"

  Scenario: an end before the start is 400 and writes nothing
    When I note the counts of initiatives, tags, events and actors
    And I patch the initiative "aa" with body "{'target_start':'2026-06-01','target_end':'2026-05-01'}" as actor "refused@laptop"
    Then the response status is 400
    And the problem detail contains "target_end"
    When I patch the initiative "aa" with body "{'target_start':'2026-05-01','target_end':'soon'}" as actor "refused@laptop"
    Then the response status is 400
    And the counts of initiatives, tags, events and actors are unchanged
    When I request the initiative "aa" as actor "refused@laptop"
    Then the response field "target_start" is "2026-03-01"
    And the response field "target_end" is "2026-04-01"

  Scenario: an unknown initiative is 404
    When I note the counts of initiatives, tags, events and actors
    And I patch the initiative "zz" with body "{'title':'Renamed'}" as actor "refused@laptop"
    Then the response status is 404
    And the problem has a next field containing "GET /api/v1/initiatives"
    And the counts of initiatives, tags, events and actors are unchanged
    And there is no actor row for "refused@laptop"

  Scenario: an empty body and a body that changes nothing are 200 and write no event
    When I note the counts of initiatives, tags, events and actors
    And I patch the initiative "aa" with body "{}" as actor "quiet@laptop"
    Then the response status is 200
    And the response field "title" is "First"
    When I patch the initiative "aa" with body "{'title':'First','description':'one','tags':['Home','work'],'target_start':'2026-03-01','target_end':'2026-04-01'}" as actor "quiet@laptop"
    Then the response status is 200
    And the response field "tags" is "home,work"
    And the counts of initiatives, tags, events and actors are unchanged
    And exactly 0 initiative_updated events exist for the initiative "aa"
    And there is no actor row for "quiet@laptop"

  Scenario: a second identical patch writes no second event
    When I patch the initiative "aa" with body "{'title':'Renamed'}" as actor "editor@laptop"
    And I patch the initiative "aa" with body "{'title':'Renamed'}" as actor "editor@laptop"
    Then the response status is 200
    And exactly 1 initiative_updated event exists for the initiative "aa"

  Scenario: an initiative is found by its id in either letter case
    When I patch the initiative "aa" by its id with body "{'title':'By id'}" as actor "editor@laptop"
    Then the response status is 200
    And the response field "slug" is "aa"
    When I patch the initiative "aa" by its lowercase id with body "{'title':'By lowercase id'}" as actor "editor@laptop"
    Then the response status is 200
    And the response field "title" is "By lowercase id"
    And exactly 2 initiative_updated events exist for the initiative "aa"
