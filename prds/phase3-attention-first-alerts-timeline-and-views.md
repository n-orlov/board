# Phase 3: attention first, alerts, the timeline and the remaining views

## What the operator gets

1. **Attention first everywhere** (SPEC §5.3, §6.4, §11): a task that needs the operator says so wherever it
   appears. Three reasons make a task need attention:
   - **stranded**: its newest lease expired, nobody acknowledged it, and no later lease started (the §5.3
     `expired_leases` rule, applied per task);
   - **asking**: it has an open ask (waiting for the operator's input), marked `blocking` when the ask is;
   - **done**: it is done and not closed.

   Each reason is a badge on task rows, on the initiative page and in the task header. The Tasks view and the
   initiative page list the tasks that need attention first, in their own section. The Initiatives list shows a
   per-initiative attention count and sorts initiatives with attention first. `board task ls` prints the same
   reasons and lists those tasks first. The task page of a stranded task shows the expired lease (holder, intent,
   last renewal, way back) with **Acknowledge**.
2. **Alerts out of the board** (new §5.4): when a task starts needing attention, the server POSTs one JSON message
   to a configured webhook URL (`BOARD_NOTIFY_URL`), at least once, from an outbox that survives restarts. Off when
   the URL is unset. The body carries a ready-to-send `text` plus structured fields, so a plain text relay works
   as is.
3. **The Timeline view** (§11): Gantt-style with `vis-timeline`: initiatives → tasks, planned and actual spans,
   lease sub-bars by state, markers, dependency arrows, zoom, filters, and the archive window. Served by
   `GET /timeline`.
4. **The Actors view and the Archive view** (§11): who is alive now with their way back (`GET /actors`); closed
   tasks and initiatives in a 7/30/90-day/all window with reopen.
5. **The inbox**: a built-in `inbox` initiative, there on every board, so an agent always has a place for work no
   other initiative fits.
6. **Wide screens** (R13): single-table views capped at 1280 px; from 1536 px, Tasks and the Now panel, the task
   page and the initiative page go side by side, and Attention lays its sections out in a grid.
7. **Done and active are exclusive** (R14): a done task never holds a live lease.
8. **View state that sticks** (R16): every table sorts by clicking its column headers, and each view's filters
   and sort are kept in `localStorage`, so leaving a view and coming back (nav, back, a new tab) restores them.
9. **Fixes found after Phase 1**: the image reports its version, the PWA manifest and icons load without a
   session, the SQLite WAL is checkpointed, the README describes the shipped product.

This is SPEC §16 Phase 3 plus the attention-first work, the webhook (moved in from §17) and the fixes. `SPEC.md` is
the product spec; where this PRD and `SPEC.md` disagree, **this PRD wins** and the spec is corrected in the same
commit (R15).

### Why attention is derived per task, not stored

The Attention view (Phase 2) already derives its sections at one instant from leases, asks and task status. A
stored "needs attention" flag would have to be kept in step by every write and by the expiry sweep, and would drift
the first time one path forgot. Deriving it per task with the same rules as §5.3 (one function shared by the
attention page and the task documents) keeps one definition, and a task's badges and the attention page can never
disagree.

### Why an outbox for alerts

A webhook sent inline from the request would be lost if the server stops between COMMIT and the POST, and would
slow every write by the receiver's latency. Writing a row to a `notifications` outbox in the same transaction as
the event that causes it, and delivering it from a background worker with retries, gives at-least-once delivery
across restarts and across several replicas on one Postgres (a delivery is claimed by a conditional update).

## Context the run needs

- Phases 0, 0b, 1 and 2 are done, deployed and green at the commit that adds this PRD. Read `SPEC.md` (all of
  §3–§8, §11, §13–§17), `prds/phase2-asks-notes-attention-and-live-refresh.md`, `api/openapi.yaml`, `internal/`
  (`internal/attention` above all), `web/`, `features/`, `e2e/` and `docs/ci.md` before planning.
- The Phase 2 slices are the pattern to follow end to end: OpenAPI first, generated strict server and TS types,
  `internal/<area>/` service, store queries on SQLite and Postgres, the events row in the same transaction, CLI on
  `internal/cli/client.go`, Godog features on both databases, Vitest for `web/src/lib/`, Playwright for the views,
  live refresh through `web/src/lib/live.ts`. Components stay thin; logic lives in `lib/` modules with unit tests.
  The CRAP 8 ceiling (Go and TS), coverage, Stryker and gremlins apply to all new code.
- The UI rules from Phase 0b apply to every new view and dialog: one `Dialog` component (bottom sheet on the
  phone), no horizontal scroll, 44 px hit areas and 16 px inputs on the phone, axe clean in both themes, same
  origin only, the route table drives router and navs, tables from 768 px and cards below.

Symbols and section numbers refer to `SPEC.md` and the code at the commit that adds this PRD. Drift is expected and
is not a finding: if a reference has drifted, find the current site and carry on.

## Issues in scope

None filed. This PRD is the whole statement.

**Not in scope:** the shortcuts `start`/`finish`/`ping`/`catchup`/`new`/`block`, `Idempotency-Key` replays,
`?view=compact`, `board update`, `install.sh`, releases, the skill pointer and the performance verification
(Phase 4); the initiative page's own timeline; the task queue, ACLs, MCP (§17); automatic acknowledgement of
expired leases (§18 question 4 is closed as "no": acknowledgement stays manual, and the stranded reason and its alert replace
it); retention or compaction; the homelab deployment.

**Agent protocol v3.** `board task done` now ends the caller's own lease, so agents behave differently and the
protocol text (§8.6, `internal/cli/protocol.txt`) becomes v3 with exactly these changes: `board task done <ref>
["outcome"]` ends your own lease on the task (no separate `lease end` first); `board lease start` on a done task
is refused (reopen it first with `board task reopen`); `board lease ack L<n>` acknowledges a lease that ran out;
`board task done --force` is used only when the operator says so in so many words (as `task close --for
operator`). The 45-line / 100-column budget holds. `help_agent.feature` asserts the version and the new
commands.

## Requirements

Each requirement is a behaviour plus the tests that fail without it. "Both databases" means the Godog feature runs
on the SQLite and the Postgres leg. "Phone", "desktop" and "wide" are the Playwright viewports 390×844, 1440×900 and 2560×1080. Every
write below appends its event (§4.6) in the same transaction as the change; a refused request changes nothing (no
row, event, actor or notification).

### R1 Attention reasons on tasks

- One derivation in `internal/attention` gives, for a task at one instant (after expiring due leases, §5.1), its
  `attention` list: zero or more of exactly these reasons, in this order:
  1. `stranded`: the task's newest lease (by `started_at`, then `id`) is `expired` with no `acked_at`. Equivalently, the task has an
     item in §5.3 `expired_leases`.
  2. `asking`: the task has at least one open ask. The reason carries `blocking: true` when any open ask on it is
     blocking, and `asker_waiting: true` when any is (§5.3).
  3. `done`: the task's status is `done` (done and not closed).

  A closed task has no reasons. A done task whose newest lease expired unacknowledged has both `stranded` and
  `done` (operator decision: the dead lease still wants an acknowledgement). The Attention view's
  `expired_leases`, `open_asks` (task-bound ones) and `done_not_closed` sections and the per-task reasons come from
  this one derivation, so they always agree. `overdue_watches` and `conflicts` stay in `internal/attention` as
  today and are not per-task reasons. The nav badge and SSE `attention` counts stay the view's item counts (all
  five sections, initiative and board asks included); `attention_count` counts tasks, so the two numbers differ by
  design.
