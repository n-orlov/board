Feature: The Settings -> API keys page
  A person signed in through the OIDC provider revokes an API key with the
  page's Revoke button: the page calls the API with the session cookie and the
  CSRF token, and the key gets 401 from then on. A headless browser drives the
  page against the server the scenario started, with the web UI embedded.

  Background:
    Given a new database for the board commands
    When I run "board admin migrate"
    And I run "board admin keys create workstation"
    And I remember the key stdout printed
    And the board binary embeds the built web UI
    And the mock OIDC provider signs in "alice@example.com" with a verified email
    And the board server is running with OIDC login allowing "alice@example.com"

  Scenario: a key revoked on the Settings -> API keys page gets 401
    When I create the API key "agent" as actor "claude@laptop" through the API with the remembered key
    And I request GET "/readyz" with the created key
    Then the response status is 200
    When a headless browser signs in through the mock provider and revokes the key "agent" on the Settings -> API keys page
    And I request GET "/readyz" with the created key
    Then the response status is 401
    And the problem detail is "key revoked"
    And the key_revoked event for "agent" has principal "oidc:alice@example.com" in the principal column
