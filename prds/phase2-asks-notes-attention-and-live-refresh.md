# Phase 2: asks, notes, the attention view and live refresh

## What the operator gets

1. **Asks** (SPEC §4.5, §7.2): an agent raises a question on a task, an initiative or the whole board, either
   stopping (`stop`) or waiting for the answer (`poll`, a long-poll the CLI holds up to a deadline). A blocking ask
   marks its task blocked. The operator answers in the UI; an agent records an answer given in conversation as a
   relay.
2. **Notes** (§4.7, §4.8): agents and the operator post short notes bound to a task, an initiative, a resource or
   the whole board (a broadcast), with mentions; readers ask for "what's new since my last read" through a
   server-side cursor.
3. **The attention view** (§5.3): one page, the UI's new default, with the five things that need the operator
   (open asks, done-not-closed tasks, expired leases not yet acknowledged, overdue watches, live conflicts), each
   with its inline action. `board attention` prints the same; `board now` gains its ask, note and attention parts.
4. **Live refresh**: every view updates by itself within a few seconds when something changes on the board,
   whoever changed it, with no reload. One Server-Sent Events stream (§7.1) feeds it; the UI shows whether it is
   live.
5. Agent protocol **v2** with the ask, answer and note commands.

This is SPEC §16 Phase 2, plus the SSE stream (§7.1, moved here from Phase 3) and the Notes view (§11). `SPEC.md`
is the product spec; where this PRD and `SPEC.md` disagree, **this PRD wins** and the spec is corrected in the
same commit (R14).

### Why SSE for live refresh

Polling every view on a timer is simple but repeats every query for every open tab whether or not anything changed,
and still lags by its interval. WebSockets are two-way, which nothing here needs, and add a protocol and a library.
Server-Sent Events are one-way, plain HTTP through the same auth and the same proxies, and the browser's
`EventSource` reconnects by itself and sends `Last-Event-ID`; since every change already writes a row in `events`
with a monotonically increasing id, the stream is a tail of that table and a reconnect replays exactly what was
missed. The server tails the table (a short query on an indexed id, about once a second, plus an immediate
in-process wake-up after a local write), so several replicas on one Postgres stay correct without
`LISTEN/NOTIFY`. Reaction time is "a few seconds", not real time.

## Context the run needs

- Phase 0, 0b and 1 are done, deployed and green at the commit that adds this PRD. Read `SPEC.md` (all of §3–§8,
  §11, §13–§16), `prds/phase1-tasks-leases-and-initiative-editing.md`, `api/openapi.yaml`, `internal/`, `web/`,
  `features/`, `e2e/` and `docs/ci.md` before planning.
- The task and lease slices are the pattern to follow end to end: OpenAPI first, generated strict server and TS
  types, `internal/<area>/` service, store queries on SQLite and Postgres, the events row in the same
  transaction, CLI on `internal/cli/client.go`, Godog features on both databases, Vitest for `web/src/lib/`,
  Playwright for the views. Components stay thin; logic lives in `lib/` modules with unit tests. The CRAP 8
  ceiling (Go and TS), coverage, Stryker and gremlins apply to all new code.
- The UI rules from Phase 0b apply to every new view and dialog: one `Dialog` component (bottom sheet on the
  phone), no horizontal scroll, 44 px hit areas and 16 px inputs on the phone, axe clean in both themes, same
  origin only, the route table drives router and navs, tables from 768 px and cards below.

Symbols and section numbers refer to `SPEC.md` and the code at the commit that adds this PRD. Drift is expected and
is not a finding: if a reference has drifted, find the current site and carry on.

## Issues in scope

None filed. This PRD is the whole statement.

**Not in scope:** the Timeline view, `GET /timeline`, the Actors and Archive views and `GET /actors` (Phase 3); the
shortcuts `start`/`finish`/`ping`/`catchup`/`new`/`block` and `lease_action` on asks, `Idempotency-Key` replays,
`?view=compact`, `board update`, releases (Phase 4); the task queue, ACLs, MCP (§17); the homelab deployment.
Anyone authenticated may answer or withdraw any ask (no ACL in v1; the event records who).

## Requirements

