Feature: Browser login through OpenID Connect
  People log in through a mock OIDC provider the harness starts: no external
  identity provider is contacted. The provider signs in whoever the scenario
  names and checks that the board's requests use PKCE S256, state and nonce.
  A session is the only thing a successful login creates, so every rejection
  also checks that no session row exists.

  Scenario: an allowed email logs in and acts as the operator
    Given the mock OIDC provider signs in "alice@example.com" with a verified email
    And the board server is running with OIDC login allowing "alice@example.com"
    When the browser opens "/"
    Then the response redirects to "/auth/login"
    When the browser opens "/auth/login"
    Then the response redirects to the mock provider's authorization endpoint
    And the browser holds a pre-login cookie
    When the browser opens "/"
    And the browser follows the redirects
    Then the response status is 200
    And the provider received an authorization request with PKCE method S256, a state and a nonce
    And the provider received a token request whose code verifier matches the code challenge
    And the browser holds a session cookie
    And the database holds 1 session stored only as the SHA-256 hash of the session cookie
    When the browser requests GET "/auth/session"
    Then the response status is 200
    And the session response is for "alice@example.com" acting as "operator"
    When the browser requests GET "/readyz"
    Then the response status is 200

  Scenario: an email that is not on the allowed list gets 403
    Given the mock OIDC provider signs in "mallory@example.com" with a verified email
    And the board server is running with OIDC login allowing "alice@example.com"
    When the browser opens "/"
    And the browser follows the redirects
    Then the response status is 403
    And the browser holds no session cookie
    And the database holds 0 sessions

  Scenario: an unverified email gets 403 even when it is on the allowed list
    Given the mock OIDC provider signs in "alice@example.com" with an unverified email
    And the board server is running with OIDC login allowing "alice@example.com"
    When the browser opens "/"
    And the browser follows the redirects
    Then the response status is 403
    And the browser holds no session cookie
    And the database holds 0 sessions

  Scenario: a bad state is rejected
    Given the mock OIDC provider signs in "alice@example.com" with a verified email
    And the board server is running with OIDC login allowing "alice@example.com"
    When the browser opens "/"
    And the browser follows the redirects but replaces the callback's state with "forged-state"
    Then the response status is 400
    And the response is a problem document saying "state"
    And the provider received no token request
    And the browser holds no session cookie
    And the database holds 0 sessions

  Scenario: a reused nonce is rejected
    Given the mock OIDC provider signs in "alice@example.com" with a verified email
    And the board server is running with OIDC login allowing "alice@example.com"
    When the browser opens "/"
    And the browser follows the redirects
    Then the response status is 200
    Given the provider signs its next ID token with the nonce of the previous login
    And the browser forgets its cookies
    When the browser opens "/"
    And the browser follows the redirects
    Then the response status is 400
    And the response is a problem document saying "nonce"
    And the browser holds no session cookie
    And the database holds 1 session

  Scenario: a callback without the PKCE verifier is rejected
    Given the mock OIDC provider signs in "alice@example.com" with a verified email
    And the board server is running with OIDC login allowing "alice@example.com"
    Given the browser discards the pre-login cookie it is given
    When the browser opens "/auth/login"
    And the browser follows the redirects
    Then the response status is 400
    And the response is a problem document saying "PKCE verifier"
    And the provider received no token request
    And the browser holds no session cookie
    And the database holds 0 sessions

  Scenario: a tampered ID token is rejected
    Given the mock OIDC provider signs in "mallory@example.com" with a verified email
    And the provider rewrites the email of the ID tokens it signs to "alice@example.com"
    And the board server is running with OIDC login allowing "alice@example.com"
    When the browser opens "/"
    And the browser follows the redirects
    Then the response status is 401
    And the response is a problem document saying "ID token"
    And the browser holds no session cookie
    And the database holds 0 sessions