- `GET /tasks/{ref}` and every `GET /tasks` item carry `attention: [{reason, blocking?, asker_waiting?}]` (empty
  list when none). A stranded task also carries `stranded_lease`: `{ref, holder: {handle, way_back?}, intent,
  renewed_at, expired_at}`.
- `GET /tasks?attention=true|false` keeps the tasks whose `attention` list is non-empty (or empty); any other value
  is 400. It is one more condition ANDed with the existing filters and pages as today (id order); the tested
  combinations are with `initiative`, with `status=done` and with `active=true`.
- `GET /initiatives` items and `GET /initiatives/{slug}` carry `attention_count`: the number of tasks of the
  initiative with a non-empty `attention` list.
- **Tests:** Godog on both databases, one scenario per case: a task with an expired unacknowledged lease is
  `stranded` with its `stranded_lease`; acknowledging the lease clears it; a later lease (any holder, any state)
  clears it; a task with an open non-blocking ask is `asking` without `blocking`; a blocking ask sets `blocking`;
  answering or withdrawing the last open ask clears it; a done task is `done`; closing it clears it; reopening it
  clears it; a task with all three reasons lists them in order; a closed task with an expired lease has none;
  `attention=true` and `attention=false` with and without `initiative`; a bad value → 400; `attention_count` on
  the list and on one initiative. A Go test asserts that for a seeded board the per-task reasons and the three
  Attention sections name the same tasks.

### R2 Attention first in the UI

- A `web/src/lib/attention.ts` module turns a task's `attention` list into badges (label, tone, accessible name):
  `stranded` (warning tone), `waiting on you` for `asking` (`blocking` adds "blocking", `asker_waiting` adds
  "agent waiting"), `done, not closed`. Text plus colour, never colour alone.
- **Tasks view** and **initiative page**: a **Needs attention** section above the other tasks, listing the
  matching tasks that need attention (fetched with `attention=true` under the view's filters, `limit=200`; beyond
  200 a `+K more` line links to the Tasks view filtered by `attention=true`), ordered
  stranded, then asking (blocking first), then done, each group by most recent change first. The tasks below it
  are the rest, as today. The section hides when empty. Each row shows its badges.
- **Initiatives list**: an attention count chip on each initiative with a non-zero `attention_count`, and the list
  ordered initiatives with attention first (then as today).
- **Task page**: the badges in the header. A stranded task shows a **Stranded** panel: holder, intent, last
  renewal, expiry, a **copy way back** button and **Acknowledge** (`POST /leases/{id}/ack`), which clears the panel
  through the live refresh.
