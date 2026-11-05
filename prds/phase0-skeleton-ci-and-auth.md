# Phase 0: skeleton, full CI/CD and authentication

## What the operator gets

1. A `board` binary that serves an authenticated HTTP API, Swagger UI and an embedded Svelte UI, and works as
   its own CLI, on SQLite or Postgres.
2. One working feature end to end: initiatives (create, list, get) through the API, the CLI and the UI, signed
   in with OIDC (people) or an API key (agents).
3. `board help agent`, the in-binary agent protocol, at version v0.
4. A multi-arch container image built by CI.
5. The deck-equivalent Jenkins pipeline with **every** quality gate enforced from the first green build. Every
   later phase is built and judged on this pipeline. If a gate is weak here, every later phase is unverifiable.

`SPEC.md` is the product spec. This PRD implements exactly SPEC §16 "Phase 0". Where this PRD and `SPEC.md`
disagree, **this PRD wins**, and the spec is corrected in the same commit (R16).

## Context the run needs

- The repository holds `SPEC.md`, `README.md` and this PRD. There is no code, no `ci/` and no toolchain yet.
- **The reference implementation of the pipeline is deck** (github.com/n-orlov/deck). Clone it read-only into
  a git-ignored scratch directory (for example `.ref/deck`) and copy its mechanisms, renaming `DECK_*` to
  `BOARD_*`:
  - the `Jenkinsfile`;
  - the `ci/` scripts: `Dockerfile`, `run.sh`, `jenkins-image.sh`, `lint.sh`, `golangci.sh`, `suite.sh`,
    `quality.sh`, `quality.json`, `qualitycheck/`, `crapgate/`, `covgate/`, `junit2allure/`, `junitflaky/`,
    `allure-report.sh`, `allure-smoke.sh`, `allureverify/`, `jenkins-report.sh`, `summary.sh`, `stability.sh`,
    `review.sh`;
  - `docs/ci.md`.

  Do not reinvent what deck already solves.
- **s3-browser** (github.com/n-orlov/s3-browser, same access) is the reference for the frontend gates:
  - `playwright.config.ts` and `e2e/`;
  - `stryker.config.mjs`;
  - `scripts/crap/`.

  Do not use its CRAP baseline file.
- **The toolchain runs in a sibling container.** The job container has no Go, Node or Postgres. Build
  `ci/Dockerfile` and `ci/run.sh` first, on deck's model:
  - the bind source is `$RALPHD_HOST_WORKSPACE`;
  - the sibling runs as the caller's uid/gid;
  - caches live in shared named volumes;
  - the caller's `BOARD_*` variables are forwarded.

  The image adds Node 24 and the Playwright browsers to deck's Go toolchain. Pin every tool and every `FROM`
  by digest.
- **The Postgres leg uses testcontainers-go from inside the sibling.** `ci/run.sh` therefore mounts the
  docker socket into the sibling and makes the started containers reachable from it. This is the known
  hazard of this phase: solve it in `ci/run.sh` once, and test it with the matrix itself.

Symbols and section numbers refer to `SPEC.md` at the commit that adds this PRD. Drift is expected and is not a
finding: if a reference has drifted, find the current site and carry on.

## Issues in scope

None filed. This PRD is the whole statement.

**Not in scope:**

- tasks, leases, resources, conflicts, asks, notes, cursors, the attention view, the timeline, SSE;
- `board now`, the CLI shortcuts, `install.sh`, `INSTALL-AGENT.md`, `board update`;
- a real release, a real deployment;
- any change to the homelab Jenkins configuration (the job exists, see Standing rules).

## Requirements

Every requirement names its test. Each test fails at the parent of the commit that makes it pass.
"Both databases" means the Godog scenario runs on the SQLite leg and on the Postgres leg of the matrix.

### Part A: foundation

