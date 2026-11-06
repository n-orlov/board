# Phase 1: tasks, leases, conflicts and initiative editing

## What the operator gets

1. **Initiatives are editable**: edit title, description, tags and target dates; close (with `force`) and
   reopen; an initiative page that lists its tasks.
2. **Tasks**: create, list with filters, a task page, edit, done, reopen, close with a resolution,
   dependencies, watch tasks with checks, milestones and a per-task event timeline. Full controls in the UI
   on phone and desktop, in the API and in the CLI.
3. **Leases and conflicts** (SPEC §4.4, §5.1, §5.2): agents start, renew and end leases on tasks with
   resources; expiry sweeps them; conflicts come back on start and renew; `board now` shows the lease and
   conflict picture; the UI shows live leases and conflicts.
4. Short refs for every Phase 1 object (§8.2), the global event log, and agent protocol **v1**.

This is SPEC §16 Phase 1, plus initiative PATCH / close / reopen and the UI controls for everything above.
`SPEC.md` is the product spec; where this PRD and `SPEC.md` disagree, **this PRD wins** and the spec is
corrected in the same commit (R13).

## Context the run needs

- Phase 0 (skeleton, auth, every CI gate) and Phase 0b (Tailwind v4 design system, responsive shell, dialogs,
  axe, touch rules) are done and green at the commit that adds this PRD. Read `SPEC.md` (all of §3–§8, §11,
  §15, §16), `api/openapi.yaml`, `internal/`, `web/`, `features/`, `e2e/` and `docs/ci.md` before planning.
- The existing initiative slice is the pattern to follow end to end: OpenAPI first, generated strict server
  and TS types, `internal/<area>/` service, store queries on SQLite and Postgres, the events row in the same
  transaction, CLI on `internal/cli/client.go`, Godog features on both databases, Vitest for `web/src/lib/`,
  Playwright for the views. Components stay thin; logic lives in `lib/` modules with unit tests. The CRAP 8
  ceiling (Go and TS), coverage, Stryker and gremlins apply to all new code.
- The UI rules from Phase 0b apply to every new view and dialog: one `Dialog` component (bottom sheet on the
  phone), no horizontal scroll, 44 px hit areas and 16 px inputs on the phone, axe clean in both themes, same
  origin only, the route table drives router and navs, tables from 768 px and cards below.

Symbols and section numbers refer to `SPEC.md` and the code at the commit that adds this PRD. Drift is expected
and is not a finding: if a reference has drifted, find the current site and carry on.

## Issues in scope

None filed. This PRD is the whole statement.

**Not in scope:** asks, notes, cursors, the attention view and `GET /attention` (Phase 2); the Timeline view,
SSE and `/stream` (Phase 3); the shortcuts `start`/`finish`/`ping`/`catchup`/`new`/`block`, `Idempotency-Key`
replays, `?view=compact`, `board update`, releases (Phase 4); the task queue, ACLs, MCP (§17); the homelab
deployment. `GET /now` in this phase returns only its lease and conflict parts (R7); the ask, note and
attention parts arrive with Phase 2.

## Requirements

Each requirement is a behaviour plus the tests that fail without it. "Both databases" means the Godog
feature runs on the SQLite and the Postgres leg. "Phone" and "desktop" are the Playwright viewports 390×844
and 1440×900. Every write below appends its event (§4.6) in the same transaction as the change, with
`principal`, actor (upserted as today), `initiative_id`, `task_id` and `ref_id` set as they apply; a refused
request changes nothing (no row, event or actor).

### R1 Initiative edit, close, reopen and list filters

- `PATCH /api/v1/initiatives/{slug}` with any of `title`, `description`, `tags` (replaces the set),
  `target_start`, `target_end` (`null` clears a date). Same validation as create; the slug is immutable (a
  `slug` field is a 400). An empty body or one that changes nothing answers 200 and writes no event; a change
  writes `initiative_updated` with `data.changed` listing the changed field names. 404 for an unknown ref.
- `POST /api/v1/initiatives/{slug}/close` (`{force?: bool}`): with an open or done task and no `force`, 409
  whose `next` names the force option; with `force`, every not-closed task is closed `dropped` (each with its
  `task_closed` event) and then the initiative (`initiative_closed`). Closing a closed initiative is 409.
  `POST …/reopen` reopens a closed one (`initiative_reopened`; its tasks stay closed); reopening an open one is
  409.
