# Phase 0b: UI shell, design system and the phone experience

## What the operator gets

1. A modern, dense UI that renders well from a 360 px phone to a wide desktop, and is good to use one-handed
   on a phone.
2. A design system (Tailwind v4 plus design tokens, light and dark themes) and a responsive app shell
   (bottom tab bar on phones, sidebar on wider screens) that every later phase builds its views on.
3. The two Phase 0 views, **Initiatives** and **Settings → API keys**, rebuilt on that shell, with proper
   sign-in, signed-out, loading, empty and error states.

No backend behaviour changes. `SPEC.md` is the product spec; where this PRD and `SPEC.md` disagree, **this PRD
wins**, and the spec is corrected in the same commit (R12).

## Context the run needs

- Phase 0 is done: run `board-phase0` built the server, the CLI, the UI (`web/`, Svelte 5 + Vite + TS), the
  e2e suite (`e2e/`, Playwright) and the Jenkins pipeline with every gate (`ci/`, `docs/ci.md`). All of it is
  green at the commit that adds this PRD. Read `SPEC.md` §3.2, §11 and §12.3, `web/`, `e2e/` and `docs/ci.md`
  before planning.
- The UI today is three components (`web/src/App.svelte`, `Initiatives.svelte`, `ApiKeys.svelte`) with no
  styling, plus logic modules in `web/src/lib/` with unit tests. Keep logic in `lib/` modules with unit tests
  and keep components thin; the TS CRAP ceiling (8), the coverage gate and Stryker apply to everything new.
- The server already has `GET /auth/session`, `GET /auth/login` and `POST /auth/logout` (with
  `X-CSRF-Token`, §3.2). The UI has no sign-out yet.
- The UI is served by the binary from embedded assets and makes **no external network requests** (§11).
  That stays true: no web fonts, CDNs or remote icons. Icons are inline SVG (a pinned, bundled icon package is
  allowed).

Symbols and section numbers refer to `SPEC.md` and the code at the commit that adds this PRD. Drift is expected
and is not a finding: if a reference has drifted, find the current site and carry on.

## Issues in scope

None filed. This PRD is the whole statement.

**Not in scope:** any new API endpoint, schema, migration or server behaviour; the Attention, Timeline, Task,
Notes, Actors and Archive views (later phases; the shell only has to make adding them a matter of adding a
route and a nav entry); the CLI; the homelab deployment.

## Design direction

Guidance for the worker, not a review ground (taste is never a blocking finding):

- **Linear-style and dense.** Neutral grey scale (zinc/slate family), one accent colour, 1 px hairline
  borders, small radii (6–8 px), no heavy shadows, no gradients. An 8 px spacing grid.
- **Type.** System font stack (`ui-sans-serif, system-ui, …`) and `ui-monospace` for slugs, key prefixes and
  keys. About 13–14 px body text on desktop, 15–16 px on phones; tabular numerals for dates and counts.
- **Dark first**, with an equally finished light theme. Status shown as small coloured badges, tags as
  chips.
- **Phone.** Thumb-reachable primary actions (bottom tab bar, a floating or bottom-anchored primary
  button), sheets that slide up from the bottom for forms and confirmations, cards instead of tables, no
  hover-only affordances, respect for the notch and home indicator.
- Subtle transitions (150 ms or so), all disabled under `prefers-reduced-motion: reduce`.

## Requirements

Each requirement is a behaviour plus the tests that fail without it. "Phone", "tablet" and "desktop" mean
the Playwright viewports **390×844**, **820×1180** and **1440×900**. "Each view" means the finite set:
**Initiatives** (`#/`), **Settings → API keys** (`#/settings/keys`) and **Signed out** (`#/signed-out`).

### R1 Tailwind v4 and design tokens

- Tailwind CSS v4 through `@tailwindcss/vite`, versions pinned exactly in `web/package.json` and the lock
  file. One stylesheet defines the tokens as CSS custom properties (colours for surface, raised surface,
  border, text, muted text, accent, danger, success, warning; radius; spacing scale; font stacks), with one
  set per theme, and Tailwind's theme maps to them. Components use the tokens or Tailwind utilities, never
  hard-coded colour values.
- The build stays fully self-contained: `npm run build` output references only relative, same-origin URLs.
- **Tests:** a Playwright spec visits each view at phone and desktop size and records every request the page
  makes; every request is same-origin. A unit or build test fails if the built CSS has no token custom
  properties for both themes.

### R2 Themes