- The nav's Attention badge (Phase 2) is unchanged.
- **Tests:** Vitest for `attention.ts` (each reason and flag combination's label and accessible name; the section
  ordering function on a mixed list). Playwright at phone and desktop: through the API make one task stranded,
  one asking (blocking), one done; each appears in Needs attention on the Tasks view and on its initiative page,
  in that order, with its badge; acknowledging from the task page removes the stranded task from the section
  within 5 s without reload; answering the ask and closing the done task each remove theirs; the Initiatives list
  shows the count and puts that initiative first; the section is absent on an initiative with none.

### R3 Attention first in the CLI

- `board task ls` lists the tasks that need attention first (same order as R2), then the rest in id order, and
  appends their reasons to the line: `!stranded`, `!asking` (`!asking(blocking)` when blocking), `!done`. `--attention`
  keeps only those tasks (the `attention=true` filter). `--json` carries the API's `attention` and
  `stranded_lease` fields. `board task show <ref>` prints an `attention:` line and, for a stranded task, a
  `stranded: L<n> by <holder>: "<intent>" expired <age> ago → board lease ack L<n>` line.
- `board lease ack <L…>` acknowledges an expired lease (`ok lease L<n> acknowledged`); a lease that has not expired
  exits 6; an unknown ref exits 4. It calls the existing `POST /leases/{id}/ack`.
- `board init ls` appends ` !<count>` to the line of each initiative with a non-zero `attention_count`.
- **Tests:** Godog CLI scenarios for: the ordering and markers on a seeded board; `--attention`; `--json` fields;
  the `task show` lines; `lease ack` success, not-expired exit 6, unknown exit 4; `init ls` counts.

### R4 Webhook alerts

- Configuration (§13): `BOARD_NOTIFY_URL` (unset: alerts off, no outbox rows written), `BOARD_NOTIFY_ON`
  (comma list of `stranded`, `asking`, `done`; default all three; any other word makes `serve` exit 1),
  `BOARD_NOTIFY_TIMEOUT` (default `10s`), `BOARD_NOTIFY_HEADER` (optional `Name: value` sent with every POST, for a
  receiver that wants a token; never logged, never in an event).
- **Triggers**, exactly these, each enqueuing one notification in the same transaction as its event:
  1. `stranded`: a lease expires (`lease_expired`) and it is its task's newest lease;
  2. `asking`: an ask is raised on a task, an initiative or the board (`ask_raised`);
  3. `done`: a task is marked done (`task_done`).

  A notification is enqueued only for the kinds in `BOARD_NOTIFY_ON`. Renewals, ends, answers, closes and notes
  enqueue none. "Newest" is by `started_at`, then `id`, as in R1. A notification is delivered even if its condition
  has cleared since (lease acknowledged, ask answered); nothing cancels an outbox row.
- Migration creates `notifications` (`id` ULID, `kind`, `event_id` fk, `payload` JSON, `state`
  `pending|sent|failed`, `attempts`, `next_attempt_at`, `claimed_until`, `last_error`, `created_at`, `sent_at`),
  unique on `(event_id, kind)` so an event never enqueues twice, on both databases.
- **Payload**: `{"text": "<one or two lines>", "kind", "task"?: {ref, title, url}, "initiative"?: {slug},
  "actor": {handle, way_back?}, "ask"?: {ref, question, blocking}, "lease"?: {ref, intent, expired_at},
  "board_url"}`. `text` is plain text a person can read on a phone, naming the kind, task ref and title, the actor
  and, for `stranded`, the intent and the way back (for an initiative or board ask, the initiative slug or
  "board" in place of the task); `url` and `board_url` are built from `BOARD_PUBLIC_URL`
  (absent fields when it is unset). The payload never carries a key, a session or a header value.
- **Delivery**: a background worker in `serve` claims due rows with a conditional update (`claimed_until` a short
  lease, so several servers on one database never deliver the same row concurrently), POSTs the payload
  (`Content-Type: application/json`) with the timeout, and marks it `sent` on any 2xx. Otherwise it records
  `last_error` (status or error class, no body), and retries with backoff 30 s, 2 min, 10 min, 30 min, 1 h; after
  the fifth failed retry the row is `failed`. A row claimed by a server that died is retried once its claim lapses.
  Delivery is at least once; the receiver may see a duplicate after a crash between the POST and the update.
  The worker's tick, claim lapse and backoff run on the server's injected clock. `BOARD_NOTIFY_URL` comes from the
  environment only; no real receiver URL appears in the repository, the spec or its examples.
