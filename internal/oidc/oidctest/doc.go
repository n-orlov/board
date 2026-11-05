// Package oidctest is the mock OpenID Connect provider the tests start. It is
// test-only: cmd/board never imports it (ci/repocheck checks `go list -deps`),
// so the release image holds no mock provider. Its signing key is generated
// when a Provider is created, and no key is ever committed.
package oidctest