Each requirement is a behaviour plus the tests that fail without it. "Both databases" means the Godog feature runs
on the SQLite and the Postgres leg. "Phone" and "desktop" are the Playwright viewports 390×844 and 1440×900. Every
write below appends its event (§4.6) in the same transaction as the change, with `principal`, actor (upserted as
today), `initiative_id`, `task_id` and `ref_id` set as they apply; a refused request changes nothing (no row, event
or actor).

### R1 Asks: raise, read, list, answer, withdraw

- Migration creates `asks` (§4.5) with short ref `A<n>`, plus `poll_seen_at` (nullable), and the indexes for
  `state`, `task_id` and `actor_id` lookups, on both databases.
- `POST /api/v1/asks` `{task?, initiative?, question (1–200, one line), details? (≤ 20000), options? (≤ 10 strings
  of 1–80), mode (stop|poll, default stop), blocking? (default false)}`. `task` and `initiative` together is 400
  unless the task belongs to that initiative; `initiative` is derived from `task`. An ask on a closed task or
  closed initiative is 409. 201; `ask_raised`.
- `GET /api/v1/asks/{ref}` (ref `A<n>` or ULID): the ask with `ref`, `task` (ref), `initiative` (slug), asker,
  `answered_by`, `on_behalf_of`, `channel`, times and the derived `asker_waiting` (below).
- `GET /api/v1/asks`: filters `state` (`open` default | `answered` | `withdrawn` | `all`), `actor`, `task`,
  `initiative`, with `limit`/`after` paging, ordered by id.
- `POST /asks/{ref}/answer` `{answer (1–20000), channel? (ui|api|relay; default ui for a session, api for a key),
  on_behalf_of? (a handle; relay only)}`: open → answered; `ask_answered`. `POST /asks/{ref}/withdraw` `{reason?}`:
  open → withdrawn; `ask_withdrawn`. Either on a non-open ask is 409 naming its state.
- A task is `blocked` (§4.3) while it has an open dependency **or** an open blocking ask; `GET /tasks/{ref}` lists
  its `open_asks`; `GET /tasks?blocked=true` follows the new rule.
- **Tests:** Godog on both databases: raise on a task, on an initiative, global; each validation refusal; task +
  mismatched initiative → 400; ask on closed task and closed initiative → 409; read by `A<n>` and ULID (either
  case); each list filter and paging; answer with each channel and a relay `on_behalf_of`; withdraw; answer or
  withdraw a non-open ask → 409; `blocked` true with an open blocking ask, false for a non-blocking ask and after
  the answer; the three events on the task's timeline.

### R2 Ask long-poll and "asker waiting"

- `GET /api/v1/asks/{ref}?wait=N` (1 ≤ N ≤ 300; outside is 400): returns at once when the ask is not open,
  otherwise holds the request until it is answered or withdrawn (answering with the new state) or N seconds pass
  (answering with the still-open ask, 200). The waiter is woken by the same change feed as the stream (R8), so an
  answer written through another server process on the same database ends the wait within the feed's tail
  interval.
- While a wait is held the server sets `poll_seen_at` to now at its start, at least every 30 s, and at its end.
  `asker_waiting` is true when the ask is open, its mode is `poll` and `poll_seen_at` is within the last 60 s. This
  is stored, not held in memory, so it is right with several replicas.
- **Tests:** Godog on both databases: wait on an answered ask returns at once; wait answered mid-wait returns the
  answer before N; wait times out with the open ask; withdraw mid-wait; `asker_waiting` true during a wait and
  false 60 s after it ends (injected clock); N = 0 and N = 301 → 400. A Go test with two server instances on one
  Postgres: a wait held on one is ended by an answer written through the other.

### R3 Notes and cursors

- Migration creates `notes` (§4.7) with short ref `N<n>` and the §4.10 indexes, and `cursors` (§4.8), on both
  databases.
- `POST /api/v1/notes` `{summary (1–200), body? (≤ 20000), task?, initiative?, resource? (1–100), mentions?
  (≤ 20 handles)}`. `initiative` is derived from `task`; task and a different initiative is 400. 201. **Every
  note writes `note_posted`** (broadcasts included, with no task or initiative), so the change feed sees all of
  them; §4.7 is corrected accordingly.
