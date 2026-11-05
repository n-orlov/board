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
  `BOARD_CI_NPM_VOLUME` override the image and the volume names;
- when the docker socket exists (`/var/run/docker.sock`, or
  `BOARD_CI_DOCKER_SOCK`) it is bind-mounted into the sibling, the sibling joins
  the socket's group so its non-root uid can use it, and `host.docker.internal`
  resolves to the docker host. See the next section.

`ci/repocheck/runsh_test.go` runs the script against a stub `docker` and checks
that argv.

## The database matrix and the docker socket

The scenarios in `features/` run on two database legs, chosen by
`BOARD_DB_LEG=sqlite|postgres` (SQLite when unset):

    BOARD_GODOG_PATHS=health.feature BOARD_DB_LEG=postgres ci/run.sh go test -count=1 ./features/ -run TestFeatures

The Postgres leg starts a Postgres 16 container (pinned by digest in
`features/postgres_test.go`) with testcontainers-go through the docker socket
`ci/run.sh` mounted, so the container is a sibling of the toolchain container on
the host daemon, not a child of it:

- it publishes its port on the host, and the harness reaches it at
  `host.docker.internal` (`BOARD_CI_DOCKER_HOST`, which `ci/run.sh` sets while
  the socket is mounted);
- it carries the labels `ralphd.role=sibling`, `board.ci=postgres` and, when the
  caller runs inside a ralphd job, `ralphd.run=<run id>` (`BOARD_CI_RUN_ID`), so
  the job that owns the run can reap a leaked one without matching anything else;
- the testcontainers reaper is off (`TESTCONTAINERS_RYUK_DISABLED`): the harness
  terminates each container it started when the run, or the scenario that owns
  it, ends;
- one container serves the whole run, with a fresh empty database per scenario.
  A scenario tagged `@dedicated-database` gets a container of its own, so that
  `the database is stopped` cannot disturb another scenario. A scenario tagged
  `@postgres` runs on the Postgres leg only (the SQLite leg runs `~@postgres`).

`ci/suite.sh` runs the `features/` pass once per leg of `BOARD_CI_DB_LEGS`
(default `sqlite postgres`; SQLite is always first and keeps the file names it
had before the matrix). Each other leg gets the same fail-once-retry-once, a
scenario at a time, into files named for it: `features-run1-<leg>.log`,
`flaky-features-<leg>.txt` and `junit-features-<leg>-gotestsum.xml`. Both legs
write a merged JUnit, `junit-merged/junit-features-<leg>.xml`, kept apart so that
one scenario run on both legs is never read as a retry of itself, and
`ci/legcompare` reads them: a scenario green on one leg and red on another is
written to `leg-divergence.txt` and fails the suite. A scenario red on both legs
fails its legs, not the comparison, and one that only some legs run is not
compared. `ci/summary.sh` counts every leg. Allure covers the SQLite leg only:
the same scenario on a second leg would share a history id with it and read as
its retry.

`ci/suitecheck/legfixture_test.go` runs the real `ci/suite.sh` against
`testdata/legfixture/`, a `TestFeatures` that reads `BOARD_DB_LEG` and is red on
the leg `LEGFIXTURE_RED_ON` names, and requires the suite to fail and name the
scenario.

## Disposable-clone measurement: `ci/review.sh`

`ci/review.sh` clones the repository into the git-ignored `.review-clone/` and,
through `ci/run.sh`, asserts that the module path is `github.com/n-orlov/board`
and that `go list ./internal/...` resolves only to directories under the clone,
never the original checkout. With no argument (or `--smoke`) it stops there and
exits 0; any other arguments are passed to `go test` inside the same clone.
The clone is of the committed HEAD, so commit before running it.

## The Jenkinsfile

`Jenkinsfile` is the multibranch job `board/ci` (agent label
`linux && amd64 && docker`, `@Library('homelab')`, `disableConcurrentBuilds`, a
`git clean -ffdxq` workspace). Stages, in order:

| stage | command | runs |
|---|---|---|
| CI image | `ci/jenkins-image.sh` (cache volume `board-go-cache-jenkins`) | every build |
| Lint | `ci/run.sh ci/lint.sh` | every build |
| Suite | `ci/run.sh ci/suite.sh` | every build |
| Quality | `ci/run.sh ci/quality.sh ci-results` | every build |
| Stability | `ci/stability.sh 3` on the agent | extended lane only |
| Mutation | `ci/run.sh ci/mutation.sh` | every `main` build and the extended lane; other branches skip it |

