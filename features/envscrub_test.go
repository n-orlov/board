package features

import (
	"slices"
	"testing"
)

func TestScrubProductEnvDropsBoardAndVHButKeepsCIInternalAndOthers(t *testing.T) {
	in := []string{
		"PATH=/bin", "HOME=/home/x", "NOT_BOARD=1", "XBOARD_URL=1", "VH=1",
		"BOARD_URL=a", "BOARD_API_KEY=a", "BOARD_ACTOR=a", "BOARD_WAY_BACK=a", "BOARD_TASK=a", "BOARD_HOME=a",
		"VH_URL=a", "VH_API_KEY=a", "VH_ACTOR=a", "VH_WAY_BACK=a", "VH_TASK=a", "VH_REPO=a", "VH_VERSION=a",
		"BOARD_CI_DOCKER_HOST=h", "BOARD_CI_RUN_ID=r", "BOARD_GODOG_PATHS=p", "BOARD_DB_LEG=sqlite",
		"BOARD_MUTATION_LANE=l", "BOARD_IMAGE_TAR=i", "BOARD_ALLURE_X=a", "BOARD_FEATURES_COVERMODE=set",
	}
	want := []string{
		"PATH=/bin", "HOME=/home/x", "NOT_BOARD=1", "XBOARD_URL=1", "VH=1",
		"BOARD_CI_DOCKER_HOST=h", "BOARD_CI_RUN_ID=r", "BOARD_GODOG_PATHS=p", "BOARD_DB_LEG=sqlite",
		"BOARD_MUTATION_LANE=l", "BOARD_IMAGE_TAR=i", "BOARD_ALLURE_X=a", "BOARD_FEATURES_COVERMODE=set",
	}
	if got := scrubProductEnv(in); !slices.Equal(got, want) {
		t.Fatalf("scrubProductEnv =\n%v\nwant\n%v", got, want)
	}
}

func TestHarnessEnvScrubsTheTestProcessEnvironmentAndAppendsExtra(t *testing.T) {
	t.Setenv("BOARD_URL", "http://leaked.invalid")
	t.Setenv("VH_API_KEY", "leaked")
	t.Setenv("BOARD_CI_PROBE", "kept")
	got := (&harness{}).env("VH_URL=http://127.0.0.1:1")
	for _, kv := range got {
		if kv == "BOARD_URL=http://leaked.invalid" || kv == "VH_API_KEY=leaked" {
			t.Errorf("env leaks %q", kv)
		}
	}
	if !slices.Contains(got, "BOARD_CI_PROBE=kept") {
		t.Error("a CI-internal variable was dropped")
	}
	if got[len(got)-1] != "VH_URL=http://127.0.0.1:1" {
		t.Errorf("extra is not last: %v", got[len(got)-3:])
	}
}