- `GET /api/v1/initiatives` gains `status` (`open` | `closed` | `all`, default `open`) and `closed_within`
  (`7d`, `30d`, `90d`, an integer of days; only with `status=closed|all`), plus per item `counts`
  `{open, done, closed}` of its tasks and `live_leases`.
- Creating or patching a task in a closed initiative is 409.
- **Tests:** Godog on both databases for each case above (patch each field; clear a date; slug in body;
  no-op patch writes no event; close with open task without force → 409; close with force drops tasks and
  writes the events; close closed → 409; reopen; reopen open → 409; each status filter value and
  `closed_within`; counts). CLI: `board init edit <slug> [--title …] [--description file|-] [--tag t]...
  [--no-tags] [--start D] [--end D] [--clear-start] [--clear-end]`, `board init close <slug> [--force]`,
  `board init reopen <slug>`, `board init ls --status open|closed|all [--closed-within N]`, each with `ok`
  lines, `→ next` errors and exit codes per §8.1, covered by Godog CLI scenarios.

### R2 Tasks: create, read, list, edit

- Migration creates `tasks` (§4.3) and `task_deps`, with the §4.10 indexes, on both databases. A task's ref is
  `<initiative-slug>-<seq>`, `seq` per initiative from 1, never reused; refs are accepted case-insensitively
  wherever a task is named, as is the ULID.
- `POST /api/v1/tasks` `{initiative, title, description?, kind? (work|watch, default work), planned_start?,
  planned_end?, cadence_seconds?, next_check_at?, external_refs?, blocked_by?: [ref]}`. Watch-only fields on a
  work task are 400; a watch task's `cadence_seconds` is 60 to 31536000; `next_check_at` defaults to
  now + cadence; `external_refs` items are `{label (1–80), url (http/https)}`, at most 20; title 1–200,
  description ≤ 20000; `planned_end` not before `planned_start`. 201 with the task; `task_created`.
- `GET /api/v1/tasks/{ref}`: the task with `ref`, `initiative` (slug), `deps` (blocked-by tasks with ref,
  title, status), `blocking` (tasks it blocks), `live_leases`, and the derived `active`, `blocked`,
  `overdue` (§4.3; `blocked` here means an open dependency, asks come in Phase 2).
- `GET /api/v1/tasks`: filters `initiative` (repeatable), `tag` (an initiative tag, repeatable, all must
  match), `status` (`open` | `done` | `closed` | `not_closed` default | `all`), `kind`, `active`,
  `blocked`, `overdue` (each `true`/`false`), `closed_within`, `q` (case-insensitive substring of title or
  description). `limit`/`after` paging as for initiatives. Ordered by id.
- `PATCH /api/v1/tasks/{ref}`: `title`, `description`, `planned_start`, `planned_end`, `cadence_seconds`,
  `next_check_at`, `external_refs` (replaces). Kind and initiative are immutable (400). No-op writes no
  event; a change writes `task_updated` with `data.changed`. Patching a closed task is 409.
- **Tests:** Godog on both databases: create work and watch tasks, each validation refusal, ref assignment
  across two initiatives (`a-1`, `a-2`, `b-1`), case-insensitive ref and ULID lookup, each list filter value
  and `q`, paging, patch each field, immutable fields, no-op patch, closed task patch → 409, create in closed
  initiative → 409. Go unit tests for the ref parser and validation.

### R3 Task transitions

- `POST /tasks/{ref}/done` (open → done; `task_done`, sets `done_at`, `done_by`), `/reopen` (done or closed →
  open; `task_reopened`, clears the done/closed fields), `/close` `{resolution: completed|dropped|superseded|
  duplicate}` (open or done → closed; `task_closed`). Every other transition is 409 naming the current
  status: done on done, done on closed, reopen on open, close on closed. A missing or unknown resolution is
  400. Reopening a task in a closed initiative is 409.
- Closing or marking done ends nothing implicitly: live leases on the task stay live (the holder ends them).
- **Tests:** Godog on both databases: the 4 allowed transitions, the 4 refused ones, each of the 4
  resolutions, missing resolution, reopen in closed initiative.