- `GET /api/v1/notes`: the §6.2 note filters `since=<ULID|N<n>|RFC 3339|cursor>`, `task`, `initiative`,
  `resource` (repeatable), `broadcast=only|exclude`, `mention`, `author`, `fields=summary|full` (default summary:
  no `body`, but `has_body`), `limit`/`after` paging, ordered by id. `since=cursor` uses the caller's `notes`
  cursor (none set: from the start). `ack=true` moves the caller's `notes` cursor to the last item returned.
  `GET /api/v1/notes/{ref}` returns the full note.
- `PUT /api/v1/cursors/{stream}` `{position}` (`notes` takes a note ULID or `N<n>`, `events` an event ULID or
  `E<n>`): sets the caller's cursor; an unknown stream is 404, a position naming nothing is 400. `GET
  /api/v1/events?since=cursor` uses the `events` cursor the same way.
- **Tests:** Godog on both databases: post bound to a task (initiative derived), an initiative, a resource, a
  broadcast, with mentions; each validation refusal; `note_posted` for each kind; each filter including both
  `broadcast` values and each `since` form; `fields`; `ack=true` then `since=cursor` returns only newer notes;
  `PUT /cursors` for both streams, unknown stream 404, unknown position 400.

### R4 Attention

- `GET /api/v1/attention` `?tag=…&initiative=…` (repeatable, as for tasks): the five §5.3 sections, each newest
  relevant first, each item carrying the actors involved (handle and way back):
  1. `open_asks`: ask ref, question, task, asker, mode, `asker_waiting`, `blocking`, age;
  2. `done_not_closed`: task, `done_by`, `done_at`, the outcome of the task's last ended lease;
  3. `expired_leases`: expired leases with no `acked_at` and no later lease on the same task: lease, holder, task,
     intent, last renewal;
  4. `overdue_watches`: task, `next_check_at`, last `check_recorded` summary;
  5. `conflicts`: resource, holders, tasks (§5.2).

  Plus `counts` `{open_asks, done_not_closed, expired_leases, overdue_watches, conflicts, total}`.
  `?fields=counts` returns only `counts`.
- **Tests:** Godog on both databases: an empty board has every count 0; each section populated and cleared by the
  action that clears it (answer or withdraw the ask; close the task; ack the lease; start a later lease on the
  task; record the check; end one of the conflicting leases); `tag` and `initiative` filters; `fields=counts`.

### R5 `GET /now` and `board now` complete

- `GET /api/v1/now` gains `my_asks` (open asks I raised, plus mine answered since my `notes` cursor or in the last
  24 h, with the answer), `notes` (notes since my `notes` cursor that concern me: on tasks I hold a live lease on,
  on resources of my live leases, mentioning me, or broadcasts; summaries, at most 20, plus `notes_more`) and
  `attention` (the R4 counts). Reading `/now` moves no cursor.
- `board now` prints the new sections `my asks` and `notes` (one line each, `ANSWERED` prefixed for an answered
  ask) and an `attention` line with the counts; exit codes unchanged (7 on conflict).
- **Tests:** Godog API and CLI scenarios: each new part empty and populated; a note on another actor's task that
  does not concern me is absent; a mention is present; a broadcast is present; acked notes are absent.

### R6 CLI for asks, notes and attention

- Commands (flags in either position, `--json` on each, §8.1 output and exit codes, short refs everywhere):
  - `board ask <task> "<question>" [--details file|-] [--option a]... [--poll D | --stop] [--blocking]`: `--stop`
    (default) prints `ok ask A<n> raised` and exits 0; `--poll D` raises the ask as `poll`, then waits with
    repeated `?wait=` calls (each ≤ 300 s) until the deadline: an answer prints `ok A<n> answered by <h>: <answer>`
    (exit 0), a withdrawal prints it and exits 6, the deadline prints `A<n> still open → board ask show A<n>` and
    exits **3**.
  - `board ask show <A…>`, `board ask ls [--state …] [--mine] [--task ref]`, `board ask withdraw <A…> [reason]`.
  - `board answer <A…> "<answer>" [--relay] [--for <handle>]`.
  - `board note "<summary>" [--body file|-] [--task ref] [--init slug] [--res r] [--mention h]...`.
  - `board notes [--since last|<time>|<N…>] [--task ref] [--init slug] [--broadcast] [--res r]... [--mention h]
    [--ack]` (`last` is the cursor), one line per note with a `+` marker when it has a body; `board note show
    <N…>` prints the body.
  - `board attention [--tag t]... [--init slug]`: the five sections with their counts, one line per item.
- `answer` or `withdraw` of a non-open ask exits 6; an unknown `A…`/`N…` exits 4.
- **Agent protocol v2**: `internal/cli/protocol.txt` gains ask (stop vs poll and exit 3), answer `--relay`, note,
  notes `--since last --ack`, and attention; heading `board protocol v2`; golden file updated; the §8.6
  command/flag existence feature and the 45-line / 100-column budget pass.
- **Tests:** Godog CLI scenarios for each command's success line, a refusal with its `→ next` and exit code,
  `--json`, the `--poll` paths (answered mid-wait, withdrawn, exit 3 with a short deadline); unit tests for the
  poll loop's deadline arithmetic; the existing help-agent feature.

### R7 The SSE stream

- `GET /api/v1/stream?topics=events,attention` (default both; an unknown topic is 400), authenticated like every
  other route (session cookie or key). `Content-Type: text/event-stream`, `Cache-Control: no-cache`,
  `X-Accel-Buffering: no`; each message is flushed as written.
- Topic `events`: one SSE message per event row, `event: board`, `id: <event ULID>`, `data:` the event as in
  `GET /events` (one JSON line). Topic `attention`: `event: attention`, `data:` the R4 `counts`, sent once on
  connect and whenever they change. Counts are recomputed after new events and at least every 30 s (overdue watches
  and expiries change with time).
- **Resume:** a `Last-Event-ID` header (or `?last_event_id=`) replays every event after it, in order, before live
  ones, with no gap and no duplicate. When more than 1000 events would replay, or the id is unknown, the server
  sends one `event: reset` instead and continues live; clients then refetch.
- **Change feed:** the server tails `events` by id (interval `BOARD_STREAM_POLL`, default `1s`) and a local write
  wakes the tail at once. The same feed wakes ask waiters (R2). Several servers on one database each see every
  event.
- A comment heartbeat (`: ping`) every `BOARD_STREAM_HEARTBEAT` (default `25s`). A stream outlives the server's
  write timeout (deadlines extended per write) and ends cleanly on client disconnect or server shutdown, leaking no
  goroutine.
- **Tests:** Godog or Go HTTP tests on both databases: an event written after connect arrives within 2 s; topic
  filtering; `Last-Event-ID` replays exactly the missed events then continues live; an unknown id and an
  over-1000 gap each get `reset`; the attention message on connect and after a change (raise an ask) and not
  after a change that leaves counts equal; heartbeat at a short configured interval; no auth → 401; a stream kept
  open past a short configured write timeout; goroutine count back to baseline after disconnect. Two servers on one
  Postgres: an event written through one arrives on a stream from the other.

### R8 Live refresh in the UI

- One `EventSource` per tab in a `web/src/lib/live.ts` module. Each view declares which events concern it (a
  predicate over event type, initiative and task); a matching event refetches that view's data, debounced to
  at most one refetch per 500 ms burst, so a change shows within a few seconds. `attention` messages update the
  nav badge without a fetch. A `reset` or a reconnect after an error refetches the visible view.
- Refetching never discards user input: an open dialog keeps its fields, a form keeps its values, scroll position
  and expanded rows are kept. A row the user is looking at that disappears (closed elsewhere) goes away without an
  error toast; a task page whose task changed status shows the new status.
- A **live indicator** in the shell: `live` when the stream is open, `reconnecting` after an error, `offline` when
  the browser is offline; text plus colour, with an accessible name, visible on phone and desktop.
- Views covered: Attention, Initiatives list, initiative page, Tasks (and its Now panel), task page, Notes,
  Settings → API keys.
- **Tests:** Vitest for `live.ts` with a fake `EventSource` (connect, message routing by predicate, debounce,
  reset and reconnect trigger a refetch, indicator states). Playwright at phone and desktop: for each view above,
  open it, make a change through the API (a request from the test, not the page), and see it appear within 5 s
  without reload; with a dialog open and text typed, a change through the API keeps the typed text; the indicator
  shows `live`, then `offline` with `context.setOffline(true)`, and after `setOffline(false)` a change made while
  offline appears.

### R9 UI: Attention view

- Route `#/attention`, the **default route** (`#/` lands here), first in the nav with a count badge from the
  stream. Five sections in §5.3 order, each with its count and a calm empty state; each item shows the actors
  involved and a **copy way back** button.