- **R1 (toolchain and module).**
  - `ci/Dockerfile`, `ci/run.sh` and `ci/review.sh` exist. `review.sh` is deck's disposable-clone protocol,
    asserting that `go list` resolves `./internal/...` inside the clone.
  - Module `github.com/n-orlov/board`, using the SPEC §16 layout.
  - `ci/run.sh go build ./...` and `ci/run.sh go vet ./...` are clean.
  - Test: the Lint stage runs, and its scripts run in Suite.
- **R2 (one binary).** `board serve` starts the server. `board version` prints the version stamped from build
  info, and `-dirty` for a modified tree. Unknown subcommands exit 2 with a one-line usage error. Tests: a Godog
  scenario `cli_basics.feature` for version and usage; unit tests for the version stamping.
- **R3 (health).**
  - `/healthz` answers 200 without credentials.
  - `/readyz` answers 200 only when the database is reachable and migrated, and 503 otherwise.
  - Test: a scenario on both databases, including a stopped database (Postgres leg) and an unmigrated store
    with `BOARD_MIGRATE=off`.
- **R4 (store).** SPEC §12.2:
  - goose migrations are embedded, and `board admin migrate` applies them;
  - the dialect layer covers SQLite (`modernc`) and Postgres (`pgx`);
  - SQLite runs with WAL, `busy_timeout=5000`, `foreign_keys=ON` and `synchronous=NORMAL`, using one writer
    connection and a read pool;
  - the Phase 0 tables are `actors`, `initiatives`, `initiative_tags`, `events`, `api_keys` and `sessions`,
    with the SPEC §4 columns that this phase uses, including `n` short-ref columns and `protocol_seen`.

  Tests:
  1. migrate from empty on both databases;
  2. a store whose schema is newer than the binary refuses to start, with a one-line error;
  3. the SQLite pragmas are read back;
  4. the dialect builders are unit-tested.
- **R5 (OpenAPI and Swagger).**
  - `api/openapi.yaml` (3.1) covers every Phase 0 operation.
  - The server uses oapi-codegen's strict server. The TS client comes from `openapi-typescript`.
  - Lint fails on generated-code drift and on spec lint errors.
  - `/api/openapi.yaml` and `/api/openapi.json` are served. Swagger UI is at `/api/docs`, with embedded
    assets and no CDN.

  Tests:
  - a Godog scenario: every OpenAPI operation, probed without credentials, returns 401 except `/healthz`
    (SPEC §15.2 scenario 6);
  - Playwright: Swagger UI renders the operation list.

### Part B: authentication (SPEC §3)

- **R6 (OIDC for people).**
  - Generic OIDC: PKCE S256, state and nonce, and the fixed callback `/auth/oidc/callback`.
  - `email_verified` is required. An email in `BOARD_OIDC_ALLOWED_EMAILS` maps to the operator actor.
  - The session is an HttpOnly, Secure, SameSite=Lax cookie, stored hashed server-side, with idle and
    absolute expiry.
  - CSRF uses `X-CSRF-Token` plus an Origin check.
  - Logout invalidates the session.

  A **mock OIDC provider** that the test harness starts (a test-only binary or package, never in the release
  image) backs every test. CI never contacts an external identity provider. Tests: SPEC §15.2 scenario 6's
  OIDC cases, one scenario each, on both databases. The cases are:
  1. an allowed email logs in;
  2. a disallowed email gets 403;
  3. an unverified email gets 403;
  4. a bad `state` is rejected;
  5. a reused `nonce` is rejected;
  6. a missing PKCE verifier is rejected;
  7. a tampered ID token is rejected;
  8. a cookie write without a CSRF token is rejected;
  9. a cookie write with a foreign `Origin` is rejected;
  10. logout invalidates the session;
  11. idle expiry;
  12. absolute expiry.

  Playwright: log in through the mock provider and see the initiatives page.