### R4 Dependencies

- `POST /tasks/{ref}/deps/{blockedByRef}` adds; `DELETE` removes. A self-dependency is 400; one that would
  close a cycle (any length) is 409 naming the path; adding an existing one is 200 with no event; removing a
  missing one is 404. Events `dep_added` / `dep_removed` on the dependent task with `data.blocked_by`. A
  dependency is open while the blocking task is not `closed` (§4.3).
- **Tests:** Godog on both databases: add, duplicate add, remove, remove missing, self → 400, 2-cycle and
  3-cycle → 409, `blocked` derived true with a done-not-closed blocker and false once it closes,
  dependencies across initiatives.

### R5 Watch checks, milestones and events

- `POST /tasks/{ref}/checks` `{summary (1–200), next_check_at?}` on a watch task: `check_recorded`; sets
  `next_check_at` to the given time or now + cadence. On a work task 409. `overdue` is a watch task, not
  closed, with `next_check_at < now`.
- `POST /tasks/{ref}/milestones` `{summary (1–200), data?: object}`: `milestone`.
- Events get the `n` short ref `E<n>` and a one-line `summary` (§4.6). `GET /tasks/{ref}/events` lists a
  task's events in order; `GET /api/v1/events` lists all, filters `since` (an event ULID, `E<n>`, or RFC 3339
  time), `initiative`, `task`, `type` (repeatable), `actor`, with `limit`/`after` paging. Each event is
  `{id, ref, at, type, actor, principal, initiative?, task?, ref_id?, summary, data}`.
- **Tests:** Godog on both databases: check on watch (with and without `next_check_at`), check on work →
  409, overdue true/false, milestone with data, task events in order after a create → edit → done → reopen →
  close sequence, each `/events` filter and each `since` form.

### R6 Leases, expiry and conflicts

- Migration creates `leases` and `lease_resources` (§4.4); lease ref `L<n>`.
- `POST /api/v1/leases` `{task, intent (1–200), ttl_seconds?, resources?: [string]}`: ttl clamped to
  [60, 43200], default 7200; resources 1–100 chars each, at most 20, deduplicated. A task that is closed is
  409. If the calling actor already holds a live lease on that task, it is renewed instead (same lease, 200,
  `lease_renewed`), otherwise 201 and `lease_started`. The response is `{lease, conflicts}`.
- `POST /leases/{ref}/renew` `{ttl_seconds?, intent?, resources?}`: pushes `expires_at`; an expired lease is
  410 carrying `expired_at`; an ended lease is 409. Response `{lease, conflicts}`; `lease_renewed`.
- `POST /leases/{ref}/end` `{outcome?}`: `ended`; `ended_by` set when the caller is not the holder;
  `lease_ended`. Ending a non-live lease is 409.