The extended lane is the nightly `main` build (03:17) or a build with the
`EXTENDED` parameter: `-race`, fresh vulnerability databases, Stability and
Mutation. A red Suite, Quality, Stability or Mutation stage fails the build
and the later stages still run. `post` publishes the JUnit, Allure and CI
summary reports, archives `ci-results/` (the gremlins reports in
`ci-results/mutation/` included), archives `ci-results/release-candidate.json`
on a fully green `main` build and sends `notifyTelegram` last.
`ci/repocheck/jenkinsfile_test.go` pins the Mutation stage: last, after
Quality and Stability, on `main` and in the extended lane, running
`ci/run.sh ci/mutation.sh`. The Allure smoke stage, the README badges,
`Jenkinsfile.release` and deck's other Jenkinsfile probes land with task 011.
Quality's `mutation-go` row reads `ci-results/mutation/`, which the Mutation
stage writes after Quality, so on Jenkins that row fails until task 011
reconciles the two.

## Quality gates

Every SPEC section 15.4 gate is in `ci/quality.json` with `enabled: true` at
its threshold. A gate whose input does not exist yet fails; it never passes.

| key | threshold | state |
|---|---|---|
| `coverage` | 85 total, 80 per package, 50 fixture | runs |
| `crap` | ceiling 8, fixture ceiling 8 | runs |
| `crap_ts` | ceiling 8, no baseline | runs (needs `web-coverage/` from `ci/suite.sh` and `web/node_modules`) |
| `frontend_coverage` | 85 total, 80 per file (statements and lines) | runs (needs `web-coverage/coverage-summary.json` from `ci/suite.sh`) |
| `golangci` | zero findings | runs |
| `govulncheck` | zero reachable | runs |
| `trivy` | HIGH, CRITICAL on the source | runs |
| `trivy_image` | HIGH, CRITICAL on the image | fails until the image tar exists (task 011) |
| `mutation_go` | efficacy 80, `internal/domain` and `internal/store`, zero mutants fails | runs (fails until `ci/mutation.sh` has written `ci-results/mutation/`) |
| `stryker` | break 80, per module 80, `web/src/lib` | runs (fails until `ci/mutation.sh` has written `ci-results/mutation/stryker/mutation.json`) |
| `sbom_module` | CycloneDX of `go.mod`, non-empty | runs |
| `sbom_frontend` | CycloneDX of `web/package.json` and `web/package-lock.json` (dev dependencies included: all of the frontend's are), non-empty | runs |
| `sbom_image` | CycloneDX, non-empty | fails until the image tar exists |
| `openapi_lint`, `openapi_drift` | lint clean, no drift | runs (the same commands `ci/lint.sh` runs; fails when `api/openapi.yaml`, the generated code or `web/node_modules` is missing) |

`ci/quality.sh` exits non-zero until every row runs. Each row that fails for
a missing input names the input in the report.

## Pipeline tools and scripts

The Go tools under `ci/` have their own tests, and `ci/run.sh go test -count=1
./ci/...` runs them all:

- `ci/mutation.sh` and `ci/mutationgate`: the Go mutation gate, the command of
  the Jenkins Mutation stage (every `main` build and the extended lane). The
  script runs `gremlins unleash` over each package in `mutation_go.packages`
  (`internal/domain`, `internal/store`), writes one JSON report per package to
  `ci-results/mutation/` and then runs `mutationgate`, which reads the
  threshold from `ci/quality.json` and fails when
  - a package's efficacy (gremlins' definition, killed over killed plus lived)
    is below `mutation_go.efficacy` (80 %), or
  - a package generated no mutant, or judged none (all not covered or timed
    out): a gate that measured nothing has not passed (`fail_on_no_mutants`),
    or
  - a package's report is missing or unreadable.
  Counts come from the per-mutation statuses, since gremlins' summary fields
  omit timed-out and not-covered mutants. The script passes
  `--timeout-coefficient 60`: with a warm build cache gremlins' default bound
  timed out every `internal/store` mutant. `ci/quality.sh` evaluates the same
  reports as its `mutation-go` gate and fails when `ci-results/mutation/` is
  absent. Run it with `ci/run.sh ci/mutation.sh`, which is also the Jenkinsfile
  Mutation stage's command (see "The Jenkinsfile").
