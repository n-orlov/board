Feature: API keys over HTTP
  Any authenticated principal can create, list and revoke API keys through
  /api/v1/keys. A key is returned once, by the response that creates it, and is
  stored nowhere: the database keeps its SHA-256 hash and a display prefix. A
  key expires at its expires_at, and the event of a creation records who
  authenticated (the principal) and who acted (the Board-Actor handle) in
  separate columns.

  Background:
    Given a new database for the board commands
    When I run "vh admin migrate"
    And I run "vh admin keys create workstation"
    And I remember the key stdout printed
    And the board server is running on a free port

  Scenario: a key created through the API works
    When I create the API key "laptop" as actor "claude@laptop" through the API with the remembered key
    Then the response status is 201
    And the response holds the new key exactly once
    When I request GET "/readyz" with the created key
    Then the response status is 200

  Scenario: a key revoked through the API then gets 401
    When I create the API key "laptop" as actor "claude@laptop" through the API with the remembered key
    And I request GET "/readyz" with the created key
    Then the response status is 200
    When I revoke the API key "laptop" through the API with the remembered key
    Then the response status is 200
    When I request GET "/readyz" with the created key
    Then the response status is 401
    And the problem detail is "key revoked"

  Scenario: a key past its expires_at gets 401 with key expired
    When I create the API key "laptop" expiring in "12h" as actor "claude@laptop" through the API with the remembered key
    Then the response status is 201
    When I request GET "/readyz" with the created key
    Then the response status is 200
    When the key "laptop" expired an hour ago
    And I request GET "/readyz" with the created key
    Then the response status is 401
    And the problem detail is "key expired"

  Scenario: the key is not retrievable after creation
    When I create the API key "laptop" as actor "claude@laptop" through the API with the remembered key
    And I request GET "/api/v1/keys" with the remembered key
    Then the response status is 200
    And the response does not contain the created key
    And the response lists the key "laptop" with a display prefix and no key field
    When I run "vh admin keys list"
    Then stdout does not contain the created key
    And the api_keys row named "laptop" holds the SHA-256 hash of the created key and nowhere the key itself

  Scenario: the event of a key creation records the principal and the Board-Actor handle separately
    When I create the API key "laptop" as actor "claude@laptop" through the API with the remembered key
    Then the key_created event for "laptop" has principal "key:workstation" in the principal column
    And the key_created event for "laptop" has actor "claude@laptop" in the actor column