- `GET /api/v1/notifications?state=…` lists outbox rows (newest first, paged) for the operator to see failures.
- **Tests:** Go tests with an `httptest` receiver, on both databases: each trigger delivers exactly one POST with
  the documented payload fields and a readable `text`; a non-newest expired lease (a later lease exists) enqueues
  nothing; renew, end, answer, close and note enqueue nothing; `BOARD_NOTIFY_ON=done` delivers only `done`;
  unset `BOARD_NOTIFY_URL` writes no row; a 500 then 200 receiver retries and ends `sent` (injected clock for the
  backoff); six failures end `failed`; a hung receiver hits the timeout (`BOARD_NOTIFY_TIMEOUT=200ms` in that
  test) and is retried; `BOARD_NOTIFY_HEADER` is
  sent and appears in no log line or event; a refused write enqueues nothing; two servers on one Postgres deliver
  each row once; a row claimed by a stopped server is delivered by the other after its claim lapses; a bad
  `BOARD_NOTIFY_ON` makes `serve` exit 1. Godog on both databases for `GET /notifications` filters.

### R5 `GET /timeline`

- `GET /api/v1/timeline?from=&to=` (RFC 3339; `to` after `from`; at most 400 days apart; defaults: the last 14
  days to 7 days ahead), filters `initiative`, `tag` (repeatable, as for tasks), `actor` (a handle: keeps tasks on
  which that actor held a lease in the window) and `closed_within` (as for tasks; without it closed tasks are left
  out). Answers `{from, to, initiatives: [{slug, title, tags, closed}], tasks: [{ref, initiative, title, status,
  kind, planned_start?, planned_end?, created_at, closed_at?, attention, deps: [ref]}], leases: [{ref, task,
  holder, state, started_at, ended_at? (ended, expired: the expiry), intent}], markers: [{task, type (milestone,
  ask, check, done, closed), at, label}]}`, each limited to the window (a lease or task overlapping the window is
  in), in stable order.
- **Tests:** Godog on both databases: a seeded board answers the expected tasks, leases and markers for a window;
  each filter; a window that cuts a lease keeps it; closed tasks absent without `closed_within` and present with
  it; `to` before `from`, over 400 days and bad times → 400.

### R6 UI: Timeline view

- Route `#/timeline` in the nav. `vis-timeline` (bundled, no CDN): groups are initiatives (collapsible) → tasks; per
  task a light planned bar and an actual span (created → closed, or → now), lease spans as stacked sub-bars
  coloured by state (live, ended, expired) with text in their tooltip, markers for milestones, asks, checks, done
  and close, dependency arrows between tasks. Zoom from hour to quarter (buttons plus wheel/pinch). A task that
  needs attention shows its R2 badge on its row label. Clicking a task or lease opens the task page.
- Filters in the URL fragment's query: initiatives, tags, actor, "show closed within N days" (off, 7, 30, 90). The
  visible window follows panning and refetches `GET /timeline` for the new range, debounced. Live refresh (R8 of
  Phase 2): events in the window refetch it.
- On the phone the view fits 390 px with no horizontal page scroll (the timeline scrolls inside its own frame) and
  the controls keep 44 px targets.
- **Tests:** Vitest for the `lib/` mapping from the API answer to vis-timeline items and groups (each lease state,
  each marker type, dependency pairs, attention label) and the filter ↔ URL encoding. Playwright at phone and
  desktop: a seeded board renders the expected groups, items and markers (asserted through the DOM, not pixels);
  collapse a group; zoom in and out; each filter changes the rendered tasks and survives reload; a lease started
  through the API appears within 5 s (the view refetches `GET /timeline` on stream events of the `lease_*`,
  `task_*`, `ask_*` and `milestone_*` kinds, never on a timer); clicking a task opens its page.

### R7 Actors and Archive

- `GET /api/v1/actors` and `GET /api/v1/actors/{handle}`: each actor with `handle`, `way_back`, `last_seen`, its
  live leases (ref, task, intent, expiry, resources) and its open asks; the list ordered by `last_seen`, newest
  first, `alive=true` keeping actors with a live lease.
- **Actors** view `#/actors`: alive actors first (live leases with task links), then the rest by last seen, a
  **copy way back** button on each. **Archive** view `#/archive`: closed tasks and closed initiatives in a window
  (7, 30, 90 days, all; default 30) held in the URL, newest closed first, resolution shown, **Reopen** on each
  (task: `board task reopen` rules; initiative: reopen).
- **Tests:** Godog on both databases for `/actors` (fields, ordering, `alive`, unknown handle 404). Vitest for the
  window ↔ URL encoding. Playwright at phone and desktop: Actors lists an actor with a live lease first and copies
  its way back; Archive shows a task freshly closed through the API under the 7-day window; a task whose
  `closed_at` the e2e store moves 30 days back (as `attention.spec.ts` moves expiry) is absent under 7 days and
  present under `all`; a closed initiative appears likewise; Reopen on the task brings it back to the Tasks view,
  and Reopen on the initiative brings it back to the Initiatives list.

### R8 Built-in inbox