- Inline actions: **Answer** an ask (dialog: the question, details as safe Markdown, option buttons that fill the
  answer, free text), **Withdraw** an ask, **Close** a done task (resolution select), **Acknowledge** an expired
  lease, **Record check** on an overdue watch, and links to the task and initiative. Each action shows refusals
  inline and a toast on success; the item leaves its section through the live refresh.
- Filters `tag` and `initiative` held in the URL fragment's query.
- **Tests:** Vitest for the section presentation (ages, asker-waiting label, empty states) and the filter ↔ URL
  encoding. Playwright at phone and desktop: each section populated through the API, each inline action clears its
  item, answer with an option button and with free text, copy way back puts the text on the clipboard, filters
  survive reload, `#/` opens Attention.

### R10 UI: asks and notes on tasks and initiatives, and the Notes view

- Task page: an **Asks** section (open first, then answered and withdrawn; question, asker, mode, blocking,
  answer, who answered and how) with **Answer** and **Withdraw**, and a **Notes** section (newest first, summary
  with expand-to-body, author, time) with **Post note**. The initiative page shows its open asks and notes the same
  way. A blocking open ask shows in the task's `blocked` badge with its reason.
- **Notes** view `#/notes` in the nav: newest first, summaries with expand-to-body, filters (broadcast only, task,
  initiative, resource, author, mention) in the URL fragment's query, paging by "load more", **Post note** (summary,
  body, optional task, initiative, resource, mentions).