- Three settings: **System** (default, follows `prefers-color-scheme`), **Light** and **Dark**, chosen from a
  control in the shell and persisted in `localStorage`. The theme is applied before first paint (no flash of
  the wrong theme), on `<html>` as a `data-theme` attribute holding the effective theme (`light` or `dark`),
  and a `<meta name="theme-color">` matches the effective theme's surface colour.
- **Tests:** unit tests for the theme module (setting → effective theme for each of the 3 settings × 2 system
  preferences; persistence; an unknown stored value falls back to System). Playwright: with the browser
  emulating `dark`, System gives `data-theme="dark"`; choosing Light gives `light`, survives a reload, and
  the page background's computed colour differs between the two themes.

### R3 Responsive app shell

- **Phone (below 768 px):** a compact top bar (product name, current view title, account menu) and a fixed
  **bottom tab bar** with one entry per view (Initiatives, Settings), each an icon plus a label. Content is
  padded so the bar never covers it, and the bar respects `env(safe-area-inset-bottom)`
  (`viewport-fit=cover` in the viewport meta).
- **Tablet (768–1023 px):** a collapsed left **icon rail**.
- **Desktop (1024 px and up):** a left **sidebar** with labels, the account (email, sign-out) at its foot.
- The current view's nav entry has `aria-current="page"`. A "Skip to content" link is the first focusable
  element. Adding a view is one route-table entry plus one nav entry (one data structure drives the router
  and both navs).
- **Tests:** Playwright at each of the three viewports on each signed-in view: the expected navigation
  (bottom tab bar / icon rail / sidebar) is visible and the other two are not; navigating by the nav works
  and moves `aria-current`; `document.documentElement.scrollWidth <= clientWidth` (no horizontal scroll);
  on the phone, the last item of a long initiatives list is not covered by the tab bar when scrolled to the
  bottom. A unit test proves the route table drives both the router and the nav.

### R4 Touch and phone ergonomics

- On the phone viewport every interactive element in the shell, forms, lists and dialogs has a hit area of
  at least 44×44 CSS px, and every text input, select and textarea has a computed `font-size` of at least
  16 px (prevents iOS zoom on focus). Nothing depends on hover.
- **Tests:** a Playwright spec at the phone viewport, on each view and with each dialog/sheet open (the
  finite set: new-initiative sheet, new-key sheet, revoke confirmation, account menu), measures every
  visible `a, button, input, select, textarea, [role=button]` and asserts both rules.

### R5 Session states: signing in, signed out, errors

- **Signing in:** a centred card with the product name and a spinner while the session loads or the login
  redirect starts (replaces the bare "Signing in…").
- **Sign out:** the account menu (phone/tablet) and the sidebar foot (desktop) show the signed-in email and a
  **Sign out** button that posts `POST /auth/logout` with the session's `X-CSRF-Token` and then shows the
  **Signed out** view (`#/signed-out`): a card with a **Sign in** button that goes to `/auth/login`. The
  Signed out view never starts a login redirect by itself.
- **Session error:** a card with the problem detail and a **Retry** button that reloads the session.
- **Tests:** unit tests for the session-state transitions (loading → signed in, → redirecting, → error,
  → signed out; retry). Playwright through the existing mock provider: sign in, sign out, the Signed out
  view shows and does not redirect, an API call with the old cookie gets 401, **Sign in** signs in again.

### R6 Initiatives view

- **Desktop/tablet:** a dense table (title with slug beneath in monospace, status badge, tag chips, created
  as relative time in a `<time datetime>` element whose `title` shows the absolute local time). A **New
  initiative** button opens the create form in a side panel or dialog.
- **Phone:** the same data as a card list (one card per initiative), and the create form in a bottom sheet
  opened by a bottom-anchored primary button.
- Loading shows skeleton rows; an empty list shows an empty state with a **New initiative** call to action;
  a refusal shows the problem detail inline in the form and keeps what was typed (as today); success closes
  the form, reloads the list and shows a toast in an `aria-live="polite"` region.
- The `data-testid`s used today (`initiative-row`, `initiative-status`, `initiative-tags`, `email`) keep
  their meaning on every viewport.
- **Tests:** unit tests for the relative-time formatter (the finite cases: just now, minutes, hours,
  yesterday, days, and older than 7 days → a date) and for tag/status presentation logic. Playwright at
  phone and desktop: create an initiative through the sheet/panel, it appears in the list with its status
  and tags; a taken slug shows the detail and keeps the input; the empty state's call to action opens the
  form. `e2e/initiatives.spec.ts` is rewritten for the new UI with the same behaviours it covers today.

