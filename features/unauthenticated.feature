Feature: Only /healthz needs no credential
  The operation list is read from api/openapi.yaml when the scenario runs, so
  an operation added to the document is probed without anyone editing this
  file. Every declared operation, sent with no Authorization header and no
  cookie, answers 401, except GET /healthz, which answers 200.

  Scenario: the document declares the spec endpoints and the probes
    Then api/openapi.yaml declares "GET /healthz"
    And api/openapi.yaml declares "GET /readyz"
    And api/openapi.yaml declares "GET /api/openapi.yaml"
    And api/openapi.yaml declares "GET /api/openapi.json"
    And api/openapi.yaml declares "GET /api/docs"

  Scenario: every declared operation but GET /healthz answers 401 without credentials
    Given the board server is running on a free port
    When I send every operation declared in api/openapi.yaml with no Authorization header and no cookie
    Then every operation except "GET /healthz" answered 401
    And "GET /healthz" answered 200

  Scenario: the embedded UI and an unknown path answer 401 without credentials too
    Given the board server is running on a free port
    When I request GET "/" without credentials
    Then the response status is 401
    When I request GET "/no/such/route" without credentials
    Then the response status is 401