- Note bodies and ask details render as safe Markdown (the Phase 1 renderer).
- **Tests:** Vitest for the notes filter ↔ URL encoding and the ask-state presentation. Playwright at phone and
  desktop: raise an ask through the API and answer it on the task page; post a note on a task, on an initiative and
  a broadcast from the UI and find each in the Notes view under the matching filter; expand a body; load more.

### R11 Nav and shell

- The route table gains Attention (default) and Notes; the phone tab bar holds Attention, Initiatives, Tasks,
  Notes and a "more" entry for the rest if they do not fit in 44 px targets at 390 px. The Attention badge shows
  `counts.total` (hidden at 0, `99+` above 99) with an accessible label.
- **Tests:** the `shell` spec extended: route table order, default route, badge values 0 / 3 / 120, tab bar fits
  at phone with 44 px targets.

### R12 Migration safety

- Migrations are additive: a Phase 1 database with initiatives, tasks, leases, keys, sessions and events upgrades
  and reads every row back.
- **Tests:** a Go or Godog test migrates a populated Phase 1 database on both legs and reads the rows back.

### R13 UI quality bars extend to the new views

- Every new view (`#/attention`, `#/notes`) and every new dialog (answer ask, withdraw ask, post note, close from
  attention, acknowledge, record check from attention) and the live indicator meet the Phase 0b bars: no
  horizontal scroll with the longest accepted values, 44 px / 16 px on the phone, axe zero serious/critical at
  phone and desktop in both themes, same-origin requests only (the stream included), focus trapped and returned,
  `aria-current` on the nav.
- **Tests:** the existing `overflow`, `touch`, `a11y`, `sameorigin`, `dialogs` and `shell` specs are extended with
  these views, dialogs and the indicator as finite listed states (each named in the spec file).

### R14 The spec matches the code

