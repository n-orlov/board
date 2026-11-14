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

`Jenkinsfile` is the multibranch job `board/ci`
(`https://jenkins.wisent-owl.ts.net/job/board/job/ci/`; `@Library('homelab')`,
`agent none`, `disableConcurrentBuilds`, a 480-minute pipeline timeout). The
header comment of the file is the authoritative description. Every test stage is
a **unit**: its own parallel branch, with its own retry policy, that checks the
build's commit out into its own workspace on the one agent every unit uses
(`int21h-ws`, reached through the single label constant `AMD`, `board-test &&
amd64 && docker`, with 4 executors; no unit asks for an arm64 or any other
label), builds `board-ci:local` (`ci/jenkins-image.sh`, cached by the Docker
daemon; cache volumes `board-go-cache-jenkins` and `board-npm-cache-jenkins`,
each chowned to the agent's uid once), runs, and removes the sibling and
Testcontainers containers its attempt labelled (`ralphd.run=<attempt id>`, by
exact ID). The Mutation unit is no exception: `ci/mutation.sh` scrubs every `BOARD_*`
variable but hands `BOARD_CI_RUN_ID` (the attempt id `ci/run.sh` forwards from
`RALPHD_RUN_ID`) to gremlins, so the Postgres testcontainers of every mutant's
test run carry the attempt label and the cleanup reaps the ones a killed mutant
run leaves. An infrastructure failure (agent lost, Docker unavailable, a timeout)
gets another attempt, up to three; a failing command is rerun once, and a pass
there marks the build UNSTABLE ("flaky"). The build page carries a per-unit
timing table (`ci-results/distributed/timing.md`).

The pipeline has three phases, one after another; the units of a phase run in
parallel, and a phase runs only if every earlier one passed (UNSTABLE counts as
passed):

| phase | units | runs |
|---|---|---|
| Core | Lint (`ci/run.sh ci/lint.sh`), Suite (`ci/run.sh ci/suite.sh`), Image (`ci/image-build.sh` then `ci/image-check.sh`), Allure smoke (the Godog formatter fixture, then `ci/allure-smoke.sh`), and Quality (`ci/run.sh ci/quality.sh ci-results ci/quality.json --mutation-deferred`), which waits for Suite (its coverage) and Image (the tar) without holding an executor | every build |
| Stability | three units, each `ci/stability.sh 1` | `EXTENDED=true` or the nightly timer |
| Mutation | one unit, in one of two modes (below) | `MUTATION=true` or `MUTATION_FULL=true` or the nightly timer |

Three booleanParams select what runs: `EXTENDED`, `MUTATION` and
`MUTATION_FULL`. The nightly timer (`17 3 * * *`, on `main` only; every other
branch has no timer) counts as `EXTENDED` and `MUTATION_FULL` together.

- **Core** runs on every build, a push build of `main` included (about 20-35
  minutes). `EXTENDED=true` and the nightly timer also add `-race` (the Suite's
  limit goes from 45 to 120 minutes) and fresh vulnerability databases to Core.
- **Stability** is the extended lane: `EXTENDED=true` or the nightly timer.
- **Mutation** is one unit, gated on `MUTATION` or `MUTATION_FULL` or the
  timer and on nothing else: `EXTENDED=true` on its own runs no mutation.
  - *diff* (`MUTATION=true`): only the lines changed since a base, in one
    45-minute unit (`BOARD_MUTATION_DIFF=<base>`). The base is the merge base
    with `origin/<CHANGE_TARGET, or main>` (fetched into the workspace first,
    because the multibranch checkout fetches only the built branch), and `HEAD~1`
    on `main` itself, where the merge base would be `HEAD`. A package with no
    mutant passes; one with mutants keeps the 80 efficacy floor; Stryker runs
    only if `web/src/lib` changed.
  - *full* (`MUTATION_FULL=true` or the nightly timer on `main`): every package
    plus Stryker in one unit, `ci/run.sh ci/mutation.sh ci-results/mutation
    ci/quality.json all`, with `nproc / 2` gremlins workers at GOMAXPROCS 2 per
    worker (`BOARD_MUTATION_WORKERS`, `BOARD_MUTATION_TEST_CPU`), at `nice 10`,
    under one 6-hour limit and a single attempt (a timeout is not worth another
    6 hours). It takes about 70 minutes.

A red Lint, Suite, Image, Allure smoke, Quality, Stability or Mutation unit
fails the build. Within a phase the other units still finish, and a later phase
does not start after a failure.

**Allure smoke.** `features/allure_formatter_test.go` is board's Godog formatter
for Allure 2 (selected by `BOARD_GODOG_ALLURE=<absolute results dir>`, which
`ci/suite.sh` sets for every `features/` run, rerun included, so a rerun scenario
keeps its history id). `features/allure_formatter_fixture_test.go` runs a fixture
feature through it (a pass with an issue link, a failure with attachments, a skip
and a retried scenario). The unit runs that test with `BOARD_ALLURE_FIXTURE_OUT`
set, then `ci/allure-smoke.sh` runs the pinned Allure CLI over the results and
fails unless the report carries the fixture's steps and its `@gh-61` link.

**Quality and Mutation.** `ci/mutation.sh` writes the reports that `mutation-go`
and `stryker` read, in the Mutation phase, which comes after Core (Quality
included) and runs only when `MUTATION`, `MUTATION_FULL` or the timer asks for
it. So the Quality unit passes `--mutation-deferred` as `ci/quality.sh`'s third
argument: those two rows print "deferred to the Mutation unit" instead of
reading reports that cannot exist yet. The Mutation unit's `ci/mutation.sh`
judges the same reports against the same `ci/quality.json` thresholds (efficacy,
break, per-module break, no mutant fails) and fails the build below them.
Without the flag (`ci/run.sh ci/quality.sh`, the local recipe) the rows read
`ci-results/mutation/` and fail when it is missing. Every other gate runs in the
Quality unit on every build.

**The quality report format.** Every gate prints a section `=== <name> gate ===`
(its own human-readable body) that ends in exactly one machine-readable line,
`gate verdict: passed`, `gate verdict: failed` or `gate verdict: deferred`.
`ci/qualitycheck` derives it from the gate's result alone: a deferred gate (the
two mutation rows under `--mutation-deferred`) stays ok, so the exit status is
unchanged, but it is never reported as passed. The CI summary page
(`ci/jenkins-report.sh`) reads that line and nothing else, never a gate's
wording: `passed` is a green badge, `failed` a red one with the section open,
`deferred` a neutral grey one, and a section with no verdict line reads
`failed`. The quality tile reads `passed` when the report has at least one gate
and none failed (a deferred gate is not a failure), `failed` otherwise (an
empty or missing report included), so it agrees with `ci/quality.sh`'s exit
status. A deferred `mutation-go` or `stryker`
section says "not run in this build" when the build has no Mutation unit
results. When it has them (`ci-results/mutation/`, unstashed by the report step)
the section shows that unit's verdict for the gate, from
`ci-results/mutation/unit-verdict.txt` (`mutation-go: passed|failed`,
`stryker: passed|failed|not run`, written by `ci/mutation.sh` next to its
reports), and links to the archived reports.