- The frontend gates (`ci/qualitycheck/frontend.go`, tests in `frontend_test.go`
  and in `web/scripts/*/*.test.ts`, which Vitest runs):
  - `web/scripts/crap/` scores every function of `web/src` (`*.ts`, tests
    excluded) as ESLint complexity against the Vitest v8 coverage in
    `coverage-final.json` and fails above `crap_ts.ceiling` (8), which the
    script reads from `ci/quality.json`. There is no baseline and no exemption
    list. Logic that a Svelte component needs belongs in a `src/lib` module,
    where it is scored, covered and mutated. Run it by hand with
    `ci/run.sh sh -c 'cd web && npm run test && npm run crap'`.
  - `frontend_coverage`: `coverage-summary.json` must show statements and lines
    at or above `total_floor` (85) over the frontend and `file_floor` (80) for
    every file; a summary that lists no file fails. Vitest measures
    `src/lib/**/*.ts` only (`web/vitest.config.ts`).
  - `stryker`: `ci/mutation.sh` also runs `npm run mutation` in `web/`
    (`web/stryker.config.mjs`: `src/lib`, `thresholds.break` 80). Stryker's own
    break judges the whole run; `web/scripts/mutation/` also fails any one module
    below `stryker.module_break` (80) and checks that the config's break and path
    equal `ci/quality.json`'s. The report is copied to
    `ci-results/mutation/stryker/mutation.json`, which `ci/quality.sh` judges.
    `web/scripts/mutation/vitestFileRunner.mjs` (from s3-browser) narrows a mutant
    run to the covering test files because Vitest 5 matches `-t` differently.
- `ci/covgate`: coverage floors from `ci/quality.json` (total, per package,
  fixture) over the merged profile. A package with no statements is named by
  exact path in `zeroStatementPackages`; a stale or missing entry fails.
- `ci/crapgate`: CRAP score (cyclomatic complexity against coverage) per
  function against the ceiling in `ci/quality.json`. There is no baseline file.
- `ci/legcompare`: compares per-leg JUnit and lists the scenarios that are
  green on one database leg and red on another (exit 1).
- `ci/qualitycheck`: the single quality-gate runner behind `ci/quality.sh`. It
  runs whichever gates `ci/quality.json` switches on (coverage, CRAP,
  golangci-lint, govulncheck, Trivy and the module SBOM run today; see the
  table below for the rest) and fails if a threshold is loosened against the
  merge-base copy of the file. A table-driven test (`completeness_test.go`)
  holds the real file to SPEC section 15.4 and proves that removing,
  disabling or loosening any gate is noticed.
- `ci/junit2allure`, `ci/junitflaky`, `ci/allureverify`: JUnit to Allure
  conversion, the flaky-test list, and the check that the Allure report keeps
  the Godog steps and issue links.
- `ci/suitecheck`: probes of `ci/suite.sh` against small fixture modules (the
  `BOARD_*` scrub, merged coverage, retry-once, timeouts, Allure results).
- `ci/lintcheck`: probes of `ci/lint.sh` (stubbed tools: every stage runs in
  order and a failing stage stops the rest) and of the golangci-lint config, plus
  one fixture per added checker (`fixtures_test.go`): ESLint, svelte-check, tsc,
  shellcheck, the OpenAPI lint and the generated-code drift check each pass on an
  unmodified copy of the tree and fail on one planted violation. They run the
  real tools, so they need `web/node_modules` (the test runs `npm ci` itself on a
  fresh checkout).
- `ci/repocheck`: repository probes. Besides the Dockerfile digest and
  `ci/run.sh` argv checks above, it asserts that go.mod carries a
  `toolchain go1.N.P` line naming a patched release no older than the `go`
  directive (`toolchain_test.go`), and that golangci-lint is one shared
  invocation switched on in `ci/quality.json`, while gofmt, `go vet` and
  `go mod tidy` run only in `ci/lint.sh` and in no other CI script or pipeline
  file (`lintgate_test.go`).

Scripts, each run through `ci/run.sh` unless noted:

- `ci/suite.sh`: `go test -p=1 -count=1 ./...` through gotestsum in two passes
  (every package but `features/TestFeatures`, then `TestFeatures`), with
  fail-once-retry-once and a `flaky-*.txt` record. It unsets every `BOARD_*`
  variable after reading its own `BOARD_CI_*` settings. It empties the unit and
  feature coverage-data directories first, so a rerun into the same results
  directory never merges stale counters. `BOARD_CI_OUT` or a
  positional argument names the results directory.
  When `web/package.json` exists it first runs `npm ci` and `npm run build` in
  `web/` (the build lands in `internal/webui/dist`, so the Go passes embed the
  real UI rather than the placeholder page), then `npm run test` (Vitest with
  `@vitest/coverage-v8`), writing `junit-vitest.xml`, `web-coverage/` and
  `flaky-vitest.txt` into the results directory. A failed Vitest run is
  repeated once: a pass on the repeat is recorded in `flaky-vitest.txt`, a
  second failure fails the suite. `ci/suitecheck/vitest_test.go` pins this.
  It then runs Playwright (`e2e/`, after `npm ci` there): the config starts the
  real binary on a free port against a freshly migrated, seeded SQLite store
  (`e2e/serve.sh`) and never retries (`retries: 0`). The retry-once is the
  suite's: a failed run is kept as `junit-playwright-run1.xml` and
  `playwright-run1.log`, repeated once, and a pass on the repeat is recorded in
  `flaky-playwright.txt`; a second failure fails the suite. `junit-playwright.xml`
  and the HTML, Allure and JSON output (`playwright/`) land in the results
  directory, and `ci/summary.sh` counts the JUnit and flaky files.
  `ci/suitecheck/playwright_test.go` pins this and fails if `retries` is not 0.
  One spec: `ci/run.sh sh -c 'cd e2e && npx playwright test smoke.spec.ts'`.