- `SPEC.md` describes in present tense: §4 (`asks` with `poll_seen_at`, `notes`, `cursors`; every note writes
  `note_posted`), §5.3 (as built), §6 (§6.6 Asks, §6.7 Notes and cursors, §6.8 Attention in the §6.3 style; `/now`
  complete; `since=cursor` on events), §7 (§7.1 as built, with `reset`, `BOARD_STREAM_POLL`, the tail of
  `events` as the change feed for several replicas; §7.2 woken by that feed instead of `LISTEN/NOTIFY`, and
  `poll_seen_at`), §8.1 (exit 3 reachable, the new commands in the exit-code table), §8.3 (shipped commands and
  their output lines), §8.6 (protocol v2), §11 (Attention, Notes, task and initiative page sections, the live
  indicator, the nav), §13 (`BOARD_STREAM_POLL`, `BOARD_STREAM_HEARTBEAT`) and §16 (Phase 2 shipped, one line; SSE
  moved from the Phase 3 row to Phase 2). `api/openapi.yaml` declares every new operation, the stream included.
  Updated in the same commit as each behaviour, never in a docs-only task.

## Materiality rubric

Reject only on substance:

- a requirement's behaviour is absent or wrong in the live code;
- a test the requirement names is missing, red, or asserts the opposite of the requirement;
- the spec contradicts the live code;
- a secret is exposed.

Everything else verifies, with the gap recorded as a residual note: wording, form, provenance, process, visual
taste, and **any number or claim in prose outside the spec** (commit messages, notes, issue comments, run
artifacts). Prose outside the spec is never a blocking ground.

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
- **Plan shape.** Pre-split at plan time: R1, R3 each into migration/store, API, and their CLI from R6; R2 after
  R7's change feed exists (the feed is its own task, before the stream handler); R4 into store queries and API;
  R5 API and CLI; R6's protocol v2 as its own task after the commands exist; R7 into change feed, stream handler,
  resume/reset; R8 into `live.ts` and one task per group of views; R9, R10 each into lib modules plus views; R11
  one task; R13 as one task per spec file. Backend before the UI that needs it.
- **Screenshots for the operator.** Whenever a task changes how a view looks, take Playwright screenshots of that
  view at phone and desktop in both themes into `/run/ralphd/artifacts/screenshots/` (never committed) and look at
  them before calling the task done. Send a few to the operator with the telegram-notify skill at each milestone
  (backend API done, stream done, Attention view done, live refresh across views done, sweep green).
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
  never an edit to `ci/quality.json`. The protocol v2 text not fitting 45 lines is a petition, not a raised
  budget.
- **Engine defects to avoid by hand:**
  - every requirement here names its finite cases, and so must the tasks;
  - do not signal COMPLETE while tasks are pending;
  - when an approach ends in review, read the findings before replanning;
  - after any cure pass, check `tasks.json` for a task that depends on itself, and repair it by steer.

## Non-negotiables

- **No weakened test, and no gate loosened or skipped.** No `enabled: false`, no warn-only mode, no exemption
  or baseline file, no `// nolint` or `eslint-disable` without a stated reason on the line, no axe rule
  disabled, and no `skip` in a feature, spec or test. A test pinning behaviour this wave changes (the default
  route, the nav, `blocked`, `/now`'s shape, protocol v1) is rewritten in the same commit and keeps every
  behaviour it asserted that still holds.
- **No raised timeout, budget, worker count or per-test bound.**
- **Playwright never retries**, and no visual-snapshot (pixel-diff) assertions. Time-dependent tests use an
  injected clock or the configurable intervals (`BOARD_SWEEP_INTERVAL`, `BOARD_STREAM_POLL`,
  `BOARD_STREAM_HEARTBEAT`), never sleeps longer than a few seconds. Live-refresh tests wait on the DOM (an
  `expect(...).toBeVisible()` with at most a 5 s timeout), never on a fixed sleep.
- **Nothing in the UI polls on a timer.** Live data comes from the stream; a refetch happens on an event, a
  `reset`, a reconnect or a user action.
- **No secret** in a commit, a log line, an event, a golden file, a screenshot or a test fixture.
- **No homelab specifics in the repository** (hostnames, tailnet names, personal paths), except for the
  Jenkins job URL in `docs/ci.md` and the badge URLs in the README. The repository is meant to go public.
- **No external network request** from the UI or from tests beyond the containers the suite starts.
- **Migrations are additive and forward-only** (R12).

## Budget

- 500 iterations, 3 approaches, a 48 h wall clock and a 120-minute iteration cap.
- Peer plan review and the peer verdict gate are on.
- Vigilant, reflect and router task selection are on.
- Docker access is on (sibling toolchain, testcontainers). Host networking is on (Jenkins is tailnet-only).