- **R7 (API keys for agents).** Keys follow SPEC §3.3: `brd_` plus 32 random bytes in base32, stored as
  SHA-256, shown once, and sent as Bearer.
  - `board admin keys create|list|revoke` works directly on the database (`BOARD_DB_URL`).
  - `GET/POST /api/v1/keys` and `POST /keys/{name}/revoke` exist.
  - The UI has a Settings → API keys page: list, create (the key is shown once), revoke.
  - Events record `principal` and actor separately.

  Tests, scenarios on both databases:
  1. create, and the key works;
  2. revoke through the UI, the API and the admin CLI, and the key then gets 401;
  3. an expired key gets 401;
  4. the key is never retrievable after creation (not in list output, the API or the database);
  5. the `principal` and actor columns on events.

  Playwright covers the keys page.

### Part C: the vertical slice

- **R8 (initiatives API).** These SPEC §6 operations:
  - `POST /api/v1/initiatives`, with the slug validated as `[a-z0-9-]{2,40}`. A duplicate slug returns 409
    with `next`.
  - `GET /api/v1/initiatives`, with tags and pagination (`limit`, `after=<ulid|ref>`).
  - `GET /api/v1/initiatives/{slug}`.

  Every write appends an event with the actor (`Board-Actor` headers, upserting the actor row) and the
  principal. Errors are problem documents with a `next` field. Tests: a scenario on both databases for each
  operation, the validation errors, and the pagination boundary; unit tests in `internal/domain`.
- **R9 (initiatives CLI and output rules).** This covers `board init add <slug> "<title>" [--tag t]...`,
  `board init ls [--tag t] [--limit N|--all]`, `board init show <slug>` and `board whoami`, configured by
  `BOARD_URL`, `BOARD_API_KEY` (environment only, never a flag), `BOARD_ACTOR` and `BOARD_WAY_BACK`, with the
  §9 fallback actor. They follow SPEC §8.1 exactly:
  - writes print one `ok …` line;
  - errors are one stderr line, ending `→ <next>` where the server or CLI knows one;
  - plain lists cap at 20 with `+K more → …`;
  - `--json` prints full objects;
  - the exit codes that Phase 0 can produce are 0, 1, 2, 4 and 5.

  Tests: one CLI scenario per command, plus one per exit code, plus the cap boundary (20, and 21 with the
  `+1 more` line).
- **R10 (`board help agent` v0).** SPEC §8.6, with Phase 0's content:
  - the text is the identity rules, `board whoami` and the initiative commands, headed `board protocol v0`;
  - it is embedded from `internal/cli/protocol.txt`;
  - `--version` prints `0`;
  - running it records `protocol_seen` through `PUT /api/v1/actors/me/protocol-seen`, and still prints when
    the server is unreachable;
  - `GET /api/v1/meta` returns the server version, the protocol version, the principal and the actor.

  Tests:
  1. a golden file `testdata/help-agent.golden`;
  2. a Godog scenario that parses every `board …` command and `--flag` in the text and resolves each against
     the CLI command tree;
  3. the line budget (at most 45 lines and 100 columns);
  4. `protocol_seen` is recorded;
  5. offline printing.

  The staleness hint is Phase 4 and is not built here.
- **R11 (web UI).**
  - Vite, TypeScript and Svelte 5, built to static assets and embedded in the binary with `go:embed`, served at
    `/`.
  - The pages are: login (redirect to OIDC), initiatives (list and create), and Settings → API keys.
  - The frontend's API base comes from the page's own path, not from a hard-coded origin.

  Tests: Vitest unit tests for the stores and the API wrappers; Playwright (s3-browser's reporter set, **no
  Playwright retries**) for create-then-list, the validation error shown, the keys page (R7), Swagger (R5) and
  login (R6).
- **R12 (container image).**
  - The image is multi-stage, final `FROM scratch` or distroless static, with CA certificates and tzdata,
    running as a non-root uid.
  - It is built for linux/amd64 and linux/arm64 by cross-compiling (`--platform=$BUILDPLATFORM`, `GOARCH`), so
    no emulation is needed.
  - It is built in CI on every build and not pushed in this phase.

  Tests: a CI step that loads the amd64 image, runs `board version` and `/healthz` against it, and inspects
  both platforms' manifests. Trivy scans the image (R13).

