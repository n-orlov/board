# CI

This is the only CI document. It grows with the pipeline; this section covers
the toolchain image and how every command runs in it.

## Toolchain image

`ci/Dockerfile` builds `board-ci:local`:

- Go on the 1.26.x line, Node 24, and the Playwright 1.64.1 browsers
  (chromium, firefox, webkit) under `/ms-playwright`;
- gotestsum, golangci-lint, govulncheck, gremlins, shellcheck and trivy.

Every `FROM` and every `COPY --from` image is pinned by `@sha256:` digest, and
the tool versions are pinned in the file. `ci/repocheck/dockerfiledigest_test.go`
fails on an unpinned one. Build it once by hand:

    docker build -t board-ci:local -f ci/Dockerfile ci

## Running a command: `ci/run.sh`

`ci/run.sh <command...>` runs the command in a throwaway sibling container of
the image above, on the host docker daemon:

- the bind source is `$RALPHD_HOST_WORKSPACE` (a HOST path), else `$PWD`;
- the sibling runs as the caller's `uid:gid`, so it leaves no root-owned files;
- two named volumes carry the caches: `board-go-cache` (`/go-cache`: Go module,
  build and golangci-lint caches) and `board-npm-cache` (`/npm-cache`);
- every `BOARD_*` variable in the caller's environment is forwarded, and no
  other variable is. `BOARD_CI_IMAGE`, `BOARD_CI_CACHE_VOLUME` and
  `BOARD_CI_NPM_VOLUME` override the image and the volume names.

`ci/repocheck/runsh_test.go` runs the script against a stub `docker` and checks
that argv.

## Disposable-clone measurement: `ci/review.sh`

`ci/review.sh` clones the repository into the git-ignored `.review-clone/` and,
through `ci/run.sh`, asserts that the module path is `github.com/n-orlov/board`
and that `go list ./internal/...` resolves only to directories under the clone,
never the original checkout. With no argument (or `--smoke`) it stops there and
exits 0; any other arguments are passed to `go test` inside the same clone.
The clone is of the committed HEAD, so commit before running it.

## Pipeline tools and scripts

The Go tools under `ci/` have their own tests, and `ci/run.sh go test -count=1
./ci/...` runs them all:

- `ci/covgate`: coverage floors from `ci/quality.json` (total, per package,
  fixture) over the merged profile. A package with no statements is named by
  exact path in `zeroStatementPackages`; a stale or missing entry fails.
- `ci/crapgate`: CRAP score (cyclomatic complexity against coverage) per
  function against the ceiling in `ci/quality.json`. There is no baseline file.
- `ci/qualitycheck`: the single quality-gate runner behind `ci/quality.sh`. It
  runs whichever gates `ci/quality.json` switches on (coverage, CRAP,
  golangci-lint, govulncheck, Trivy) and fails if a threshold is loosened
  against the merge-base copy of the file.
- `ci/junit2allure`, `ci/junitflaky`, `ci/allureverify`: JUnit to Allure
  conversion, the flaky-test list, and the check that the Allure report keeps
  the Godog steps and issue links.
- `ci/suitecheck`: probes of `ci/suite.sh` against small fixture modules (the
  `BOARD_*` scrub, merged coverage, retry-once, timeouts, Allure results).
- `ci/lintcheck`: probes of `ci/lint.sh` and of the golangci-lint config.

Scripts, each run through `ci/run.sh` unless noted:

- `ci/suite.sh`: `go test -p=1 -count=1 ./...` through gotestsum in two passes
  (every package but `features/TestFeatures`, then `TestFeatures`), with
  fail-once-retry-once and a `flaky-*.txt` record. It unsets every `BOARD_*`
  variable after reading its own `BOARD_CI_*` settings. `BOARD_CI_OUT` or a
  positional argument names the results directory.
- `ci/quality.sh`: scrubs `BOARD_*` and runs `ci/qualitycheck` over
  `ci/quality.json` and the suite's merged coverage profile.
  **CRAP ceiling 8**, for product code and for fixtures alike; coverage floors
  are 85 % total, 80 % per package and 50 % per fixture. No gate may be
  switched off or loosened: `ci/qualitycheck` fails on a change that does.
- `ci/lint.sh`: gofmt, `go vet`, `go mod tidy` drift, then `ci/golangci.sh`
  (the golangci-lint invocation `ci/quality.sh` shares, configured by
  `.golangci.yml`; a `//nolint` must name its linter and give a reason).
- `ci/stability.sh`: the suite N times, each run labelled by the exit status of
  `go test` itself.
- On the Jenkins agent, outside `ci/run.sh`: `ci/jenkins-image.sh` (builds
  `board-ci:local` and fixes the cache volume's owner), `ci/allure-report.sh`
  (pinned, checksum-verified Allure CLI), `ci/allure-smoke.sh`,
  `ci/jenkins-report.sh` and `ci/summary.sh` (the Allure report and CI summary
  pages).