**The image.** The root `Dockerfile` is multi-stage (the Svelte build, a static
`CGO_ENABLED=0` Go build, then `FROM scratch` with the binary, the CA
certificates, the time zone database embedded in the binary with
`-tags timetzdata`, `/data` and uid 65532), every `FROM` pinned by digest. Both
builder stages are `FROM --platform=$BUILDPLATFORM` and the Go build takes
`GOOS=$TARGETOS GOARCH=$TARGETARCH`, so linux/amd64 and linux/arm64 are built by
cross-compiling on the agent, without emulation: the final stage only `COPY`s.
`ci/image-build.sh` (on the agent, outside `ci/run.sh`: it needs the docker CLI)
does the build. The agent's docker has no buildx, so the script copies the buildx
plugin out of a digest-pinned `docker/buildx-bin` image into a throwaway docker
config, starts a digest-pinned `moby/buildkit` container as a `docker-container`
builder (removed on exit), builds both platforms into one OCI layout
(`ci-results/board-image-oci.tar`), builds amd64 again from the builder's cache and
loads it as `board:ci`, and saves it as `ci-results/board-image.tar`, which the
`trivy-image` and `sbom-image` gates scan. `ci/image-check.sh` then inspects what
was built: `go run ./ci/imagecheck` (through `ci/run.sh`) requires a manifest for
each platform, a non-root user, `/board` as the entrypoint, and an ELF `/board`
whose machine is that platform's (so an arm64 manifest holding the build host's
binary fails); `docker run board:ci version` runs the loaded image; a container of
it must answer `GET /healthz` with 200. Both run in the Image unit of the Core
phase, whose tar the Quality unit then scans with `ci/quality.sh`.
`ci/repocheck` fails when the final stage is not scratch or
distroless static, has a `RUN`, runs as root or sets no user, lacks the CA
certificates or tzdata, when a builder `FROM` lacks `--platform=$BUILDPLATFORM`, or
when the Go build does not cross-compile. Nothing is pushed: the registry push
belongs to the release job (Phase 4).