### Part D: the pipeline (SPEC §15)

- **R13 (deck-equivalent Jenkinsfile and `ci/`).**
  - **The Jenkinsfile:**
    - stages: CI image → Lint → Suite → Allure smoke → Quality → Stability (extended only) → Mutation (every
      `main` build and extended);
    - `@Library('homelab') _`, agent label `linux && amd64 && docker`, `disableConcurrentBuilds`, a clean
      workspace;
    - an `EXTENDED` parameter, and a nightly trigger on `main`.
  - **Suite:**
    - gotestsum with the fail-once-retry-once flaky list (deck's `junitflaky`), for Go, Godog and Playwright;
    - the Godog scenarios run on SQLite and Postgres, and a scenario green on one database and red on the
      other fails;
    - Vitest;
    - merged Go coverage;
    - JUnit for every layer;
    - Allure with per-branch history.
  - **Quality**, with every SPEC §15.4 gate in `ci/quality.json`, `enabled: true`, at its threshold:
    - Go coverage 85/80/50;
    - CRAP ceiling 8 for Go (deck's `crapgate`, fixture ceiling 8) and for TS (s3-browser-style scoring);
    - **no baseline or exemption file**;
    - frontend coverage 85/80;
    - golangci-lint;
    - govulncheck;
    - Trivy HIGH/CRITICAL on the source and the image;
    - mutation: gremlins at efficacy ≥ 80 % over `internal/domain` and `internal/store` (a run with no mutant
      fails), and Stryker break 80 overall and per module over `web/src/lib`;
    - a CycloneDX SBOM for the module, the frontend and the image (missing or empty fails);
    - OpenAPI lint and drift.
  - **Reports:**
    - deck's CI summary and build summary line;
    - the README badges `build`, `tests`, `coverage` and `quality`, at the public badge path for
      `board/ci/main`, generated as deck does;
    - `notifyTelegram()` last in `post`;
    - a fully green `main` build archives `ci-results/release-candidate.json`.
  - **`Jenkinsfile.release`:** a placeholder that validates `VERSION` and `CI_BUILD` against the candidate and
    stops, with `DRY_RUN` behaviour only.

  Tests:
  - deck's `ci/` script tests carried over and passing;
  - a test in `ci/qualitycheck` that fails when any SPEC §15.4 gate is missing from `ci/quality.json`, is
    disabled, or is looser than its threshold;
  - the pipeline itself, green at the final sha (R17).
- **R14 (lint).** deck's `ci/lint.sh` plus:
  - ESLint, `svelte-check`, `tsc --noEmit`;
  - OpenAPI lint and generated-code drift;
  - shellcheck on every tracked shell script.

  (The `install.sh` ↔ `INSTALL-AGENT.md` check is Phase 4.) Test: one lint fixture per added checker, proving
  that it fails on a planted violation.
- **R15 (docs/ci.md).** It describes board's pipeline as it is: stages, gates, lanes and the local recipes. It
  is the only CI document; there is no report or record file.

### Part E: spec and sweep

- **R16 (spec in step).** In the same commits as the behaviour, SPEC states the behaviour Phase 0 settles (exact
  flags, exit codes, endpoint shapes, the mock provider's role) in present tense. No paragraph contradicts the
  code. SPEC §16 stays the phase plan and is not marked "done".
- **R17 (the sweep).** The last three tasks follow the order given in Standing rules, all at one `origin/main`
  head. Each is `sweep: true` and commits nothing.

**Pre-split into tasks at plan time, in this order:**

1. R1 + R2 + R3 (SQLite only), with a trivial slug validator in `internal/domain` so that every gate has
   something to measure.
2. R13 + R14 + R15 with every gate on against that code, and the Svelte shell + Vitest + Stryker + TS CRAP +
   one Playwright smoke (R11's skeleton). The first green `board/ci/main` build is this task's criterion.
3. R4 (Postgres leg and matrix in `ci/run.sh` and Suite).
4. R5.
5. R6 server side and the mock provider.
6. R6 UI login + Playwright.
7. R7 API and admin CLI.
8. R7 UI page.
9. R8.
10. R9 commands.
11. R9 output rules and exit codes.
12. R10.
13. R11 pages.
14. R12.
15. The three sweep tasks.

R16 is done inside each task that changes behaviour.

## Materiality rubric

Reject only on substance:

- a requirement's behaviour is absent or wrong in the live code;
- a test the requirement names is missing, red, or asserts the opposite of the requirement;
- the spec contradicts the live code;
- a secret is exposed.

Everything else verifies, with the gap recorded as a residual note: wording, form, provenance, process, and
**any number or claim in prose outside the spec** (commit messages, notes, issue comments, run artifacts). Prose
outside the spec is never a blocking ground.

Working clauses:

- file the rule, not the example;
- read the direction of the evidence;
- a review after a cure pass confirms that the cure landed and does not re-audit the product;
- judge a hat or router by its contract, not its taste;
- a stale sweep is never process.

## Standing rules

- **Test recipes, verbatim:**
  - one Go package: `ci/run.sh go test -count=1 ./internal/<pkg>/`;
  - one scenario file, one database: `BOARD_GODOG_PATHS=<file>.feature BOARD_DB_LEG=sqlite|postgres ci/run.sh go
    test -count=1 ./features/ -run TestFeatures`;
  - frontend unit: `ci/run.sh sh -c 'cd web && npm run test'`;
  - one Playwright spec: `ci/run.sh sh -c 'cd e2e && npx playwright test <spec>'`;
  - lint: `ci/run.sh ci/lint.sh`;
  - the gates: `ci/run.sh ci/quality.sh`;
  - the whole suite: `ci/run.sh ci/suite.sh`, never run in parallel with another heavy command.
  - Until task 1 lands `ci/run.sh`, build the CI image once by hand (`docker build -t board-ci:local -f
    ci/Dockerfile ci`). After that, always use the recipes.
  - Always run from a shell with no `BOARD_*` variable set, except those a recipe sets.
- **Only the final sweep runs a whole tier.** Every other task runs the narrowest recipe that covers what it
  touched, plus `ci/run.sh ci/lint.sh` before every push (once lint exists).
- **Commits are made per task and pushed to `main`.** The title is `<area>: <imperative summary> (task NNN,
  R##)`, and the author is the git identity in `/config/creds/gitconfig`. A commit carries no tool or model
  attribution and no session links.
- **CI is Jenkins, not GitHub Actions.** There are no workflows.
  - **The job.** A push to `main` builds the multibranch job `board/ci`, branch `main`, at
    `https://jenkins.wisent-owl.ts.net/job/board/job/ci/`, through a webhook, with a periodic scan as a
    backstop. The operator is setting the job up in parallel with this run.
  - **Credentials.** The API credentials are in `/config/creds/jenkins.env` (`JENKINS_URL`, `JENKINS_USER`,
    `JENKINS_TOKEN`; load them with `set -a; . /config/creds/jenkins.env; set +a`). Use them only as curl basic
    auth (`-u "$JENKINS_USER:$JENKINS_TOKEN"`), and never print the file, the token or an auth header.
  - **Queries.** Pass `curl -g`. In the endpoints below, `…` means `$JENKINS_URL/job/board/job/ci`. A build
    lists two `lastBuiltRevision` SHA1s: the homelab shared library's and the repo's. A build is of a sha when
    either of them equals it.
- **After every push from task 2 onward**, at the start of the next iteration and before picking a task, find
  the `board/ci/main` build of that sha and poll it to its result:
  - `GET …/job/main/api/json?tree=builds[number,result,building,actions[lastBuiltRevision[SHA1]]]{0,10}` finds
    the build;
  - `GET …/job/main/<n>/api/json` polls it;
  - `GET …/job/main/<n>/consoleText` reads its log.

  A build that is not `SUCCESS` is the next task, fixed at its root.

  If no build for the sha appears within 10 minutes:
  - trigger one with `POST …/job/main/build`;
  - if `…/job/main/` does not exist yet, trigger a branch scan with `POST …/build?delay=0`;
  - if `…` itself is missing or the API answers 403, the job is not ready yet: defer the task (`deferred`,
    30 minutes) and carry on with tasks that do not need CI. Do this for up to 12 hours. After that,
    petition with the evidence.

  Before task 2 is validated, builds are not judged.
- **Waits.** A wait longer than about 85 % of the iteration cap is spanned with `deferred`, never by sleeping
  past the cap.
- **The sweep's shape.** The last three tasks run in this order, all at the same `origin/main` head, and each is
  marked `sweep: true`:
  1. Under `ci/run.sh`:
     - the whole `go test -p=1 -count=1 ./...` tier;
     - `go build ./...`, `go vet ./...` and `gofmt -l` on the tracked `.go` files;
     - in `web/`, `npm run test` and `npm run build`.
  2. `ci/run.sh ci/suite.sh ci-results && ci/run.sh ci/quality.sh ci-results`.
  3. Confirm that the push build at that head is `SUCCESS`, that it ran the Mutation stage, and that its
     archived `ci-results/flaky-go.txt`, `ci-results/flaky-features.txt` and `ci-results/flaky-playwright.txt`
     are empty or absent (`GET …/job/main/<n>/artifact/ci-results/<file>`). Then:
     - trigger **one** extended build with `POST …/job/main/buildWithParameters?EXTENDED=true`;
     - poll it to its result;
     - require `SUCCESS`, with the Stability and Mutation stages run and empty flaky files.

     A build that never reached the Suite stage (an agent provisioning failure, or an API 5xx) is not judged;
     trigger another.

  Sweep tasks commit nothing. **Nothing is committed after the sweep.** If `origin/main` moved past the swept
  sha, the sweep is stale and re-runs. A stale sweep is never process.
- **Escape hatch.** A requirement the run proves contradictory or impossible is petitioned with the evidence,
  not silently dropped. Review adjudicates the petition. A threshold that real code cannot meet is a petition,
  never an edit to `ci/quality.json`.
- **Engine defects to avoid by hand:**
  - every requirement here names its finite cases, and so must the tasks;
  - do not signal COMPLETE while tasks are pending;
  - when an approach ends in review, read the findings before replanning;
  - after any cure pass, check `tasks.json` for a task that depends on itself, and repair it by steer.

## Non-negotiables

- **No weakened test, and no gate loosened or skipped.** No `enabled: false`, no warn-only mode, no exemption
  or baseline file, no `// nolint` without a stated reason on the line, and no `skip` in a feature, spec or
  test.
- **No raised timeout, budget, worker count or per-test bound** beyond what a stage first lands with.
- **Playwright never retries.** Flakiness is handled only by the suite's retry-once policy and is recorded.
- **No secret** in a commit, a log line, an event, a golden file or a test fixture. Test keys and the mock
  provider's keys are generated at test time.
- **No homelab specifics in the repository** (hostnames, tailnet names, personal paths), except for the
  Jenkins job URL in `docs/ci.md` and the badge URLs in the README. The repository is meant to go public.
- **No new external service in tests.** CI never reaches an external identity provider, and tests make no
  network calls beyond the containers the suite starts.

## Budget

- 400 iterations, 2 approaches, a 48 h wall clock and a 120-minute iteration cap.
- Peer plan review and the peer verdict gate are on.
- Vigilant, reflect and router task selection are on.
- Docker access is on (sibling toolchain, testcontainers, image builds).
- Host networking is on (Jenkins is tailnet-only).
