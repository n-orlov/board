Feature: With browser login on, only /healthz needs no credential
  Browser login adds /auth/login and /auth/oidc/callback, which answer a browser
  navigation, but an API call (no Accept: text/html) to any operation with no
  credential still gets 401.

  Scenario: with login on, a credential-less API call to any operation is still 401
    Given the mock OIDC provider signs in "alice@example.com" with a verified email
    And the board server is running with OIDC login allowing "alice@example.com"
    When I send every operation declared in api/openapi.yaml with no Authorization header and no cookie
    Then every operation except "GET /healthz" answered 401
    And "GET /auth/login" answered 401
    And "GET /auth/oidc/callback" answered 401
    And "GET /auth/session" answered 401
    And "POST /auth/logout" answered 401
    And "GET /healthz" answered 200