**Reports.** `post { always }` publishes the JUnit, Allure and CI summary reports,
declares the README badges, adds the build page summary line, and archives
`ci-results/` (the gremlins reports in `ci-results/mutation/` and the CycloneDX
SBOMs `ci-results/*.cdx.json` included). A fully green `main` build (checked in `post { always }`: `SUCCESS`, so an
UNSTABLE or failed build never qualifies) archives `ci-results/release-candidate.json`. `notifyTelegram` runs
last, in `post { cleanup }`.

- **Build page summary line** (badge plugin `addSummary`): pass/fail/skip/flaky
  counts and unit coverage, with links to the Allure and CI summary reports.
- **README badges** (`embeddable-build-status`): `build` (the plugin's own),
  `tests`, `coverage` and `quality`, which the Jenkinsfile declares on every
  build ("not run" until the build fills them in), served from
  `https://int21h.mooo.com/jenkins-static/board/ci/main/<id>.svg`. The README
  links each to the `main` job the way deck's do: `build` to `lastCompletedBuild/`,
  `tests` to `lastCompletedBuild/Allure/`, `coverage` and `quality` to
  `lastCompletedBuild/CI_20summary/` (the CI summary report), all under
  `https://jenkins.wisent-owl.ts.net/job/board/job/ci/job/main/`.

`ci/repocheck/jenkinsfile_test.go` pins all of it: the three phases Core,
Stability and Mutation in order, with the Core units and Quality's wait for
Suite and Image; the job shape (the one `AMD` label for every `node()`, the three
booleanParams, `disableConcurrentBuilds`, the 480-minute timeout, the
per-attempt container cleanup by exact ID); the nightly lane (cron `17 3 * * *`
on `main` only); the Quality unit; the two Mutation modes (diff: one 45-minute
unit over the lines changed since the merge base, `HEAD~1` on `main`; full: one
unit over lane `all`, 360 minutes, one attempt, the worker split) and that
Mutation runs only for `MUTATION`, `MUTATION_FULL` or the timer; that the job
never releases; the reports, the badges and `notifyTelegram`. Each property has
a case that feeds the check a copy of the Jenkinsfile with it removed or
altered, so a test fails if it is lost. `ci/repocheck/lintgate_test.go` pins that
Lint is the first Core unit and runs `ci/lint.sh`, and that a red unit fails the
build.

## Release

`Jenkinsfile.release` is the pipeline job `board/release`, started by hand, and
a placeholder until the release phase. Its parameters are `VERSION`
(`vMAJOR.MINOR.PATCH[-pre]`, required), `CI_BUILD` (a `board/ci` `main` build
number, empty for the last successful one) and `DRY_RUN`. Candidate validates both
against the release candidate that build archived in
`ci-results/release-candidate.json` (a build that was not fully green has none, so
the copy fails), Checkout checks the candidate commit out, and the job stops: it
builds nothing, publishes nothing, creates no tag, holds no credential and
behaves as a dry run whatever `DRY_RUN` says. `ci/repocheck/jenkinsfile_test.go`
fails if it gains a Build or Publish stage, a credential, a push or `gh release`,
or loses the validation.

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
| `trivy_image` | HIGH, CRITICAL on the image | runs (`trivy image --input ci-results/board-image.tar`; fails when the tar, which `ci/image-build.sh` writes, is missing or empty) |
| `mutation_go` | efficacy 80 per package, `internal/domain`, `internal/store` and the Phase 2 application logic (see below), zero mutants fails | runs (fails until `ci/mutation.sh` has written `ci-results/mutation/`; with `--mutation-deferred` the Jenkins Mutation phase judges it) |
| `stryker` | break 80, per module 80, `web/src/lib` | runs (fails until `ci/mutation.sh` has written `ci-results/mutation/stryker/mutation.json`; with `--mutation-deferred` the Jenkins Mutation phase judges it) |
| `sbom_module` | CycloneDX of `go.mod`, non-empty | runs |
| `sbom_frontend` | CycloneDX of `web/package.json` and `web/package-lock.json` (dev dependencies included: all of the frontend's are), non-empty | runs |
| `sbom_image` | CycloneDX, non-empty | runs (from the image tar; fails when it is missing) |
| `openapi_lint`, `openapi_drift` | lint clean, no drift | runs (the same commands `ci/lint.sh` runs; fails when `api/openapi.yaml`, the generated code or `web/node_modules` is missing) |

`ci/quality.sh` exits non-zero when any row fails. Each row that fails for a
missing input names the input in the report.

## Pipeline tools and scripts

The Go tools under `ci/` have their own tests, and `ci/run.sh go test -count=1
./ci/...` runs them all:

- `ci/mutation.sh` and `ci/mutationgate`: the Go mutation gate, the command of
  the Jenkins Mutation unit (only with `MUTATION=true`, `MUTATION_FULL=true` or the
  nightly timer; a push build of `main` skips it). The
  script runs `gremlins unleash` over each package in `mutation_go.packages`
  (`internal/domain`, `internal/store`, and the application logic Phase 2
  introduced or changed: `internal/asks`, `internal/attention`,
  `internal/audit`, `internal/checkin`, `internal/cursors`, `internal/events`,
  `internal/feed`, `internal/notes`, `internal/tasks`, `internal/server`,
  `internal/cli`), writes one JSON report per package to
  `ci-results/mutation/` and then runs `mutationgate`, which reads the
  threshold from `ci/quality.json` and fails when
  - a package's efficacy (gremlins' definition, killed over killed plus lived)
    is below `mutation_go.efficacy` (80 %), or
  - a package generated no mutant, or judged none (all not covered or timed
    out): a gate that measured nothing has not passed (`fail_on_no_mutants`),
    or
  - a package's report is missing or unreadable.

  Diff mode (`BOARD_MUTATION_DIFF=<commit>`, read before the `BOARD_*` scrub;
  `ci/mutationgate -diff <commit>`) mutates only the lines changed since the
  merge base with that commit: units with no measured file changed are not run
  and pass, gremlins gets `--diff` (through a `git` shim that adds
  `--relative=<package dir>`, because gremlins 0.6.0 compares root-relative
  diff paths with package-relative file names and would skip every mutant),
  and a unit with no mutant or no judged mutant passes while one with mutants
  keeps the 80 % floor. Stryker runs only if `web/src/lib` changed.
  `BOARD_MUTATION_WORKERS` sets gremlins' workers and `BOARD_MUTATION_TEST_CPU`
  each worker's GOMAXPROCS (gremlins' own `--test-cpu` passes `-cpu N` as one
  argument, which fails every test run and counts every mutant killed).

  `internal/server`, `internal/cli` and `internal/tasks` hold older Phase 1
  code beside Phase 2's, and a gremlins run of a package re-runs that
  package's whole test suite for every mutant, so each is narrowed to the
  files Phase 2 introduced or changed, listed under `mutation_go.scopes` in
  `ci/quality.json` (a listed file is mutated whole, its older lines
  included). `mutation_go.phase_base` is the commit Phase 2 started from:
  `ci/mutationgate -unmeasured` runs `git diff` against it and prints every
  non-test `.go` file under `internal/` or `cmd/` changed since then that no
  unit measures (generated files, marked `Code generated ... DO NOT EDIT.`,
  are left out), exiting 1 if there is one, and a test of the shipped config
  requires none, so a Phase 2 file left out of the scope fails the Suite.
  `ci/mutationgate -excludes <pkg>`
  prints one `--exclude-files` regexp per other non-test file, and
  `ci/mutation.sh` passes them to gremlins (a scoped file that does not exist
  fails). A gremlins run of `internal/server` re-runs a large suite for every
  mutant, so `mutation_go.parts` splits its scope into three disjoint
  parts (`ops`, `streams` and `core`), and `internal/cli`'s into two (`views`
  and `core`), each set together exactly its scope, which `ci/mutationgate`
  checks: each part is a unit `internal/server#ops` that gremlins runs with
  every file outside the part excluded, with its own report
  (`internal-server-ops.json`) and its own judgement against the same floor.
  `mutation_go.lanes` partitions the packages' units into lanes (a unit in no
  lane, or two, fails `ci/mutationgate`). Jenkins no longer runs a lane per
  stage: its full Mutation unit runs lane `all`, every unit of every package plus
  Stryker, in one go. A lane is the way to run part of it by hand:
  `ci/mutation.sh [outdir [config [lane]]]` runs one lane (default `all`; Stryker
  also runs in `base`) and `ci/mutationgate -lane` judges only that lane's
  reports; every lane writes into `ci-results/mutation/`, where `ci/quality.sh`
  judges them all. gremlins takes a
  package's per-mutant bound from the time of its covered `go test` run, which
  go's test cache can answer in milliseconds, so `ci/mutation.sh` runs with
  `GOFLAGS=-count=1`: the covered run is a real run, and `--timeout-coefficient
  60` multiplies the suite's real duration (without it every `internal/server`
  and `internal/cli` mutant timed out unjudged). The efficacy floor, `--timeout-coefficient 60` and the worker count
  (`nproc / 2` workers at GOMAXPROCS 2 in Jenkins) apply to every package alike; nothing is raised for the
  scoped ones. `ci/qualitycheck` reports a scope narrowed against the base as a
  loosening, like a dropped package.
  Counts come from the per-mutation statuses, since gremlins' summary fields
  omit timed-out and not-covered mutants. The script passes
  `--timeout-coefficient 60`: with a warm build cache gremlins' default bound
  timed out every `internal/store` mutant. `ci/quality.sh` evaluates the same
  reports as its `mutation-go` gate and fails when `ci-results/mutation/` is
  absent. Run it with `ci/run.sh ci/mutation.sh`, which is also the Jenkinsfile
  Mutation unit's command (see "The Jenkinsfile").
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
  runs whichever gates `ci/quality.json` switches on (every row of the table
  above) and fails if a threshold is loosened against the
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
  The feature passes get `GOCOVERDIR` and `BOARD_FEATURES_COVERMODE` (the unit
  pass's mode, `atomic` under `-race`): the Godog harness then builds the real
  board binary with `-cover -covermode=<mode>` (`features/harness_test.go`), and
  stops a scenario's server with SIGTERM so its counters are flushed. The suite
  merges those counters with the unit pass's into `coverage-merged.out`. A
  database leg whose log shows scenarios ran but that left no non-empty counter
  file in the feature coverage directory fails the suite and is named, as does a
  run whose feature profile has no blocks; a unit-only merged profile is never
  accepted. `features/coverage_test.go` pins the build line, the emitted
  counters and the server flush; `ci/suitecheck` probes the suite's refusal per leg.
  When `web/package.json` exists it first runs `npm ci` and `npm run build` in
  `web/` (the build lands in `internal/webui/dist`, so the Go passes embed the
  real UI rather than the placeholder page), then `npm run test` (Vitest with
  `@vitest/coverage-v8`), writing `junit-vitest.xml`, `web-coverage/` and
  `flaky-vitest.txt` into the results directory. A failed Vitest run is
  repeated once: a pass on the repeat is recorded in `flaky-vitest.txt`, a
  second failure fails the suite. `ci/suitecheck/vitest_test.go` pins this.
  It then starts Playwright (`e2e/`, after `npm ci` there) in the background, beside
  the Go passes and the Godog passes, which share nothing with it (one worker, a free
  port and a SQLite store of its own); the script waits for it before it merges the
  JUnit files, replays its messages to stderr and fails the suite on its failure. The
  overlap keeps the Suite stage inside its 45-minute budget, which is unchanged, and
  shortens no spec or bound. The config starts the
  real binary on a free port against a freshly migrated SQLite store
  (`e2e/serve.sh`) and never retries (`retries: 0`). It has two projects over
  that one store, `chromium-early` first: `shell.spec.ts`, the `timeline*.spec.ts`
  specs and `wide.spec.ts`, which load the unfiltered Timeline again and again, then
  `chromium` with every other spec. The store only grows, and an unfiltered Timeline
  takes seconds to draw once it holds thousands of tasks; run last, those specs failed
  the first run on the agent and the repeat took the Suite past its bound.
  `ci/suitecheck` fails if that order is lost. Only `GET /healthz` is open,
  so `e2e/global-setup.ts` mints a key with `board admin keys create` at test
  time (no key is committed) and the `test` exported by `e2e/fixtures.ts`
  sends it, with a `Board-Actor` header, on every request; every spec imports
  `test` from there. The retry-once is the
  suite's: a failed run is kept as `junit-playwright-run1.xml` and
  `playwright-run1.log`, repeated once as a whole run (every spec runs again, not only
  the failed ones, unlike the Go and Godog reruns), and a pass on the repeat is recorded in
  `flaky-playwright.txt`; a second failure fails the suite. `junit-playwright.xml`
  and the HTML, Allure and JSON output (`playwright/`) land in the results
  directory, and `ci/summary.sh` counts the JUnit and flaky files.
  `ci/suitecheck/playwright_test.go` pins this and fails if `retries` is not 0.
  `e2e/serve.sh` also builds and starts `cmd/fake-oidc` (the mock OIDC provider,
  test-only, signing in `operator@example.test`) and starts the binary with browser
  login against it (`BOARD_OIDC_*`, `BOARD_PUBLIC_URL`; the client secret is made
  up per run). `e2e/login.spec.ts` imports Playwright's own `test` rather than
  fixtures.ts: it sends no key, the session cookie is its only credential.
  `serve.sh` builds the UI only when `internal/webui/dist` is missing, so after
  changing `web/` locally run `npm run build` there first (the suite always does).
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
  `internal/server/openapi.gen.go` (oapi-codegen in `std-http,strict-server,models`
  mode, pinned by go.mod's `tool` directive, so no image change) and `web/src/lib/api/schema.d.ts`
  (openapi-typescript, pinned by `web/package-lock.json`; its `typescript` peer
  range is overridden in `web/package.json` because it predates TypeScript 6).
  `.golangci.yml` lints generated Go like any other file, so the script
  rewrites the two kinds of line oapi-codegen emits that errcheck,
  staticcheck and gosec reject (an unchecked `Close`, a
  `Write([]byte(fmt.Sprint(...)))`) and fails when the generator stops emitting
  one, so a generator upgrade cannot leave a stale rewrite behind. It makes a
  third rewrite for the CRAP gate (ceiling 8, generated code scored like any
  other): the six-line block oapi-codegen repeats for every query parameter
  that cannot be bound becomes one call of `bindFailed` in
  `internal/server/params.go` (the same two answers), and the now unused
  `errors` import is dropped. A fourth rewrite folds each run of those calls
  into `bind.check(name, runtime.Bind...)` on a `queryBinder`
  (`internal/server/params.go`) that answers only the first failure, closed by
  one `if bind.failed { return }`, so an operation with nine filters
  (`listEvents`) has the complexity of one branch and stays under the ceiling.
  `ci/openapi-drift.sh` regenerates both into a scratch directory and fails when
  either checked-in file differs or is missing. Run
  `ci/run.sh ci/openapi-gen.sh` after every edit of the document and commit the
  result. `web/src/lib/api` is excluded from Vitest coverage and Stryker: it is
  generated type declarations with no statements.
- `ci/image-build.sh [tar]`: builds the root `Dockerfile` for linux/amd64 and
  linux/arm64 (cross-compiled, no emulation, nothing pushed), loads amd64 as
  `board:ci` and writes `ci-results/board-image.tar` and
  `ci-results/board-image-oci.tar` (the Jenkins Image unit; on the agent). It passes the
  image's `VERSION` and `REVISION` build args (`BOARD_IMAGE_VERSION`, `BOARD_IMAGE_REVISION`, set by
  the Jenkinsfile to the commit's tag or short sha and the full commit), which the binary reports in
  `board version` and `GET /api/v1/meta`.
- `ci/image-check.sh [dir]`: runs `ci/imagecheck` over the OCI layout, `board version`
  and a `/healthz` probe against `board:ci`.
- `ci/imagecheck`: the Go tool that reads the OCI layout (both manifests, user,
  entrypoint, the ELF machine of `/board`).
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
- **Jenkinsfile tests, carried here.** deck's `ci/repocheck/jenkinsfile_test.go`
  and its `TestLintJobRunsGolangciLintBeforeTheSuite` (deck's
  `ci/repocheck/lintgate_test.go`, here `TestLintUnitRunsGolangciLintBeforeLaterPhases`)
  are carried against board's `Jenkinsfile`, renamed (`DECK_*` to `BOARD_*`, the
  `deck/` jobs to `board/`) and rewritten for its three phases (Core, Stability,
  Mutation) and parallel units. Board's pipeline adds Mutation and the image
  build, and drops the tmux-only parts. deck's release
  assertion, `TestJenkinsfileReleasePublishesTheCandidate`, is adapted to the
  placeholder `Jenkinsfile.release` as
  `TestJenkinsfileReleaseValidatesTheCandidateAndStops`: it asserts the `VERSION`
  and `CI_BUILD` validation against the candidate, that the job has no behaviour
  beyond a dry run and publishes nothing (no Build or Publish stage, no tag push,
  no `gh release`, no credential), and that it fails if `Jenkinsfile.release`
  publishes or drops that validation. deck's own Build, Publish, Version check
  and `ci/release-build.sh` assertions arrive with the real release job.