- `ci/quality.sh`: scrubs `BOARD_*` and runs `ci/qualitycheck` over
  `ci/quality.json` and the suite's merged coverage profile.
  **CRAP ceiling 8**, for product code and for fixtures alike; coverage floors
  are 85 % total, 80 % per package and 50 % per fixture. No gate may be
  switched off or loosened: `ci/qualitycheck` fails on a change that does.
  There is no baseline or exemption file. The CycloneDX files the SBOM gates
  write go into the results directory.
- `ci/lint.sh`: fail-fast, in this order: gofmt, `go vet`, `go mod tidy` drift,
  `ci/golangci.sh` (the golangci-lint invocation `ci/quality.sh` shares,
  configured by `.golangci.yml`; a `//nolint` must name its linter and give a
  reason), shellcheck over `git ls-files '*.sh'` (no ignore list), then, from
  `web/` (`npm ci` runs first when `web/node_modules` is missing), ESLint
  (`web/eslint.config.mjs`, `--max-warnings 0`), `svelte-check
  --fail-on-warnings`, `tsc --noEmit`, the Redocly lint of `api/openapi.yaml`
  (`web/redocly.yaml`: `recommended-strict`, every finding is an error, plus
  `operationId`, summary, description, licence and tag-description rules; the
  one rule left off is `operation-4xx-response`, because `GET /healthz` has no
  failing client path) and `ci/openapi-drift.sh`.
- `ci/openapi-gen.sh` writes the generated code from `api/openapi.yaml`:
  `internal/server/openapi.gen.go` (oapi-codegen, pinned by go.mod's `tool`
  directive, so no image change) and `web/src/lib/api/schema.d.ts`
  (openapi-typescript, pinned by `web/package-lock.json`; its `typescript` peer
  range is overridden in `web/package.json` because it predates TypeScript 6).
  `ci/openapi-drift.sh` regenerates both into a scratch directory and fails when
  either checked-in file differs or is missing. Run
  `ci/run.sh ci/openapi-gen.sh` after every edit of the document and commit the
  result. `web/src/lib/api` is excluded from Vitest coverage and Stryker: it is
  generated type declarations with no statements.
- `ci/stability.sh`: the suite N times, each run labelled by the exit status of
  `go test` itself.
- On the Jenkins agent, outside `ci/run.sh`: `ci/jenkins-image.sh` (builds
  `board-ci:local` and fixes the cache volume's owner), `ci/allure-report.sh`
  (pinned, checksum-verified Allure CLI), `ci/allure-smoke.sh`,
  `ci/jenkins-report.sh` and `ci/summary.sh` (the Allure report and CI summary
  pages).

## Known differences from deck

The `ci/` tools and scripts are carried from deck (`DECK_*` renamed to
`BOARD_*`, deck's module, image, volume and label names renamed to board's).
Every test in deck's copies of `ci/covgate`, `ci/crapgate`, `ci/qualitycheck`,
`ci/junit2allure`, `ci/junitflaky`, `ci/allureverify`, `ci/suitecheck`,
`ci/lintcheck` and `ci/repocheck` is carried, adapted to board's tree (CRAP
ceiling 8 instead of 10, the zero-statement list naming `features/`), except:

- **tmux-only tests.** Board has no TUI: `ci/tmuxguard/`, `ci/tmux-guard.sh`
  and the `tmux.version` assertion in deck's `ci/suitecheck/allure_test.go`
  are not carried.
- **install.sh tests, deferred to Phase 4.** deck's
  `ci/repocheck/installcheck_test.go` and
  `TestInstallScriptCurlIsHTTPSOnlyAndStatesItsChecksumLimit` (deck's
  `ci/allureverify/allureverify_test.go`) are not carried. `install.sh` is
  Phase 4 (SPEC §16), and its tests are rewritten then against SPEC §8.5's
  immutability check, not deck's checksum-only one.
- **Jenkinsfile tests, carried by task 011.** deck's
  `ci/repocheck/jenkinsfile_test.go` and
  `TestLintJobRunsGolangciLintBeforeTheSuite` (deck's
  `ci/repocheck/lintgate_test.go`) arrive with the Jenkinsfile itself, in the
  task that lands it (task 011), with the release assertion adapted to the
  placeholder `Jenkinsfile.release`. Until then `ci/repocheck/lintgate_test.go`
  carries deck's other two lint-gate probes.