### R7 Settings → API keys view

- Same treatment: table on desktop/tablet, cards on phone, the create form in a panel / bottom sheet, status
  badges (`active`, `revoked`, `expired`), relative times as in R6.
- The once-shown key appears in a highlighted box in monospace that wraps without horizontal overflow on the
  phone, with a **Copy** button (Clipboard API, a "Copied" confirmation) and **Done**. Its once-only rules
  from §11 are unchanged.
- **Revoke** asks for confirmation in a dialog (bottom sheet on the phone) naming the key; cancelling
  revokes nothing.
- **Tests:** Playwright at phone and desktop: create a key, the box shows it, Copy puts it on the clipboard
  (grant `clipboard-read`/`clipboard-write` to the context), Done drops it; revoke → cancel leaves it active;
  revoke → confirm shows `revoked`, and the revoked key gets 401. `e2e/keys.spec.ts` is rewritten for the new
  UI with the same behaviours it covers today.

### R8 Dialogs, sheets and keyboard

- One dialog/sheet component used everywhere: focus moves into it on open, is trapped while open and returns
  to the opener on close; **Escape** and the backdrop close it; it has `role="dialog"`, `aria-modal` and a
  label. Every interactive element has a visible focus ring (`:focus-visible`).
- **Tests:** unit tests of the focus-trap logic; Playwright: open each dialog/sheet in the R4 set by keyboard
  only, Tab cycles within it, Escape closes it and focus is back on the opener.

### R9 Accessibility

- **Tests:** `@axe-core/playwright` (pinned) scans each view, plus each dialog/sheet in the R4 set, at phone
  and desktop, in both themes: zero violations of impact `serious` or `critical` (this includes colour
  contrast). No rule is disabled.

### R10 Installable on a phone

- An embedded web app manifest (`name`, `short_name`, `start_url` relative, `display: standalone`,
  `theme_color`, `background_color`, an SVG icon and a 192 px and 512 px PNG icon generated at build time or
  committed), linked from `index.html`, plus an `apple-touch-icon` and an SVG favicon. No service worker.
- **Tests:** Playwright: the manifest link resolves same-origin with a JSON body that has those fields, and
  every icon it lists returns 200 with an image content type.

### R11 Existing behaviour keeps working

- Every Phase 0 behaviour of the UI (login lands on Initiatives, the CSRF header on writes, paging through
  `next_after`, the key-shown-once rules) keeps working and keeps its tests. Where a spec is rewritten for the
  new markup, it keeps every behaviour it asserted before.
- **Tests:** the full e2e, Vitest, Go and feature tiers are green in the sweep (Standing rules).

### R12 The spec matches the code

- `SPEC.md` §11 and §12.3 describe, in present tense, the design system (Tailwind v4, tokens, themes), the
  breakpoints and shell, the session states and Signed out view, the dialog behaviour, the accessibility bar
  (R9) and the manifest. Updated in the same commit as each behaviour, never in a docs-only task.

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
- **Screenshots for the operator.** Whenever a task changes how a view looks, take Playwright screenshots of
  that view at the three viewports in both themes into `/run/ralphd/artifacts/screenshots/` (never
  committed) and look at them before calling the task done. Send a few to the operator with the
  telegram-notify skill at each milestone (shell done, Initiatives done, API keys done, sweep green).
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
  disabled, and no `skip` in a feature, spec or test. A test pinning markup this wave changes is rewritten in
  the same commit and keeps every behaviour it asserted.
- **No raised timeout, budget, worker count or per-test bound.**
- **Playwright never retries**, and no visual-snapshot (pixel-diff) assertions: they are environment-flaky.
  Layout is asserted through geometry and computed styles.
- **No secret** in a commit, a log line, an event, a golden file, a screenshot or a test fixture.
- **No homelab specifics in the repository** (hostnames, tailnet names, personal paths), except for the
  Jenkins job URL in `docs/ci.md` and the badge URLs in the README. The repository is meant to go public.
- **No external network request** from the UI or from tests beyond the containers the suite starts.
- **No backend change** beyond what serving new static files (the manifest and icons) strictly needs.

## Budget

- 250 iterations, 2 approaches, a 24 h wall clock and a 120-minute iteration cap.
- Peer plan review and the peer verdict gate are on.
- Vigilant, reflect and router task selection are on.
- Docker access is on (sibling toolchain, testcontainers). Host networking is on (Jenkins is tailnet-only).