- On start, `serve` creates the initiative `inbox` (title `Inbox`, description "Work that fits no other initiative
  yet.", no tags), with its `initiative_created` event, when no initiative with that slug exists; an existing
  `inbox`, open or closed, is left exactly as it is. Several servers starting together create it once. The event's
  actor is the operator actor (`BOARD_OPERATOR_HANDLE`, kind `operator`), created by this step if absent, and its
  principal is `system:bootstrap`. `inbox` is ordered like any other initiative (no pinning).
- **Tests:** Godog or Go on both databases: a fresh database has `inbox` with one creation event; a database with
  an `inbox` of a different title and tags keeps them; a closed `inbox` stays closed; two servers started on one
  Postgres leave one `inbox` and one event.

### R9 Fixes found after Phase 1

- **Version:** the image reports the build's version: the `Dockerfile` takes `ARG VERSION` (and `REVISION`) and
  stamps them with `-ldflags -X github.com/n-orlov/board/internal/version.version=$VERSION` (and `.revision`),
  which `version.Current()` prefers over build info when set; `Jenkinsfile` passes the tag or short sha. `GET
  /meta` and `board version` show them; a build with neither args nor VCS info reports `devel`. Test: a Go test
  builds the binary with `-X` values and asserts `/meta` and `board version` report them; a `ci/` check asserts the
  `Dockerfile` declares and uses both args.
- **PWA assets without a session:** `/manifest.json` and the icons it names (`icon-192.png`, `icon-512.png`, `icon.svg`, `apple-touch-icon.png`, `favicon.svg`)
  answer 200 with the right `Content-Type` and no session or key (the `crossorigin="use-credentials"` workaround in `web/index.html` is then removed), while every other non-public route still needs
  one (`internal/server/auth.go` exempts only `GET /healthz` today). Test: a Go HTTP test fetching each asset
  anonymously, plus anonymous `GET /api/v1/tasks` still answering 401. The `crossorigin` assertion in
  `e2e/manifest.spec.ts` is rewritten in the same commit to assert the attribute is absent and that the assets load
  in a fresh browser context with no cookie.
- **SQLite WAL:** on SQLite, `serve` runs `PRAGMA wal_checkpoint(TRUNCATE)` every `BOARD_SQLITE_CHECKPOINT`
  (default `10m`; `0` off) and on clean shutdown. Test: a Go test writes enough rows to grow the WAL, runs one
  checkpoint tick (short interval) with no concurrent writer and asserts the `-wal` file is 0 bytes or absent; a
  second asserts the same after a clean shutdown.
- **README:** its Status line and its run/deploy section describe the shipped product (it says "Nothing is
  implemented yet" today). Prose only: no test, and never a blocking ground beyond "the Status line still says
  design".

### R10 Live refresh and nav for the new views

- The route table gains Timeline, Actors and Archive; the phone tab bar keeps Attention, Initiatives, Tasks, Notes
  and puts the rest under "more". Each new view declares its live-refresh predicate (Phase 2 R8).
- **Tests:** the `shell` spec extended (route table order, "more" contents, 44 px targets at phone); the Phase 2
  live-refresh Playwright pattern for Actors and Archive (a change through the API appears within 5 s).

### R11 Migration safety

- Migrations are additive: a Phase 2 database with initiatives, tasks, leases, asks, notes, cursors, keys,
  sessions and events upgrades and reads every row back.
- **Tests:** a Go or Godog test migrates a populated Phase 2 database on both legs and reads the rows back.

### R12 UI quality bars extend to the new views

- The three new views (`#/timeline`, `#/actors`, `#/archive`), the Needs attention sections, the Stranded panel,
  the attention chips and the three new dialogs (acknowledge from the task page, reopen from the archive, the
  force-done confirm) meet the Phase 0b
  bars: no horizontal page scroll with the longest accepted values, 44 px / 16 px on the phone, axe zero
  serious/critical at phone and desktop in both themes, same-origin requests only, focus trapped and returned,
  `aria-current` on the nav.
- **Tests:** the existing `overflow`, `touch`, `a11y`, `sameorigin`, `dialogs` and `shell` specs are extended with
  these views, sections, panels and dialogs as finite listed states (each named in the spec file).

### R13 Wide screens

Today nothing changes above the 1024 px sidebar breakpoint and `<main>` has no width limit, so every view stretches
across an ultra-wide screen. Exactly these rules apply (the phone, tablet and 1024–1535 px layouts stay as they are,
apart from the caps):

- **Caps at every width.** The single-table views (Initiatives, Notes, API keys, Actors, Archive) keep their
  content at most 1280 px wide, centred in the main column. Long text (a task's description, note bodies, ask
  questions and answers) is held to at most 75 characters per line. The Timeline always takes the full width of
  the main column.
- **Two columns from 1536 px** (Tailwind `2xl`) on exactly these views. The side column sits to the right, is
  sticky at the top of the viewport and scrolls on its own; the main column scrolls with the page. The pair is at
  most 2560 px wide, centred.
  - **Tasks:** main = Needs attention, filters and the task table; side (400 px) = the Now panel, always open
    (never folded).
  - **Task page:** main = header with badges, description, Links, Blocked by, Blocking, Notes and the task's
    Timeline (events); side (420 px) = the Stranded panel, Live leases, Conflicts, Expired leases and Asks.
  - **Initiative page:** main = header, Needs attention and Tasks; side (420 px) = Open asks and Notes.
  - **Attention:** its sections laid out as cards in a grid, two per row from 1536 px and three per row from
    2304 px, in the same order (row by row).
- **Tests:** a third Playwright viewport, "wide" 2560×1080, and one more at 3440×1440 for the overflow check.
  Bounding-box assertions (never pixels):
  - at wide, the Now panel is right of the task table with overlapping tops, and stays in place when the page
    scrolls by 1000 px;
  - at wide, the task page's Live leases section is right of its Timeline section, and the initiative page's Open
    asks is right of its Tasks;
  - at wide, Attention shows two sections side by side, and at 3440×1440 three;
  - at wide and at 3440×1440, the Initiatives and Archive tables are at most 1280 px wide with left and right
    margins equal within 2 px, and the Timeline frame is as wide as the main column;
  - at 1440×900, the Now panel is still above the table (today's layout).
  Specs `overflow` and `a11y` gain the wide viewport for the Tasks view, the task page, the initiative page and
  Attention (finite listed states, R12).

### R14 Done and active are exclusive

A task is never `done` (or `closed`) while it has a live lease. Today the spec allows both (§4.3 "A transition ends
nothing else: live leases stay live"; §4.4 and §6.5 "a done task is allowed"; §8.3 "Live leases on the task stay
live"); this requirement replaces those rules. Exactly these cases:

1. **Lease start on a done task** is **409** (exit 6), `task is done → board task reopen <ref> first`, and changes
   nothing, as for a closed task. Reopening stays an explicit `task reopen`.
2. **Marking a task done** while the caller holds a live lease on it ends that lease in the same transaction
   (`lease_ended`, outcome the done request's optional `outcome`, else `done`). `board task done <ref>
   ["outcome"]` passes it.
3. **Marking a task done while another actor holds a live lease** on it is **409** (exit 6) naming the holder, the
   lease and its intent (`→ wait for L<n> to end, or --force`), and changes nothing. With `force: true` every live
   lease on the task is ended in the same transaction (`lease_ended` with `ended_by` the caller, outcome `ended:
   task marked done by <handle>`). `force` is the operator's action: the UI's Done after a confirm that names the
   holders, or `board task done --force`, which agents use only when the operator says so (protocol v3, §3.4).
4. **Closing a task** ends every live lease on it the same way (`ended_by` the caller, outcome `ended: task closed
   by <handle>`).
5. **Upgrade:** on start, `serve` reopens every `done` task that holds a live lease (`task_reopened`, actor the
   live lease's holder as `lease_expired` does, principal `system:upgrade`, `data.reason` `live lease at
   upgrade`). The step is idempotent and safe with several servers starting together.

The Attention `done_not_closed` section and R1's `done` reason therefore never show an active task.

- **Tests:** Godog on both databases, one scenario per case above: lease start on a done task → 409 and no change;
  lease start on a closed task → 409 and no change; done by the holder ends its lease with the outcome given and
  with the default; done with another actor's live lease → 409 with the holder named and nothing changed; `force`
  ends every live lease with `ended_by` and marks done; close ends live leases; the first start on a seeded Phase 2
  database with a done task holding a live lease reopens it with its event, and a second start adds nothing. Godog
  CLI for `task done "outcome"`, the 409 line with its `→ next`, `--force`, and `lease start` on a done task (exit
  6 with its `→ next`). Playwright at desktop: Done on a task leased by another actor shows the confirm naming the
  holder, and confirming leaves the task done with no live lease.

### R15 The spec matches the code

- `SPEC.md` describes in present tense: §3.4 (`--force` is the operator's action, like `--for operator`), §4
  (`notifications`; the `inbox` bootstrap; §4.1 the operator row seeded by it; §4.3 and §4.4 per R14), §5.3 (the per-task derivation
  shared with the sections), a new §5.4 Notifications (triggers, payload, delivery, retries, at-least-once), §6
  (`attention` and `stranded_lease` on tasks, `attention=` filter, `attention_count` on initiatives,
  `/notifications`, `/timeline`, `/actors` in the §6.3 style), §6.4/§6.5 (R14's done/lease rules replacing "a done task is allowed" and "leaves its leases live"), §8.1 (exit
  6 for the R14 409s and for `lease ack` on a live lease), §8.3 (`task done` outcome and `--force`, `task ls`
  ordering and markers, `--attention`, `task show` lines, `lease ack` in the shipped list, `init ls` counts,
  `board version`), §8.6 (protocol v3), §11 (Needs attention sections, badges,
  Stranded panel, Timeline, Actors, Archive, the route table and "more", the wide-screen caps and columns of R13, R16's sorting and saved view state), §12.3 (PWA assets public), §13 (`BOARD_NOTIFY_*`, `BOARD_SQLITE_CHECKPOINT`, the image's
  version args), §16 (Phase 3 shipped, one line; its Timeline row names the shipped filters), §17 (webhooks removed from
  "later") and §18 question 4 (closed: acknowledgement stays manual). The README's Status line is current (R9). `api/openapi.yaml`
  declares every new operation and field. Updated in the same commit as each behaviour, never in a docs-only task.

### R16 Filters and sort persist; tables sort

Today each view keeps its filters only in the URL fragment's query (`#/tasks?status=done`, `lib/taskFilters.ts`
and its siblings), so a reload keeps them but following the nav (`#/tasks` with no query) drops them, and no table
sorts. Exactly these rules apply:

- **Sortable tables.** Every data table (Initiatives, Tasks, the initiative page's Tasks, API keys, Actors,
  Archive, and any table this phase adds) sorts by any of its columns: activating a header (click, Enter or Space)
  sorts by that column ascending, again descending, again back to the view's default order. The header is a
  `button` inside the `th`, which carries `aria-sort` (`ascending`/`descending`/`none`) and shows a direction
  mark. Comparison is per column type: refs numerically (`board-9` before `board-10`), text case-insensitively
  with `localeCompare`, times chronologically, status and kind in their lifecycle order, empty values last in
  both directions; ties keep the default order (stable). Sorting reorders the rows the view holds and never
  changes what it fetches. The Needs attention sections keep their own order. On the phone, where the header row
  is hidden, a "Sort" select (44 px target) offers the same column/direction choices.
- **Sort in the URL.** The sort is part of the view's fragment query (`sort=<column>&dir=asc|desc`, absent for
  the default order), encoded and parsed by the same `lib/` module as the view's filters.
- **Persisted per view.** Every view with filters or a sort (Tasks, Initiatives, the initiative page, Attention,
  Notes, Timeline, Actors, Archive, API keys) saves its whole fragment query to `localStorage` under one versioned
  key per view (`board.view.v1.<view>`; the initiative page shares one key for all initiatives) whenever it
  changes. Opening a view **with** a query in its fragment uses that query and saves it (a shared link wins).
  Opening it **without** one (nav, tab bar, a link with no query) restores the saved query by rewriting the
  fragment in place (`history.replaceState`, no extra history entry) before the first fetch, so only one request
  is made. **Clear filters** (and the default sort) clears the saved query too. A missing, unreadable, foreign or
  malformed saved value is ignored and the view opens at its defaults; a storage that throws (private mode, quota)
  never breaks the view. Filter values the server no longer knows (a deleted initiative, a removed tag) are
  dropped when restored. Signing out clears every `board.view.*` key.
- **Tests:** Vitest per view module for the sort ↔ query encoding and for each column type's comparator (numeric
  refs, case, empty last, ties stable, lifecycle order), and for the restore rules with a fake storage: query
  present wins and is saved; bare route restores; malformed, foreign-version and throwing storage fall back to
  defaults; clear removes the key. Playwright at phone and desktop: on Tasks set two filters and a sort, go to
  Initiatives through the nav and back through the nav, and the filters, the sort and the rendered row order are
  restored with one `GET /tasks`; the same after closing the page and opening `#/tasks` in a new page of the same
  context; a fragment with its own query overrides the saved one; Clear filters then nav away and back shows the
  defaults; sorting each table by one column of each type puts the rows in the expected order and sets
  `aria-sort`; keyboard sorting works; the phone Sort select reorders the rows. Specs `overflow` and `a11y` cover
  the sorted state of each table (R12).

## Materiality rubric

Reject only on substance:

- a requirement's behaviour is absent or wrong in the live code;
- a test the requirement names is missing, red, or asserts the opposite of the requirement;
- the spec contradicts the live code;
- a secret is exposed.

Everything else verifies, with the gap recorded as a residual note: wording, form, provenance, process, visual
taste, and **any number or claim in prose outside the spec** (commit messages, notes, issue comments, run
artifacts, the README). Prose outside the spec is never a blocking ground.

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
- **Plan shape.** Pre-split at plan time: R1 into the shared derivation (with the Attention view moved onto it),
  the task fields and filter, and `attention_count`; R2 into `attention.ts`, the Tasks/initiative sections, the
  Initiatives list, the Stranded panel; R3 one task per command group; R4 into migration and enqueue at the three
  triggers, the payload builder, the delivery worker with retries and claims, `GET /notifications`; R5 one task;
  R6 into the `lib/` mapping, the view, filters and live refresh; R7 into the API, Actors, Archive; R8, R9 (one per
  fix), R10 and R11 one task each; R13 into the caps and one task per two-column view; R14 into the server rules (cases 1-4), the startup reopen (case 5), the protocol v3 text, and the CLI and UI;
  R16 into the shared sort/persist helpers, then one task per view; R12 one task per spec file. Backend before the UI that needs it.
- **Screenshots for the operator.** Whenever a task changes how a view looks, take Playwright screenshots of that
  view at phone and desktop in both themes into `/run/ralphd/artifacts/screenshots/` (never committed) and look at
  them before calling the task done. Views R13 changes are also shot at wide (2560×1080). Send a few to the operator with the
  telegram-notify skill at each milestone (attention-first UI done, wide layout done, alerts delivering, Timeline done, Actors and Archive done, sweep green).
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
- **Judges use the CI evidence (review, verdict gate).** The gate results at a sha are its Jenkins builds: the
  push build and the sweep's extended build. A judge reads their status, consoles and archived `ci-results/`, and
  may run targeted single-package checks. A judge never re-runs `ci/run.sh`, `ci/suite.sh`, `ci/quality.sh`,
  `ci/mutation.sh`, Gremlins or Stryker in full: the extended suite takes over 2 h, cannot finish inside one
  iteration, and saturates the host. A verdict is `verified` or `rejected` from the evidence at hand. If a build the
  judge needs is still running, the judge says so as a finding rather than waiting for it.
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
  never an edit to `ci/quality.json`. `vis-timeline` failing a gate it cannot be made to pass (axe inside its own
  DOM, Trivy, coverage of its glue) is a petition naming the gate and the evidence, not a disabled rule. There is
  no bundle-size gate.
- **Live-board tracking (operator rule, bookkeeping only):**

  Operator standing rule: show this run on the live board as ONE task, `board-4`, held in progress while the run works and marked done at the end. This is bookkeeping outside the repository: it is not a PRD requirement, not a task in tasks.json, and nothing about it is committed.

  1. Set up the CLI in a shell separate from any test recipe (board.env sets BOARD_* variables the recipes must not see). It is prebuilt; do not build or use a workspace's own board binary for this:
       set -a; . /config/creds/board.env; set +a
       export BOARD_ACTOR=ralph:board-phase3 BOARD_WAY_BACK="ralphctl status board-phase3"
       B=/config/creds/board
     `$B whoami` should print `you: ralph:board-phase3 (key ralphd) ... server ok`.
     Never print board.env, BOARD_API_KEY or an Authorization header.
  2. Every worker iteration: `$B lease start board-4 "<NNN: one-line intent of the task you are on>" --ttl 3h`. It renews your live lease if you hold one and starts a fresh one if it expired. One call per iteration is enough.
  3. At each milestone the PRD names (e.g. its Telegram milestones): `$B milestone board-4 "<milestone, sha>"`.
  4. When you signal COMPLETE: `$B lease renew board-4 --ttl 3h --intent "COMPLETE at <sha>, awaiting ralphd review"` and `$B milestone board-4 "COMPLETE: <one-line outcome, sha>"`. Do NOT end the lease or mark the task done: ralphd review still follows, and the operator's session ends the lease and marks the task done when the run is verified. If review rejects and a new approach starts, carry on with step 2 (reopen first with `$B task reopen board-4` if it shows done).

  Limits:
  - Never close `board-4`, never create board tasks or initiatives, never touch any other task.
  - A board error (exit 1, unreachable) is logged in notes.md and ignored: it never blocks, fails or delays a task, and is never a task of its own.
  - The live board is a separate deployment: never use it in tests.
- **Engine defects to avoid by hand:**
  - every requirement here names its finite cases, and so must the tasks;
  - do not signal COMPLETE while tasks are pending;
  - when an approach ends in review, read the findings before replanning;
  - after any cure pass, check `tasks.json` for a task that depends on itself, and repair it by steer.

## Non-negotiables

- **No weakened test, and no gate loosened or skipped.** No `enabled: false`, no warn-only mode, no exemption
  or baseline file, no `// nolint` or `eslint-disable` without a stated reason on the line, no axe rule
  disabled, and no `skip` in a feature, spec or test. A test pinning behaviour this wave changes (the task list
  order in the UI and CLI, the task and initiative documents' shape, the nav, the Attention derivation's home,
  lease start on a done task, `task done` and `task close` leaving leases live, the protocol version, the
  manifest's `crossorigin`) is
  rewritten in the same commit and keeps every behaviour it asserted that still holds.
- **No raised timeout, budget, worker count or per-test bound.**
- **Playwright never retries**, and no visual-snapshot (pixel-diff) assertions. Time-dependent tests use an
  injected clock or the configurable intervals (`BOARD_SWEEP_INTERVAL`, `BOARD_STREAM_POLL`,
  `BOARD_STREAM_HEARTBEAT`, `BOARD_SQLITE_CHECKPOINT`, the notifier's backoff through an injected clock), never
  sleeps longer than a few seconds. Live-refresh tests wait on the DOM (at most a 5 s timeout), never on a fixed
  sleep.
- **Nothing in the UI polls on a timer.** Live data comes from the stream.
- **No secret** in a commit, a log line, an event, a notification payload, a golden file, a screenshot or a test
  fixture. `BOARD_NOTIFY_HEADER`'s value is never logged.
- **No homelab specifics in the repository** (hostnames, tailnet names, personal paths, the operator's notifier
  URL), except for the Jenkins job URL in `docs/ci.md` and the badge URLs in the README. The repository is meant to
  go public.
- **No external network request** from the UI or from tests beyond the containers and `httptest` receivers the
  suite starts. `vis-timeline` is bundled, never loaded from a CDN.
- **Migrations are additive and forward-only** (R11).

## Budget

- 800 iterations, 5 approaches, a 96 h wall clock and a 120-minute iteration cap (operator: the whole scope, with
  the time it needs).
- Peer plan review and the peer verdict gate are on.
- Vigilant and router task selection are on; reflect is off.
- Docker access is on (sibling toolchain, testcontainers). Host networking is on (Jenkins is tailnet-only).