- `POST /leases/{ref}/ack`: records the operator's acknowledgement of an expired lease (`acked_at`,
  `acked_by`; a column added by this migration); on a non-expired lease 409. A later lease on the same task
  also counts as acknowledged (derived, for Phase 2's attention view).
- **Expiry:** a sweeper in `board serve` every 15 s marks `live` leases with `expires_at < now` as `expired`
  and writes `lease_expired`, with the conditional update of §5.1 (idempotent, safe with two servers on one
  database). Every read that returns leases first runs the same expiry, so no read shows a lease live past
  its expiry. The interval is configurable for tests (`BOARD_SWEEP_INTERVAL`, documented in §13).
- **Conflicts** (§5.2): the live leases of other leases holding any of the same resources, each
  `{lease, holder, task, intent, expires_at, resources}` (the shared resources). A lease never conflicts
  with itself; two leases of the same actor on different tasks sharing a resource do conflict.
- `GET /api/v1/leases`: filters `state` (`live` default | `ended` | `expired` | `all`), `actor`, `task`,
  `resource`, paging. `GET /leases/{ref}`.
- **Tests:** Go unit tests for ttl clamping and the conflict computation; Godog on both databases: start,
  idempotent start renews, start on closed task, ttl clamp at both ends and default, renew, renew expired →
  410, renew ended → 409, end by holder, end by another actor sets `ended_by`, end twice → 409, ack expired,
  ack live → 409, conflicts on start and on renew (none, one, two holders, same actor two tasks), each list
  filter, expiry by sweeper (short interval) and lazily on read (sweeper disabled), and the two-server
  conditional update writing exactly one `lease_expired` (two stores on one Postgres).

### R7 `board now` and `GET /now` (lease and conflict parts)

- `GET /api/v1/now` `?resources=r1,r2`: `{actor, my_leases (live, with task ref and title, expires_at),
  others_leases (live leases of other actors), conflicts (on my live leases' resources, and on the given
  `resources`)}`. No ask, note or attention fields in this phase.
- `board now [--resources r1,r2] [--json]`: sections `my leases`, `others`, `conflicts`, one line each,
  `CONFLICT` prefixed lines for conflicts; exit **7** when a conflict touches the given resources or my live
  leases, else 0.
- **Tests:** Godog API and CLI scenarios: no leases, mine only, others only, a conflict on my lease, a
  conflict on a queried resource, exit 7 vs 0.

### R8 CLI for tasks and leases

- Commands (flags in either position, `--json` on each, §8.1 output and exit codes, short refs everywhere):
  - `board task add <initiative> "<title>" [--description file|-] [--kind watch --cadence 24h]
    [--after <ref>]... [--start T] [--end T] [--link label=url]...`
  - `board task ls [<initiative>] [--status open|done|closed|not_closed|all] [--kind k] [--active]
    [--blocked] [--overdue] [--q text] [--limit N|--all]`
  - `board task show <ref>` (fields, deps, blocking, live leases, last 10 events)
  - `board task edit <ref> [--title …] [--description file|-] [--start T] [--end T] [--cadence D]
    [--link label=url]... [--no-links]`
  - `board task done|reopen <ref>`, `board task close <ref> [--resolution r] [--for operator]` (default
    `completed`; `--for operator` sets `Board-On-Behalf-Of` to the operator and the event's `on_behalf_of`)
  - `board task dep add|rm <ref> <blocked-by-ref>`, `board task events <ref>`
  - `board lease start <task> "<intent>" [--ttl 2h] [--res r]...`, `board lease renew [<L…>|<task>] [--ttl …]
    [--intent …]` (no argument: all my live leases), `board lease end [<L…>|<task>] ["<outcome>"]`,
    `board lease ls [--state …] [--mine] [--task ref] [--res r]`
  - `board milestone <task> "<summary>"`, `board check <task> "<summary>" [--next 24h]`
  - `board events [--since …] [--task ref] [--initiative slug] [--type t]... [--actor h]`
- `lease start` and `lease renew` print each conflict as a `CONFLICT` line and exit 7 when any exists.
- Durations accept `s`, `m`, `h`, `d`; times accept RFC 3339 or `YYYY-MM-DD`.
- **Agent protocol v1**: `internal/cli/protocol.txt` gains the task, lease, `now`, milestone and check
  commands, conflict handling (exit 7 / `CONFLICT`) and "close only when asked, with `--for operator`";
  heading `board protocol v1`; golden file updated; the §8.6 command/flag existence feature and the 45-line /
  100-column budget pass.
- **Tests:** Godog CLI scenarios for each command's success line, a refusal with its `→ next` and exit code,
  `--json`, and the exit-7 paths; unit tests for duration/time parsing; the existing help-agent feature.

### R9 UI: initiative page and editing

- Route `#/i/<slug>`: the initiative's title, slug, status badge, tags, targets, description (Markdown
  rendered safely: no raw HTML, links get `rel="noopener noreferrer"`), counts, and its tasks table (ref,
  title, kind, status, active/blocked/overdue badges, live lease holder) with a status filter
  (not closed / done / closed / all). Rows in the Initiatives list link to it, and the list shows counts and
  live leases and gains a status filter (open / closed / all).
- Actions: **Edit** (form in the dialog: title, description, tags, targets; refusals inline, input kept),
  **Close** (confirmation; when tasks are not closed, the dialog says how many and offers "Close and drop
  N tasks", which sends `force`), **Reopen**, **New task** (R10's form with the initiative preset).
- **Tests:** Vitest for the Markdown renderer (raw HTML escaped, `javascript:` links dropped, links get
  `rel`), the filter state and the counts presentation. Playwright at phone and desktop: open an initiative
  from the list, edit each field, a refused edit keeps input, close with no tasks, close with an open task
  (force path) shows the tasks dropped, reopen, filter tasks.

### R10 UI: tasks

- A **Tasks** view `#/tasks` in the nav (route table entry): all tasks across initiatives with filters
  (status, kind, initiative, active, blocked, overdue, text search) held in the URL fragment's query, so a
  reload keeps them.
- **New task** form (dialog; bottom sheet on phone; primary button anchored above the tab bar on phone):
  initiative (select; preset from the initiative page), title, description, kind, cadence (watch only, shown
  only for watch), planned start/end, blocked-by (ref picker over open tasks), links.
- Task page `#/t/<ref>`: header (ref in monospace, title, status, kind, badges), description, planned
  window, links, deps and blocking (each linked, add / remove a dependency), live leases (holder, intent,
  resources, expires in), and the event timeline (newest first, relative times, summary, actor).
- Actions: **Edit**, **Done**, **Reopen**, **Close** (resolution select in a confirmation), **Milestone**,
  **Record check** (watch only), and on a live lease **End** and, on an expired one, **Acknowledge**. Every
  action shows refusals inline and a toast on success, and reloads the page's data. On the phone the main
  actions sit in a bottom action bar or menu, never hover-only.
- **Tests:** Vitest for the filter ↔ URL encoding, the action-availability rules per status and kind (the
  finite table: open/done/closed × work/watch), the cadence/duration formatter and the "expires in"
  formatter. Playwright at phone and desktop: create a work task and a watch task, filter the Tasks view and
  reload, edit, done, reopen, close with each path's resolution, add and remove a dependency and see
  `blocked` change, record a check, add a milestone, end a lease started through the API, acknowledge an
  expired lease (sweeper interval short in the e2e server), and see each action in the event timeline.

### R11 UI: leases and conflicts

- The Initiatives list, initiative page and task page show live lease counts or holders; the task page
  shows conflicts of its live leases (resource, other holder, other task linked). A small **Now** panel on
  the Tasks view (collapsible on phone) lists live leases across the board and current conflicts from
  `GET /api/v1/leases` and the conflict data.
- **Tests:** Playwright: two leases through the API sharing a resource show as a conflict on both task pages
  and in the Now panel; ending one clears it.

### R12 UI quality bars extend to the new views

- Every new view (`#/tasks`, `#/i/<slug>`, `#/t/<ref>`) and every new dialog (edit initiative, close
  initiative, new task, edit task, close task, milestone, record check, add dependency, end lease) meets the
  Phase 0b bars: no horizontal scroll with the longest accepted values, 44 px / 16 px on the phone, axe zero
  serious/critical at phone and desktop in both themes, same-origin requests only, focus trapped and
  returned, `aria-current` on the nav.
- **Tests:** the existing `overflow`, `touch`, `a11y`, `sameorigin`, `dialogs` and `shell` specs are
  extended with these views and dialogs as finite listed states (each named in the spec file).

### R13 The spec matches the code

- `SPEC.md` describes in present tense: §4 (tables now created, `acked_at`/`acked_by`), §6 (a §6.4 Tasks and
  §6.5 Leases section in the §6.3 style, PATCH/close/reopen in §6.3, `/events`, `/now` parts), §5.1–§5.2
  (sweeper interval setting), §8.3 (shipped commands and their output lines and exit codes), §8.6 (protocol
  v1), §11 (the new views and dialogs), §13 (`BOARD_SWEEP_INTERVAL`) and §16 (Phase 1 shipped, one line).
  `api/openapi.yaml` declares every new operation. Updated in the same commit as each behaviour, never in a
  docs-only task.

## Materiality rubric

Reject only on substance:

- a requirement's behaviour is absent or wrong in the live code;
- a test the requirement names is missing, red, or asserts the opposite of the requirement;
- the spec contradicts the live code;
- a secret is exposed.

Everything else verifies, with the gap recorded as a residual note: wording, form, provenance, process,
visual taste, and **any number or claim in prose outside the spec** (commit messages, notes, issue comments,
run artifacts). Prose outside the spec is never a blocking ground.

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
  - A Playwright spec needs the built UI embedded in the binary; if a recipe in `docs/ci.md` builds it first,
    use that. Adding a dependency changes `ci/Dockerfile` only if the toolchain itself needs it.
  - Always run from a shell with no `BOARD_*` variable set, except those a recipe sets.
- **Only the final sweep runs a whole tier.** Every other task runs the narrowest recipe that covers what it
  touched, plus `ci/run.sh ci/lint.sh` before every push.
- **Plan shape.** Pre-split at plan time: R1 into API, CLI; R2–R6 each into migration/store, API, and
  their CLI where named in R8; R8's protocol v1 as its own task after the commands exist; R9, R10, R11 each
  into lib modules plus views; R12 as one task per spec file. Backend before the UI that needs it.
- **Screenshots for the operator.** Whenever a task changes how a view looks, take Playwright screenshots of
  that view at phone and desktop in both themes into `/run/ralphd/artifacts/screenshots/` (never committed)
  and look at them before calling the task done. Send a few to the operator with the telegram-notify skill at
  each milestone (backend API done, initiative page done, tasks UI done, sweep green).
- **Commits are made per task and pushed to `main`.** The title is `<area>: <imperative summary> (task NNN,
  R##)`, and the author is the git identity in `/config/creds/gitconfig`. A commit carries no tool or model
  attribution and no session links.
- **CI is Jenkins, not GitHub Actions.** There are no workflows.
  - **The job.** A push to `main` builds the multibranch job `board/ci`, branch `main`, at
    `https://jenkins.wisent-owl.ts.net/job/board/job/ci/`, through a webhook, with a periodic scan as a
    backstop.
  - **Credentials.** `/config/creds/jenkins.env` (`JENKINS_URL`, `JENKINS_USER`, `JENKINS_TOKEN`; load it
    with `set -a; . /config/creds/jenkins.env; set +a`). Use them only as curl basic auth
    (`-u "$JENKINS_USER:$JENKINS_TOKEN"`), and never print the file, the token or an auth header.
  - **Queries.** Pass `curl -g`. In the endpoints below, `…` means `$JENKINS_URL/job/board/job/ci`. A build
    lists two `lastBuiltRevision` SHA1s: the homelab shared library's and the repo's. A build is of a sha when
    either of them equals it.
- **After every push**, at the start of the next iteration and before picking a task, find the
  `board/ci/main` build of that sha and poll it to its result:
  - `GET …/job/main/api/json?tree=builds[number,result,building,actions[lastBuiltRevision[SHA1]]]{0,10}` finds
    the build;
  - `GET …/job/main/<n>/api/json` polls it;
  - `GET …/job/main/<n>/consoleText` reads its log.

  A build that is not `SUCCESS` is the next task, fixed at its root. If no build for the sha appears within
  10 minutes, trigger one with `POST …/job/main/build`. A build that never reached the Suite stage (an agent
  provisioning failure, or an API 5xx) is not judged; trigger another.
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
  or baseline file, no `// nolint` or `eslint-disable` without a stated reason on the line, no axe rule
  disabled, and no `skip` in a feature, spec or test. A test pinning behaviour this wave changes is rewritten
  in the same commit and keeps every behaviour it asserted.
- **No raised timeout, budget, worker count or per-test bound.**
- **Playwright never retries**, and no visual-snapshot (pixel-diff) assertions. Time-dependent tests use an
  injected clock or the configurable sweep interval, never sleeps longer than a few seconds.
- **No secret** in a commit, a log line, an event, a golden file, a screenshot or a test fixture.
- **No homelab specifics in the repository** (hostnames, tailnet names, personal paths), except for the
  Jenkins job URL in `docs/ci.md` and the badge URLs in the README. The repository is meant to go public.
- **No external network request** from the UI or from tests beyond the containers the suite starts.
- **Migrations are additive and forward-only**: existing initiatives, keys, sessions and events survive the
  upgrade untouched (a Godog or Go test migrates a Phase 0 database with rows and reads them back).

## Budget

- 500 iterations, 3 approaches, a 48 h wall clock and a 120-minute iteration cap.
- Peer plan review and the peer verdict gate are on.
- Vigilant, reflect and router task selection are on.
- Docker access is on (sibling toolchain, testcontainers). Host networking is on (Jenkins is tailnet-only).
