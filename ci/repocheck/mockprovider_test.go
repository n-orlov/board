// mockprovider_test.go keeps the mock OIDC provider test-only (SPEC 3.2): the
// release binary and so the release image must not hold it. `go list -deps`
// of cmd/board names every package the binary is built from.
package repocheck

import (
	"os/exec"
	"strings"
	"testing"
)

const mockProviderPackage = "github.com/n-orlov/board/internal/oidc/oidctest"

// goListDeps returns the import paths `go list -deps args...` prints.
func goListDeps(t *testing.T, args ...string) []string {
	t.Helper()
	root, err := repositoryRoot()
	if err != nil {
		t.Fatalf("repositoryRoot: %v", err)
	}
	cmd := exec.Command("go", append([]string{"list", "-deps"}, args...)...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -deps %s: %v", strings.Join(args, " "), err)
	}
	return strings.Fields(string(out))
}

// importsMockProvider reports whether deps include the mock provider.
func importsMockProvider(deps []string) bool {
	for _, d := range deps {
		if d == mockProviderPackage {
			return true
		}
	}
	return false
}

func TestReleaseBinaryDoesNotImportTheMockProvider(t *testing.T) {
	deps := goListDeps(t, "./cmd/board")
	if len(deps) < 10 {
		t.Fatalf("go list -deps ./cmd/board printed %d packages: the probe sees nothing", len(deps))
	}
	if importsMockProvider(deps) {
		t.Fatalf("cmd/board depends on %s: the mock provider must stay test-only", mockProviderPackage)
	}
}

// TestMockProviderProbeSeesATestImport proves the probe can fail: the Godog
// harness imports the mock provider, so listing its test dependencies finds it.
func TestMockProviderProbeSeesATestImport(t *testing.T) {
	if !importsMockProvider(goListDeps(t, "-test", "./features/")) {
		t.Fatalf("go list -deps -test ./features/ does not list %s: the probe cannot see the mock provider", mockProviderPackage)
	}
	if importsMockProvider([]string{"fmt", "os"}) || !importsMockProvider([]string{"fmt", mockProviderPackage}) {
		t.Fatal("importsMockProvider does not match exactly the mock provider's import path")
	}
}
