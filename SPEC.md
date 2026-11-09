# board — product spec

A shared work board for one operator and the coding agents working for them.
Initiatives, tasks, live leases, asks and notes, in one service with a web UI for
the human, and an HTTP API plus a CLI for agents.

This document is the design. Nothing here is implemented yet. Requirement IDs
(`B1`, `B2`, …) are stable: code, tests and PRDs cite them.

Contents

1. Why
2. Glossary
3. Authentication and rules of use
4. Data model
5. Derived state: leases, conflicts, attention
6. HTTP API
7. Live feed and long-poll
8. CLI
9. Agent identity (template)
10. Agent skill (outline)
11. Web UI
12. Stack
13. Configuration and deployment
14. Performance targets
15. Testing, CI and quality gates
16. Delivery phases
17. Out of scope / later
18. Open questions

---

## 1. Why

One person runs many agent sessions at once: interactive Claude Code / pi / Codex
sessions, autonomous runs, sessions on several machines. Each one knows only its own
context. Nobody (human or agent) can see at a glance:

- which longer efforts are in flight, and how they progress over time;
- which agent is working on what **right now**, and which ones died without saying so;
- what is waiting for the operator (a question, a finished task to accept, an overdue check);
- whether two agents are about to touch the same thing (a host, a repo, a service);
- what other agents wanted everyone to know.

`board` is that shared surface. It is **tool-agnostic**: it has no integration with any
session manager or job runner. Agents take part because a skill tells them to, and identify
themselves from what their environment exposes (§9).

### Hard requirements

| ID | Requirement |
|---|---|
| **B1** | Human UI and agent API see the same data. Every UI action has an API call. |
| **B2** | Live work is a **lease** with a TTL. Work that stops renewing **expires** and is shown as such, without anyone cleaning up. |
| **B3** | The operator's **attention view** answers "what needs me now" in one screen (§5.3). |
| **B4** | Agents can read "what's new since T" cheaply: summaries first, bodies on demand. |
| **B5** | Conflicts are **warnings**, never locks. The board never blocks an action. |
| **B6** | Generic SQL: SQLite and Postgres supported from v1, same behaviour, same test suite (§12.2). |
| **B7** | Small and fast: one static binary, one container, tens of MB of RAM (§14). |
| **B8** | The API is self-describing: OpenAPI document and Swagger UI served by the app (§6.1). |
| **B9** | No integration code for any specific agent tool. Tool specifics live in the operator's agent instructions (§9), never in the server. |

### Non-goals (v1)

- Roles, permissions, teams (§3). One operator, any number of agents; every authenticated principal can do everything.
- A task queue or automatic pickup (§17).
- Replacing an external issue tracker. Tasks may link to external tickets; they do not mirror them.
- Rich text editing, attachments, comments threads. Notes are text (Markdown rendered).

---

## 2. Glossary

These names are fixed. Code, API, UI and skill text use exactly these words.

| Term | Meaning |
|---|---|
| **Board** | The service, its CLI (`board`) and its UI. |
| **Initiative** | A group of tasks: a bounded effort ("onboard the new phone") or an ongoing area ("vulnerability hygiene"). Flat, never nested. Carries free-form **tags** (`work`, `personal`, `homelab`, …) used for filtering. |
| **Task** | A unit of work inside exactly one initiative. Status is **open → done → closed** (§4.3). May depend on other tasks ("blocked by"). Kind is `work` (default) or `watch`. |
| **Watch task** | A task of kind `watch`: an ongoing duty with a check-in **cadence** and a `next_check_at`. Recording a check moves `next_check_at` forward. Overdue checks appear in the attention view. |
| **Done** | An actor says the task's work is finished. The task stays visible and appears in the attention view until closed. |
| **Closed** | The task is accepted and finished. Its timeline bar ends, and it moves to the **archive**. Carries a **resolution**: `completed`, `dropped`, `superseded`, `duplicate`. |
| **Archive** | Closed tasks (and closed initiatives). Hidden by default; the UI and API filter "closed within the last N days". |
| **Lease** | An actor's live, time-bounded claim that it is working on a task. Started, **renewed**, **ended** by the actor, or **expired** by the server when its TTL passes without renewal. Default TTL 2 h, maximum 12 h. Declares the **resources** it touches. |
| **Resource** | A typed string naming something a lease touches: `repo:<name>`, `host:<name>`, `service:<name>`, `run:<id>`, `path:<…>`, or any `<type>:<name>`. Used only for conflict detection and note addressing. |
| **Conflict** | Two or more live leases declaring the same resource. A warning returned to the actor starting/renewing the lease and shown to everyone. Never a block. |
| **Actor** | Whoever acts on the board. `kind` is `operator` (the human) or `agent`. Identified by a **handle** (e.g. `deck:work/fix-ci`, `ralph:run-42`, `claude:laptop:3f2a`) and carries a **way back**: how the operator reaches that actor (a command, a URL, a session name). |
| **Event** | An append-only entry on a task's timeline (§4.6): lease started/renewed/ended/expired, ask raised/answered/withdrawn, check recorded, done, reopened, closed, note, field changes. Some types are drawn as markers on the timeline. |
| **Ask** | A question from an actor that needs the operator. Open until **answered** (or **withdrawn**). The asking actor picks a **mode**: `stop` (it ends its turn; the operator comes back later) or `poll` (it long-polls for the answer with a timeout, §7.2). The answer can be given in the UI, or by the operator replying to the waiting agent directly, in which case the agent records the answer via the API. May be marked `blocking` (the task cannot proceed without it). |
| **Note** | A message from an actor: a one-line **summary** and an optional longer **body** (Markdown). Optionally bound to a **task**, an **initiative** and/or a **resource**. A note with no task and no initiative is a **broadcast**. Readers list summaries since a timestamp and fetch bodies when relevant. |
| **Attention view** | The operator's default view (§5.3): open asks, done-not-closed tasks, expired leases, overdue watches, live conflicts, each with the actors involved. |

---

## 3. Authentication and rules of use

**B10.** Every request except `GET /healthz` is authenticated, by a browser session (humans)
or an API key (agents). v1 has **no roles or ACL**: any authenticated principal can do
everything the API offers, including closing tasks and answering asks. Access control is a
later addition (§17).

### 3.1 Principal and actor

Two separate things are recorded on every write:

- **Principal**: who authenticated. `oidc:<email>` for a browser session, `key:<key name>` for
  an API key.
- **Actor**: who acts on the board (§2). A session principal always acts as the **operator**
  actor. A key principal acts as the actor named in the `Board-Actor` header (the handle an
  agent derives, §9); the header is required on key-authenticated requests.

Events (§4.6) store both `principal` and `actor_id`. Revoking a key cuts off every actor that
used it.

### 3.2 Humans: OIDC

Generic OpenID Connect; the app is provider-neutral (any issuer with discovery).

- Configuration (§13): issuer URL, client ID, client secret, **allowed emails** list, public base
  URL. Discovery via `<issuer>/.well-known/openid-configuration` (fetched on first use and cached, so
  the board starts while the issuer is down); ID token signature (RS256) checked against the
  issuer's JWKS (cached, refreshed on an unknown `kid`, at most once per 10 s). The ID token's
  issuer, audience (the client ID), expiry and nonce are checked too.
- Flow: authorization code with **PKCE (S256)**, plus `state` and `nonce`, both single-use,
  bound to a short-lived (10 min) HttpOnly pre-login cookie `board_login` (path `/auth`). The
  server keeps the login's `state`, `nonce` and PKCE verifier in memory under the cookie's random
  ID and spends them on the first callback, whatever its outcome. Scopes `openid email profile`.
  The client authenticates to the token endpoint with `client_secret_basic`.
  1. `GET /auth/login` redirects the browser to the provider's authorization endpoint and sets the
     pre-login cookie.
  2. The provider redirects back to **`/auth/oidc/callback`** with
     `code` and `state`.
  3. The callback answers **400** when no login is in progress (the pre-login cookie is missing,
     used or expired, so there is no PKCE verifier), when `state` differs from the login's, when it
     carries no code, or when the ID token carries another login's nonce (a reused nonce); **401**
     when the ID token is not valid (bad or tampered signature, wrong issuer, audience or expiry);
     **403** when the provider reports an error or admission fails (below); **502** when the provider
     cannot be reached or refuses the code. Every one of these creates no session.
  4. On success it starts the session and redirects to `/`. The pre-login cookie is left to expire:
     its login is already spent server-side, so it opens nothing.
- Session endpoints: `GET /auth/session` describes the caller's session (`email`,
  `principal` = `oidc:<email>`, `actor` = the operator handle, `csrf_token`), and
  `POST /auth/logout` deletes the session row and clears the cookie (204). Both need a session
  and answer 401 to anything else, a key included.
- Admission: `email_verified` must be true and the email must be in the allowed list
  (case-insensitive, exact). Anything else gets 403 and no session. An admitted email acts as
  the **operator** actor (§3.1).
- Session: random 256-bit ID in an **HttpOnly, Secure, SameSite=Lax** cookie `board_session`, path
  `/`, `Max-Age` = the absolute lifetime; stored server-side as SHA-256 hash (§4.9), so logout and
  expiry are real. The cookie value is never stored. Two expiries end a session, both enforced
  on the server whatever the cookie says, and both delete its row (on the next request presenting
  it that is not refused, or by the sweeper, see below):
  - **absolute**: `BOARD_SESSION_TTL` (default `168h`, 7 days) after login; activity does not extend it;
  - **idle**: `BOARD_SESSION_IDLE` (default `24h`) since the session was last used (a request
    refreshes its last-seen time, written at most once per tenth of the idle timeout).
  - Like a key's `last_used_at` bump (§3.3), both session writes a lookup makes (the last-seen
    time, the deletion of a session found dead) commit only with a request that succeeds (a
    response status below 400, written just before that status goes out). A refused request (400
    and up: a failed CSRF or Origin check, an invalid, unknown or conflicting write, or a 401 for
    a dead session) leaves `sessions` as it was, and the session's next successful request still
    makes the write. A navigation redirected to `/auth/login` is not refused, so it removes a
    dead session's row.
  After either, and after logout, the old cookie is as good as none: an API call gets 401, a
  navigation is redirected to `/auth/login`. The expiry scenarios (`features/oidc_session.feature`)
  start the server with lifetimes of a few seconds, so no step sleeps longer than 5 s.
- **CSRF**: every cookie-authenticated request whose method is not GET, HEAD or OPTIONS (so
  `POST /auth/logout` and every write) must carry `X-CSRF-Token` equal to the session's CSRF token
  (delivered to the UI by `GET /auth/session`) **and** an `Origin` header whose scheme, host and port equal those of `BOARD_PUBLIC_URL`; a
  `Referer` never stands in for a missing `Origin`. A missing or wrong token, a missing or foreign
  `Origin` (including `null`) gets **403** with a problem document, and
  the request does not run. Bearer-authenticated requests are exempt (no ambient credential).
- **API call or browser navigation**: a browser navigation is a `GET` whose `Accept` header
  contains `text/html`; every other request is an API call. A request with no credential (no
  Bearer key, no live session cookie) is answered by that rule when login is on: an API call gets
  401 with a problem document, including `/auth/login` and `/auth/oidc/callback`; a navigation to
  `/auth/login` or `/auth/oidc/callback` is served; a navigation to anything else is redirected
  (302) to `/auth/login`, which redirects to the provider. Only `GET /healthz` is answered with no
  credential at all. When login is off (`BOARD_OIDC_ISSUER` unset) every credential-less request
  gets 401, and the `/auth/*` operations answer 404 to an authenticated caller.
- **The mock provider** (`internal/oidc/oidctest`) is a test-only OIDC issuer the test harness
  starts in-process. It serves discovery, a JWKS, an authorization endpoint that signs in whoever
  the test names (no login page) and refuses a request that lacks PKCE S256, a state or a nonce, and a token
  endpoint that checks the client secret, the single-use code and that the `code_verifier` hashes to
  the challenge. Its RSA key is generated when it starts and nothing is committed. Knobs let a
  test sign in an unverified email, reuse an earlier login's nonce, or rewrite a signed token's
  email. `cmd/board` never imports it, so the release image does not hold it
  (`ci/repocheck` checks `go list -deps ./cmd/board`). CI contacts no external identity provider.
  `cmd/fake-oidc` runs it as a process for the Playwright specs (`e2e/serve.sh` starts it beside the
  real binary with a client secret made up for the run and passed through the environment, and
  points `BOARD_OIDC_ISSUER` at it); it is a test fixture like `cmd/fake-*`, and `cmd/board` never
  imports it either.
- **The UI's login**: the UI has no login form of its own. A navigation without a session never
  reaches the app: it is redirected to `/auth/login`, which starts the flow above and ends on `/`
  with a session. At load the app calls `GET /auth/session` (`web/src/lib/session.ts`): 200 shows
  the signed-in page (the Attention view) with the session's email, 401 (the session ended while
  the page was open) sends the page to `/auth/login` under the app's mount prefix, and any other
  failure is shown as an error without a redirect, so a down server cannot cause a login loop.
  `e2e/login.spec.ts` logs in through the mock provider and lands on the Attention view.

### 3.3 Agents: API keys

- Sent as `Authorization: Bearer <key>`.
- Format: `brd_` + 32 random bytes from the OS CSPRNG, base32 (Crockford, lowercase, no padding:
  52 characters), so keys are recognisable to secret scanners. The key is shown **once** at
  creation and never stored: the DB keeps only its SHA-256 hash (lowercase hex of the whole key
  text), a display prefix (the 8 characters after `brd_`, stored without `brd_`), name,
  `created_at`, `created_by`, `last_used_at` (coalesced, §14), `expires_at` (optional),
  `revoked_at`.
- Lookup is by hash (indexed, unique); comparison cost does not depend on the key.
- **One key per environment** (e.g. `workstation`, `ralphd`, `laptop`), not per agent, so a
  leaked environment is revoked alone. Names are unique.
- A revoked or expired key gets 401 (`key revoked` / `key expired` in the problem detail); a key
  that is not shaped like a key or that no row holds gets 401 (`key unknown`).
- The server checks a presented key against `api_keys` on every request (no cache, so a revocation
  takes effect at once) and bumps `last_used_at` at most once per key per 30 s. The bump is the only write key authentication makes, and it commits only with a request that succeeds (a response status below 400, written just before that status goes out): a refused request (400 and up, whatever the reason) leaves `api_keys` as it was, and does not use up the 30 s window, so the key's next successful request still makes the bump.
- The checks run in this order: a request with no `Authorization: Bearer` credential gets 401 and the
  store is not touched; a key the store refuses gets 401; a key the store cannot check, because it is
  unreachable or not migrated, gets 503 and never a 2xx; a valid key without a `Board-Actor` header
  gets 400.
- Management:
  - **UI** (after SSO): Settings → API keys: create (name, optional expiry), list, revoke.
  - **Bootstrap, no HTTP**: `board admin keys create <name> [--expires 90d]`, `board admin keys
    list`, `board admin keys revoke <name>`, run inside the container against `BOARD_DB_URL`
    directly. This is how the first key is made, and the recovery path if SSO is broken. The
    commands are described in §8.
  - API (any authenticated principal, v1 has no ACL; all three are in `api/openapi.yaml`):
    - `GET /api/v1/keys` answers 200 `{"items": [Key, ...]}`, every key oldest first (not paginated: keys are few).
    - `POST /api/v1/keys` takes `{"name": "...", "expires_in": "90d"}`. `name` follows the CLI's rule and
      `expires_in` is optional, in the CLI's `--expires` form (a positive count of days `90d` or hours `12h`, at most
      36500 days in either unit, so up to `36500d` or `876000h`). It answers 201 with the Key plus `key`, the `brd_` key, which is returned by this response only and
      never again. A bad name or expiry, or an unreadable body, is 400. A name already taken is 409.
    - `POST /api/v1/keys/{name}/revoke` sets `revoked_at` and answers 200 with the Key. An unknown name is 404. Revoking
      a revoked key changes nothing and answers 200 with the first `revoked_at`.
    - A **Key** is `{id, name, display_prefix, status, created_at, created_by, last_used_at?, expires_at?, revoked_at?}`:
      `status` is `active`, `expired` or `revoked`, timestamps are RFC 3339, an absent date is omitted, `created_by`
      is the creating principal (`key:<name>`, `oidc:<email>`, or `admin-cli` for the command line). A Key never holds the
      key: not in `GET /api/v1/keys`, not in `board admin keys list`, not in the `api_keys` row.
    - A creation and a revocation that happens each append an event in the same transaction as the row, `key_created` and
      `key_revoked` (§4.6), with `ref_id` the key's id, `principal` the caller's principal (`key:<name>` or
      `oidc:<email>`) and `actor_id` the actor it acted as (the `Board-Actor` handle, or the operator for a session), in
      separate columns. Their `data` holds the key's `name` (and `expires_at` for a creation), never the key. The actor
      is upserted on first use. `board admin keys` has no actor and records no event.
- **Swagger UI** declares a `bearerAuth` security scheme, so "Authorize" accepts a key; inside a
  logged-in browser session it also works with the session cookie and CSRF header.

### 3.4 Rules of use

Conventions are carried by the agent protocol (`board help agent`, §8.6), not enforced by the server:

- Agents mark tasks **done**; they **close** a task only when the operator explicitly asked
  them to. A close event records the acting actor and an optional `on_behalf_of` actor
  (`operator` when the agent closes on instruction).
- Agents answer an ask only to record an answer the operator gave them directly
  (`answered_by` = the agent, `on_behalf_of` = operator, `channel = "relay"`).
- Agents never end or expire another actor's lease. The operator may end any lease from the UI
  (recorded as `lease_ended` with `ended_by`).

The server records **who** did everything (§4.6), so misuse is visible after the fact.

---

## 4. Data model

All timestamps are UTC, stored with millisecond precision as a `BIGINT` of milliseconds since the Unix epoch
in every dialect (`store.EncodeTime` / `store.DecodeTime`). A stored instant is compared with the clock by the strict rule *stored < clock* (a lease is expired, a watch task overdue, only when its instant is strictly earlier than the clock), and the clock keeps its sub-millisecond part: the SQL predicates bind `store.EncodeBound(clock)` (the clock rounded up to a whole millisecond), so they answer exactly as the derived fields do. Equality never expires or makes overdue; dates (`target_start`, `target_end`) are
`YYYY-MM-DD` text; JSON columns are text. IDs are ULIDs (sortable by creation time, text in every dialect). Within one server process a ULID always sorts after the one made before it, even in the same millisecond, so ids order objects by creation.
Every table except the join tables and `sessions` also has an integer identity column `n` (unique, never
reused: SQLite `AUTOINCREMENT`, Postgres `GENERATED ALWAYS AS IDENTITY`) from which the **short ref** is
formed (§8.2):
initiative `slug`, task `<initiative-slug>-<n>` (per-initiative counter `seq`, e.g.
`phone-onboard-3`), lease `L<n>`, ask `A<n>`, note `N<n>`, event `E<n>`.

### 4.1 `actors`

| column | type | notes |
|---|---|---|
| id | ulid | |
| handle | text, unique | stable identity string (§9) |
| kind | `operator` \| `agent` | at most one `operator` row exists; the full product seeds it at start-up, Phase 0 creates it on the first write made by a session (below) |
| display_name | text | optional; never set in Phase 0 |
| host | text | optional, from the handle; never set in Phase 0 |
| way_back | text | optional: command/URL to reach this actor; set from `Board-Actor-Way-Back` (§6.1) |
| meta | json text | free-form environment facts the agent chose to send; a JSON object set from `Board-Actor-Meta` (§6.1), `{}` until one is sent |
| first_seen_at, last_seen_at | ts | the full product bumps `last_seen_at` on any API call with this handle (coalesced, §14); Phase 0 bumps it on every write that records the actor, uncoalesced, and a read does not |
| protocol_seen | int, nullable | highest agent protocol version this actor has read (`board help agent`, §8.6) |

An actor row is created on first use of a handle (upsert). In Phase 0 a use is a **write**: an operation that
appends an event (`POST /initiatives`, `POST /keys`, `POST /keys/{name}/revoke`) and `PUT /actors/me/protocol-seen`.
A read (`GET /meta`, the lists and gets) authenticates and echoes the identity but never touches `actors`, so a handle
that has only read has no row, and the operator row does not exist until the first write made by a session (its
actor is `BOARD_OPERATOR_HANDLE`, kind `operator`). A handle's kind follows the strongest write seen for it: a session write (acting as `BOARD_OPERATOR_HANDLE`) makes
that handle's row `operator`, promoting a row an earlier key write created as `agent` under the same handle, and an
`operator` row is never demoted, so a key write that names the operator's handle after a session write leaves the
kind `operator` (the event's `principal` still records `key:<name>` or `oidc:<email>`, so the two stay
distinguishable). Which kind a row has therefore never depends on the order the key and the session arrive in.
A write that carries a non-empty `Board-Actor-Way-Back` or
`Board-Actor-Meta` replaces that column; a write that carries neither leaves the stored values. Actors are never deleted. A unique partial index
on `kind WHERE kind = 'operator'` allows at most one `operator` row.

### 4.2 `initiatives`, `initiative_tags`

| column | type | notes |
|---|---|---|
| id | ulid | |
| slug | text, unique | `[a-z0-9-]{2,40}` (`domain.ValidateSlug`) |
| title | text | |
| description | text | Markdown |
| status | `open` \| `closed` | closing an initiative requires all its tasks closed (or `force=true`, which closes them `dropped`); `POST /initiatives/{slug}/close` and `/reopen` move it (§6.3). A closed initiative keeps `closed_at`/`closed_by` and takes no new task (§4.3); reopening clears them and leaves its tasks closed |
| target_start, target_end | date, nullable | planning window for the timeline |
| created_by, created_at, closed_at, closed_by | | |
| next_task_seq | int | the `seq` the next task of this initiative takes, starting at 1; only counts up, so a `seq` is never reused (§4.3) |

`initiative_tags(initiative_id, tag)`, PK both columns, index on `tag`.

### 4.3 `tasks`

| column | type | notes |
|---|---|---|
| id | ulid | |
| initiative_id | fk | |
| seq | int | per-initiative counter → `ref`; unique per initiative (`UNIQUE (initiative_id, seq)`), starts at 1 and is taken from `initiatives.next_task_seq` in the creating transaction, so it is never reused |
| title | text | |
| description | text | Markdown |
| kind | `work` \| `watch` | |
| status | `open` \| `done` \| `closed` | |
| resolution | `completed` \| `dropped` \| `superseded` \| `duplicate`, nullable | set on close |
| planned_start, planned_end | ts, nullable | planned bar on the timeline; the end is not before the start |
| cadence_seconds | int, nullable | watch tasks only: 60 to 31536000, required on a watch task |
| next_check_at | ts, nullable | watch tasks only |
| external_refs | json text | list of `{label, url}` (tickets, PRs, builds): at most 20, `label` 1-80 characters, `url` http or https; `[]` when none |
| created_by, created_at, updated_at, done_at, done_by, closed_at, closed_by | | |

Transitions (`internal/tasks`: `Done`, `Reopen`, `Close`), each in one transaction with its event:

| from → to | event | row change |
|---|---|---|
| `open → done` | `task_done` | sets `done_at`, `done_by` |
| `done → open`, `closed → open` | `task_reopened` | clears `resolution`, `done_at`, `done_by`, `closed_at`, `closed_by` |
| `open → closed`, `done → closed` | `task_closed`, `data.resolution` | sets `resolution`, `closed_at`, `closed_by` (a done task keeps `done_at`, `done_by`) |

Every transition sets `updated_at` and writes `data.ref` and `data.from` (the status it left). The resolution is
one of `completed`, `dropped`, `superseded`, `duplicate` (`domain.ValidateResolution`): a missing or unknown one is
`ErrInvalid` and nothing is read or written. Every other transition (done on done, done on closed, reopen on open,
close on closed) is `ErrConflict`, a `ConflictError` whose text names the task and its current status
("cannot close task aa-1: it is closed"), and writes no row, event or actor. Reopening a task of a closed
initiative is `ErrInitiativeClosed` and writes nothing. A transition ends nothing else: live leases stay live.

Derived, not stored: **active** (has a live lease), **blocked** (has an open dependency or an
open blocking ask), **overdue** (watch with `next_check_at < now`). `Task.Blocked` (`internal/tasks`) is
computed in the same query that reads the task, and the list filter `blocked` uses the same condition. The rule: a task is
blocked while (a) some task it is blocked by is not `closed`, or (b) it has an `open` ask whose `blocking` is true (§4.5).
Answering or withdrawing that ask ends (b); an ask without `blocking`, or one that is no longer open, never blocks. `Task.Overdue` is **overdue** (§4.6).

`task_deps(task_id, blocked_by_task_id)`: PK both, no self-reference, cycles rejected on insert.
A dependency is **open** while the blocking task is not `closed` (done-not-closed still blocks).
`internal/tasks` `AddDep(ref, blockedByRef)` and `RemoveDep(ref, blockedByRef)` take a ref or a ULID for each task and
write in one transaction with their event on the dependent task, `dep_added` or `dep_removed`, whose `data` is
`{ref, blocked_by}` (the blocker's ref). The two tasks may belong to different initiatives, and the dependent and the
blocker may be in any status. A task depending on itself is `ErrInvalid`. A dependency that would close a cycle of any
length is a `CycleError` (`errors.Is(err, ErrConflict)`) whose text names the path, each task blocked by the next
(`aa-3 is blocked by aa-1 is blocked by aa-2 is blocked by aa-3`); the search is `domain.DependencyCycle`, shortest
path first. Adding a dependency that exists succeeds and writes no row, event or actor. Removing one that does not
exist, or naming an unknown task, is `ErrNotFound`. Both return the dependent task.
Both tables exist (migration `00003_tasks`, in both dialects) with the §4.10 indexes; `task_deps` also has an index on
`blocked_by_task_id` for the `blocking` list, and the database refuses a self-reference (cycle checks are the service's).
`events.task_id` has no foreign key, so the event log outlives any task row.

A task's ref is parsed by `domain.ParseTaskRef`: `<slug>-<seq>` (the text after the last `-` is a positive integer, the
rest a valid slug) or a ULID, either in upper or lower case. A write through `internal/tasks` (`Create`, `Patch`) appends
its event (`task_created`, `task_updated` with `data.changed` naming the changed fields in column order) in the same
transaction; a patch that changes nothing writes no row, no event and no actor. `tasks.New.BlockedBy` makes a new task
blocked at creation: each blocker gets its `task_deps` row and a `dep_added` event in the creating transaction, and an unknown
one is `ErrNotFound` with nothing written. `Store.Links` reads a task's `Deps` and `Blocking` (`{Ref, Title, Status}`, by id).

### 4.4 `leases`, `lease_resources`

| column | type | notes |
|---|---|---|
| id | ulid | |
| task_id | fk | |
| actor_id | fk | |
| intent | text | one line: what this actor is doing |
| ttl_seconds | int | as requested, clamped to [60, 43200]; default 7200 |
| started_at | ts | |
| renewed_at | ts | last renewal (= started_at initially) |
| expires_at | ts | renewed_at + ttl |
| state | `live` \| `ended` \| `expired` | |
| ended_at, ended_by | | `ended_by` set when someone other than the holder ends it |
| outcome | text, nullable | short summary written at end |
| acked_at, acked_by | ts, fk actor, nullable | the acknowledgement of an expired lease (§5.3); null until acknowledged |

`lease_resources(lease_id, resource)`: PK both, index on `resource`.

Both tables exist (migration `00004_leases`, in both dialects) with `n` (the short ref `L<n>`,
`domain.FormatLeaseRef`), the indexes `leases(state, expires_at)` and `lease_resources(resource)` (§4.10), and extra indexes
on `leases(task_id)` and `leases(actor_id)`. The database holds the ttl within [60, 43200] and the state to the three values.

An actor may hold several live leases (different tasks). Starting a lease on a task where the
same actor already has a live lease **renews** that lease instead (idempotent start).

`internal/leases` (`Store`: `Start`, `Renew`, `End`, `Ack`, `ExpireDue`, `Get`) writes each change in one transaction with its
event (`lease_started`, `lease_renewed`, `lease_ended`, `lease_acked`, `lease_expired`; `ref_id` is the lease's ULID, `data.ref`
is `L<n>`, `data.task` the task's ref). The rules, with the pure parts in `internal/domain`:

- **ttl** (`domain.ClampLeaseTTL`): clamped to [60, 43200], 7200 when absent. A renewal without a ttl keeps the stored one.
- **intent** 1 to 200 characters. **Resources** (`domain.NormalizeResources`): duplicates removed (first kept), each 1 to 100
  characters, at most 20 after deduplication. Violations are `ErrInvalid`, checked before anything is read or written.
- **Start** on an unknown task is `ErrNotFound`, on a `closed` task `ErrConflict`. When the actor holds a live lease on the task,
  the start renews it (`Result.Renewed`, event `lease_renewed`, resources kept unless sent); otherwise it is a new lease and
  `lease_started`. The result carries the conflicts (§5.2).
- **Renew** pushes `expires_at` to now plus the ttl and may replace intent and resources. A lease past its expiry is `ErrGone`, a
  `GoneError` carrying `ExpiredAt`, whether or not the sweep has marked it; an ended lease is `ErrConflict`. Any actor may renew.
- **End** sets `ended_at` and `outcome` (at most 200 characters); `ended_by` is set, and `data.ended_by` written, only when
  the ending actor is not the holder. A lease that is not live (ended or expired) is `ErrConflict`.
- **Ack** of an expired lease sets `acked_at` and `acked_by` and writes `lease_acked`; a second ack changes nothing and writes no
  event. A lease that has not expired is `ErrConflict`.
- **Expiry** (`ExpireDue`) marks live leases with `expires_at < now` as expired with the conditional update of §5.1 and writes one
  `lease_expired` (principal `system:expiry`, actor the holder, `data.expired_at`) per lease it won. Every write above runs it first, inside the write's own transaction: the expiry commits only with a request that succeeds. A refused write (invalid, unknown, closed, gone or conflicting) rolls it back and changes no row of any table (lease, actor, event, the authenticating key's `last_used_at`, and the authenticating session's row, §3.2), even while unrelated live leases are past expiry; those are expired by the sweeper or by the next read (`ExpireDue`, `Current`, `List`, `LiveOf`, `CheckIn`), each writing exactly one `lease_expired`.
- **Conflicts** (`domain.Conflicts`, §5.2): the live, unexpired leases of others sharing a resource, each with only the shared ones.
- **LiveOf** (`Store.LiveOf(taskID)`) lists a task's leases in state `live` with `expires_at >= now`, oldest first, resources sorted. It first runs `ExpireDue` (§5.1), so a lease past its expiry is marked expired and left out.

### 4.5 `asks`

| column | type | notes |
|---|---|---|
| id | ulid | |
| task_id | fk, nullable | an ask may be initiative-level or global |
| initiative_id | fk, nullable | |
| actor_id | fk | asker |
| question | text | one line |
| details | text | Markdown, optional |
| options | json text | optional list of suggested answers |
| mode | `stop` \| `poll` | |
| blocking | bool | |
| state | `open` \| `answered` \| `withdrawn` | |
| answer | text | |
| answered_by, on_behalf_of | fk actor | |
| channel | `ui` \| `api` \| `relay` | relay = recorded by the agent from a direct reply |
| created_at, answered_at | | |
| withdrawn_at, withdraw_reason | nullable | set when the ask is withdrawn; the reason is at most 200 characters |
| poll_seen_at | ts, nullable | when a long poll (§7.2) on the ask last held or returned; null until one has |

An ask's short ref is `A<n>`: `n` is the table's auto-incrementing key (`domain.FormatAskRef`; read back by
`domain.ParseAskRef` with `A` in either case and a positive `n`). Every read takes `A<n>` or the ULID, in either letter
case. Timestamps are UTC milliseconds (`BIGINT`), `options` is a JSON text list (`[]` when none) and `blocking` is a
boolean (`BOOLEAN` on Postgres, 0 or 1 on SQLite). Indexes: `asks(state, created_at)`, `asks(task_id)`, `asks(actor_id)`.

**Store** (`internal/asks`). `Raise` takes a `task` (ref or ULID), an `initiative` (slug or ULID) or neither; the
initiative of an ask on a task is the task's, and a task with a different initiative is invalid. A closed task or a
closed initiative is a conflict; an unknown one is not found. The question is 1 to 200 characters on one line, the
details at most 20000, the options at most 10 of 1 to 80, the mode `stop` (default) or `poll`. `Answer` takes an answer
of 1 to 20000 characters and a channel, defaulting to `ui` for a session and `api` for a key; `on_behalf_of` (a handle)
is allowed with `relay` only. `Withdraw` takes an optional reason. Answering or withdrawing an ask that is not `open` is
a conflict naming its state and changes no row, event or actor. `List` filters by `state` (`open` by default, `answered`,
`withdrawn` or `all`), asker, task and initiative, ordered by `id`, with `limit` and an `after` cursor (`A<n>` or ULID).
Each write appends `ask_raised`, `ask_answered` or `ask_withdrawn` (with the task, the initiative and `ref_id` set as they
apply) in the same transaction as the change.

### 4.6 `events`

The task timeline and the global change log. Append-only; never updated or deleted.

| column | type | notes |
|---|---|---|
| id | ulid | ordering key for "since" reads |
| at | ts | |
| actor_id | fk | who did it |
| principal | text | who authenticated: `oidc:<email>` or `key:<name>` (§3.1) |
| on_behalf_of | fk, nullable | |
| initiative_id | fk, nullable | denormalised for filtering |
| task_id | fk, nullable | |
| type | text | see below |
| ref_id | text, nullable | the lease / ask / note id the event is about |
| summary | text | one line, rendered for humans and agents |
| data | json text | type-specific fields |

Event types (closed set in v1, extended by migration):

`initiative_created`, `initiative_updated`, `initiative_closed`, `initiative_reopened`,
`task_created`, `task_updated`, `task_done`, `task_reopened`, `task_closed`,
`dep_added`, `dep_removed`,
`lease_started`, `lease_renewed`, `lease_ended`, `lease_expired`, `lease_acked`,
`ask_raised`, `ask_answered`, `ask_withdrawn`,
`check_recorded` (watch), `milestone` (an actor marks a notable point), `note_posted`.
`key_created` and `key_revoked` record an API key being made or revoked over HTTP (§3.3); they have no
`initiative_id` or `task_id`.

**Short ref and summary.** An event's sequence number `n` (the table's auto-incrementing key) is its short ref `E<n>`
(`domain.FormatEventRef`; read back by `domain.ParseEventRef` with `E` in either case and a positive `n`). `summary`
is never empty and is one line: a check's or milestone's text has its whitespace folded (`domain.OneLine`), the
original text stays in `data.summary`.

**Watch checks and milestones** (`internal/tasks`: `Check`, `Milestone`), each in one transaction with its event:

| call | event | `data` | row change |
|---|---|---|---|
| `Check(ref, summary, next_check_at?)` | `check_recorded`, summary `check on <ref>: <text>` | `{ref, summary, next_check_at}` | `next_check_at` = the given time, else now + `cadence_seconds`; sets `updated_at` |
| `Milestone(ref, summary, data?)` | `milestone`, summary `milestone on <ref>: <text>` | `{ref, summary}` plus `data` (the caller's object, under the key `data`) when given | none |

A summary is 1 to 200 characters once trimmed (`domain.ValidateSummary`), else `ErrInvalid`, checked before the task
is read. A check on a work task or on a closed watch task is a `ConflictError` (`ErrConflict`; "cannot record a check
on task aa-1: it is a work task"). A milestone is accepted on a task of any kind and status. An unknown task is
`ErrNotFound`. A refused call writes no row, event or actor. Both events carry the task's `task_id`, `initiative_id`
and `ref_id` (the task id).

**Overdue** is derived (`domain.Overdue`, `Task.Overdue`): kind `watch`, status not `closed` (a done watch task still
counts) and `next_check_at` strictly before the store's clock when the task is read. The `overdue` and `active` list filters use the same
clock instant and rule as the derived fields, so a task selected by `overdue=true` has `overdue: true`, down to a clock a fraction of a millisecond past the stored value.

**Reads** (`internal/events`: `Store.List(Query)`, `Store.ForTask(ref, after, limit)`). Events are returned in
insertion order (`n`) as `Event{N, ID, Ref, At, Type, Actor, Principal, OnBehalfOf, Initiative, InitiativeID, Task,
TaskID, RefID, Summary, Data}`: `Ref` is `E<n>`, `Actor` and `OnBehalfOf` are handles, `Initiative` a slug, `Task` a
task ref (the task id when the task row is gone) and the optional ones are empty when the event has none. Filters
combine with AND:

| filter | meaning |
|---|---|
| `since` | an event ULID or `E<n>` (either case): the events after that event. An RFC 3339 time: the events at or after that instant. Anything else is `ErrInvalid` (an `InvalidError` naming the value and what to send) |
| `after` | the page cursor, an event ULID or `E<n>`; a time or anything else is `ErrInvalid` |
| `initiative` | slug or ULID, either case; an unknown one is `initiatives.ErrNotFound` |
| `task` | task ref or ULID, either case; an unknown one is `tasks.ErrNotFound` |
| `type` | one or more event types (any of them); an empty one is `ErrInvalid` |
| `actor` | the handle of the actor |
| `limit` | 1 to 1000, default 100, else `ErrInvalid` |

A page holds `limit` events; `NextAfter` is the ULID of its last event when more follow, else empty.

Timeline markers: `lease_started`/`lease_ended`/`lease_expired` bound lease bars;
`milestone`, `ask_raised`, `ask_answered`, `check_recorded`, `task_done`, `task_closed`
draw as point markers. `lease_renewed` is stored but not drawn (it is the heartbeat).

### 4.7 `notes`

| column | type | notes |
|---|---|---|
| id | ulid | ordering key for "since" reads |
| actor_id | fk | author |
| summary | text | ≤ 200 chars |
| body | text, nullable | Markdown |
| task_id | fk, nullable | |
| initiative_id | fk, nullable | set automatically from the task when task_id is given |
| resource | text, nullable | e.g. `host:build-1` |
| mentions | json text | list of actor handles |
| created_at | ts | |

A note's short ref is `N<n>`: `n` is the table's auto-incrementing key (`domain.FormatNoteRef`; read back by
`domain.ParseNoteRef` with `N` in either case and a positive `n`). Every read takes `N<n>` or the ULID, in either letter
case. Timestamps are UTC milliseconds (`BIGINT`) and `mentions` is a JSON text list (`[]` when none). Migration
`00006_notes` creates the table and `cursors` (§4.8) in both dialects, with the §4.10 indexes.

Broadcast = `task_id IS NULL AND initiative_id IS NULL`, whatever its `resource`. **Every note writes a `note_posted`
event, in the same transaction as the note, whether it is bound to a task, to an initiative, to a resource or to
nothing (a broadcast)**: the event carries the note's `task_id` and `initiative_id` as they apply (none for a
broadcast), `ref_id` = the note's id, the one-line summary `note N3 on aa-1: <text>` (`on <slug>` for an initiative,
`on <resource>` for a resource, `(broadcast)` for neither) and `data` = `{ref, summary, broadcast}` plus `task`,
`initiative`, `resource` and `mentions` when set. The body is never in the event.

**Store** (`internal/notes`). `Post` takes a `task` (ref or ULID), an `initiative` (slug or ULID), a `summary`, a
`body`, a `resource` and `mentions`. The initiative of a note on a task is the task's, and a task with a different
initiative is invalid; an unknown task or initiative is not found. A closed task or initiative still takes notes. The
summary is 1 to 200 characters on one line, the body at most 20000, the resource at most 100 characters, the mentions at
most 20 handles of at most 100 characters without whitespace (repeats dropped, order kept). A refused post writes no
row, event or actor. `List` filters with AND by `since` (a note ULID or `N<n>`: the notes after it; an RFC 3339 time:
the notes at or after that instant), `after` (the page cursor, a note ULID or `N<n>`), `task`, `initiative` (a task's
notes belong to its initiative), `resources` (any of them), `broadcast` (`only` or `exclude`), `mention` (an exact
handle), `author` and `limit` (1 to 1000, default 100), ordered by `id`; bodies are left out unless `full` is set.
`NextAfter` is the ULID of the last note of a page when more follow, else empty. `since=cursor` is resolved by the
server (§6.7) from the cursor store, `internal/cursors`, before the query runs.

### 4.8 `cursors`

| column | type | notes |
|---|---|---|
| actor_id | fk | the reader; with `stream` the primary key |
| stream | text | `notes` or `events` (a check constraint refuses any other) |
| position | text | the id of the last note or event the actor read on that stream |

An optional server-side read cursor per actor and stream, so an agent may ask "since my last read" without keeping
state itself (`since=cursor`). Updated only when the reader asks: `PUT /cursors/{stream}` or `ack=true` on the note
list (§6.7). The position is always the ULID of an existing note or event; `internal/cursors` (`Store.Set`, `Ack`, `Get`)
resolves an `N<n>` or `E<n>` ref to it, upserts the actor and the row (one per actor and stream) in one transaction, and
reads a handle's position (empty when none is set, which reads from the start). A cursor write adds no event.

### 4.9 `api_keys`, `sessions`

`api_keys(n, id, name unique, key_hash unique, display_prefix, created_at, created_by,
last_used_at, expires_at, revoked_at)`. `key_hash` = SHA-256 of the full key. `created_by` is the creating principal, `key:<name>` or `oidc:<email>` for a key made over
HTTP and `admin-cli` for one made by `board admin keys create`. `expires_at` is when the key stops working; the
verifier answers 401 `key expired` from that instant on.

`sessions(id_hash pk, email, csrf_token, created_at, last_seen_at, expires_at)`. Rows past
`expires_at` are deleted by the same sweeper as leases (§5.1), on the same `BOARD_SWEEP_INTERVAL` tick; a session
that only the idle timeout has ended is deleted when it is next looked up by a request that is not refused (§3.2). `sessions` has no `n`: it is keyed by `id_hash`, has no short ref
and is indexed on `expires_at` for that sweep.

Phase 0 creates `actors`, `initiatives`, `initiative_tags`, `events`, `api_keys` and `sessions`; Phase 1 adds
`tasks` and `task_deps` (migration `00003_tasks`, which also adds `initiatives.next_task_seq`) and `leases` and
`lease_resources` (migration `00004_leases`, §4.4); Phase 2 adds `asks` (`00005_asks`, §4.5) and `notes` and
`cursors` (`00006_notes`, §4.7, §4.8). The other tables of this section arrive with the phases that use them. Migrations are additive and forward-only, and rows written before
an upgrade survive it. `events.task_id` is a plain nullable column with no foreign key. The indexes `events(task_id, id)` and `events(initiative_id, id)` exist, as does
`initiative_tags(tag)`.

### 4.10 Indexes (the hot queries)

- `events(id)` (PK) and `events(task_id, id)`, `events(initiative_id, id)`: since-T reads.
- `notes(id)`, `notes(task_id, id)`, `notes(resource, id)`, partial/filtered index for broadcasts
  where the dialect supports it (`WHERE task_id IS NULL AND initiative_id IS NULL`), plain
  composite otherwise.
- `leases(state, expires_at)`: expiry sweep and the live set.
- `lease_resources(resource)`: conflicts.
- `asks(state, created_at)`: open asks.
- `tasks(status, initiative_id)`, `tasks(kind, next_check_at)`, `tasks(closed_at)`: attention and archive.
- `initiative_tags(tag)`.

---

## 5. Derived state

### 5.1 Lease lifecycle

- **Start** `POST /api/v1/leases {task, intent, ttl, resources[]}` → lease + conflicts.
- **Renew** `POST /api/v1/leases/{id}/renew {ttl?, intent?, resources?}`: pushes `expires_at`.
  Agents renew at milestones and at least every `ttl/2`. Renewing an `expired` lease is an
  error (`410 Gone`) carrying the expiry time; the agent starts a new lease, and the gap is
  visible on the timeline.
- **End** `POST /api/v1/leases/{id}/end {outcome?}`.
- **Expire**: a sweeper marks `live` leases with `expires_at < now` as `expired` and writes
  `lease_expired`. `board serve` runs it every `BOARD_SWEEP_INTERVAL` (default `15s`; any Go duration
  above zero; `off`, `0` or `0s` turns the sweeper off, and any other value makes `board serve` exit 1
  naming the variable). It also runs lazily first on every read that returns leases (`GET /leases`,
  `GET /leases/{ref}`, and the live leases of `GET /tasks/{ref}`), so a read never shows a lease as live past
  its expiry even with the sweeper off. Expiry is idempotent and safe with several replicas (conditional
  update `WHERE state='live' AND expires_at < ?`: of two servers that race, one marks the lease and writes
  its one `lease_expired` event, the other finds it already expired). The same tick deletes expired
  sessions (§4.9).

### 5.2 Conflicts

Computed on read: the live, unexpired leases of **others** that hold any of the same resources as a lease. A
lease never conflicts with itself, but two leases of one actor on different tasks that share a resource do
conflict. An ended or expired lease conflicts with nothing. Starting or renewing a lease answers
`{lease, conflicts}` (§6.5); each conflict is `{lease, holder, task, intent, expires_at, resources}`: the other
lease's ref `L<n>`, its holder's handle, its task ref, its intent, its expiry, and only the resources both leases
hold (in the order of the lease asked about), the conflicts in lease-number order. The CLI prints them
prominently. Optional resource prefixes (`host:` vs `repo:`) carry no special meaning to the server.

### 5.3 Attention view

`GET /api/v1/attention` returns, each list newest-relevant first, each item with the
**actors involved** (`{handle, way_back?}`):

1. **Open asks** (`open_asks`): ref, question, task, asker, mode (and, for `poll`, `asker_waiting`:
   a poll is in flight or ended within the last 60 s), blocking flag, `created_at` and
   `age_seconds`.
2. **Done, not closed** tasks (`done_not_closed`): task, title, who marked done (`done_by`), when
   (`done_at`), and the last ended lease of the task with its outcome text (`last_lease`).
3. **Expired leases** (`expired_leases`) not yet acknowledged: lease, holder, task, title, intent,
   last renewal (`renewed_at`) and `expired_at`. The operator acknowledges
   (`POST /api/v1/leases/{id}/ack`) after looking; a later lease by anyone on the same task also
   clears it.
4. **Overdue watches** (`overdue_watches`): task, title, owner, `next_check_at`, last check event
   (`last_check`: summary, by, at).
5. **Live conflicts** (`conflicts`): resource, holders (lease, actor, task, intent, expiry), tasks.

Filters: `tag`, `initiative`, each repeatable. `counts` (`{open_asks, done_not_closed,
expired_leases, overdue_watches, conflicts, total}`) is in the response so the UI and CLI can show
badges without the lists, and `fields=counts` answers only `counts`. The HTTP shape is §6.8.

Derivation (`internal/attention`), judged at one instant after the leases that ran out are
expired (§5.1):

- Each section is newest relevant first: open asks by creation, done tasks by `done_at`, expired
  leases by expiry, overdue watches by `next_check_at` (the one most recently due first), conflicts
  by their newest lease; ties go to the later row (conflicts: the resource name).
- A lease is listed as expired when it is `expired`, has no `acked_at` and no lease of any state
  and holder started on its task after it. The last outcome of a done task is that of its last
  ended lease (nothing when none ended). An overdue watch is a `watch` task that is not closed and
  whose `next_check_at` is past; its last check is its latest `check_recorded` event.
- A conflict is a resource that two or more live leases hold (§5.2, so two leases of one actor on
  different tasks count); it lists every holder in lease-number order and the distinct tasks.
- `initiative` (repeatable: any of them) and `tag` (repeatable: the initiative carries all of
  them) keep the items on a task or ask of a matching initiative; an ask on no initiative is left
  out under either filter, and a conflict is kept when any of its leases is on a kept task. The
  counts and `total` (their sum) follow the filters. An empty board counts 0 everywhere.

---

## 6. HTTP API

### 6.1 Shape

- Base path `/api/v1`. JSON in and out. Errors are RFC 9457 problem documents.
- **The OpenAPI 3.1 document is the source of truth**, checked in (`api/openapi.yaml`). The
  server's handler interface is generated from it by oapi-codegen's strict-server mode
  (`internal/server/openapi.gen.go`: `StrictServerInterface`, typed request and response
  objects, `std-http` routing on Go's `ServeMux`) and the TypeScript types by openapi-typescript
  (`web/src/lib/api/schema.d.ts`). Phase 0 generates exactly these two outputs. The Go CLI client
  (`internal/cli/client.go`) is handwritten `net/http` code, not generated, and the drift check
  does not cover it; its requests and problem handling are held to the document by Go tests, not
  by generation. A CI check fails if generated code is stale: `ci/openapi-gen.sh` writes the two files, `ci/openapi-drift.sh`
  regenerates them into a scratch directory and exits non-zero when either checked-in file
  differs or is missing. `ci/lint.sh` lints the document with Redocly (`recommended-strict`:
  any finding fails) and runs the drift check. The document declares `GET /healthz`,
  `GET /readyz`, `GET /api/openapi.yaml`, `GET /api/openapi.json`, `GET /api/docs`, `GET /api/docs/assets/{name}`, the browser-login operations, the three `/api/v1/keys` operations (§3.3), the `GET`/`POST /api/v1/initiatives` and `GET`/`PATCH /api/v1/initiatives/{slug}` operations (§6.3), the `POST /api/v1/tasks` and `GET`/`PATCH /api/v1/tasks/{ref}`
  operations (§6.4), the lease operations and `GET /api/v1/now` (§6.5), the five ask operations (§6.6) and the RFC 9457 `Problem` schema (which carries `expired_at` on the 410 of a lease renewal). The binary embeds the document and serves it at
  `/api/openapi.yaml` (`application/yaml`, byte for byte the checked-in file) and
  `/api/openapi.json` (the same document converted to JSON), to a caller that presents a key
  or session like any other route.
- **Swagger UI** at `/api/docs`: Swagger UI 5.34.1 (swagger-ui-dist, Apache-2.0), its script and
  stylesheet embedded in the binary (`internal/swaggerui`) and served from
  `GET /api/docs/assets/{name}` (`text/javascript` or `text/css`, 404 as a problem document for
  any other name). The page loads the OpenAPI document from `/api/openapi.json` and lists every
  operation in it, and its Authorize dialog offers the `bearerAuth` scheme, so a caller pastes
  an API key and tries an operation from the page. The page and the assets reference no external
  host (no CDN) and need a key or session like every route but `/healthz`. A Go test fails on an
  `http(s)://` URL in the served page, and `e2e/swagger.spec.ts` opens `/api/docs` with a key
  minted by `board admin keys create` and asserts the rendered operations are those of
  `api/openapi.yaml`.
- Authentication per §3: session cookie (+ `X-CSRF-Token` on writes) or `Authorization: Bearer`.
  The OpenAPI document declares both schemes (`cookieAuth`, `bearerAuth`); only `/healthz` is
  unauthenticated (`/readyz`, the spec documents, `/api/docs` and `/metrics` all require a key or
  session; `features/unauthenticated.feature` reads the operation list from `api/openapi.yaml` and
  expects 401 from each operation but `GET /healthz`). Every other route,
  including the embedded UI at `/` and an unknown path, answers a request with no credential
  with 401 and a problem document (`application/problem+json`, `WWW-Authenticate: Bearer`)
  without touching the store. A Bearer key that is unknown, revoked (`key revoked`) or expired
  (`key expired`) gets 401. A valid key without a `Board-Actor` header gets 400. A presented key
  the store cannot check, because the database is unreachable or unmigrated, gets 503, never a 2xx.
- Every key-authenticated request names its actor: header `Board-Actor: <handle>` (required), optional
  `Board-Actor-Way-Back`, `Board-Actor-Meta` (a JSON object; anything else is 400, with no actor recorded),
  `Board-On-Behalf-Of`. The actor is upserted on use, where in Phase 0 a use is a write (§4.1); the way back and
  the meta header are stored on the actor row by that upsert. `Board-On-Behalf-Of` names the actor a write is made for.
  `POST /api/v1/tasks/{ref}/close` records it as the `on_behalf_of` of the `task_closed` event (§6.4); every other
  operation accepts the header and ignores it. Session requests always act as the operator.
- Idempotency: mutating `POST`s accept `Idempotency-Key`; a replay within 24 h returns the
  original response. Agents retry safely.
- Pagination: `limit` (default 100, max 1000) + `after=<ulid|ref>` cursor. Lists are ordered by id.
- Path and query parameters accept the short ref (§8.2) or the ULID of an object; responses
  carry both (`id`, `ref`).
- Errors carry a `next` field (the suggested next action) that the CLI prints (§8.1). Every problem
  document the server writes has `type`, `title`, `status`, `detail` and `next`; an error with no
  operation-specific advice carries a default `next` for its status (401: how to send a key and an
  actor, 404, 409, 5xx, and 400 "Fix the request and send it again."). A path or query parameter the
  generated router cannot read (`limit=abc`) is a 400 problem document too. The guarantee holds for
  every path, including a problem an operation returns as a typed response (an unknown Swagger asset is a
  404 with its own `next`): a final step over the whole handler fills the status default into any
  `application/problem+json` response that arrives without a non-empty `next`.
- Compact mode: `?view=compact` returns the minimal fields agents need (ids, refs, one-line
  text, times), to keep agent context small.

### 6.2 Endpoints (v1)

| method | path | purpose |
|---|---|---|
| GET | `/now` | the agent check-in bundle: my live leases, live leases by others, conflicts touching my resources, open asks I raised (and answers), notes since my cursor that concern me (my tasks, my resources, mentions, broadcasts), attention counts. `?resources=` to check before starting. The complete shape is in §6.5. |
| GET | `/attention` | §5.3, §6.8 |
| GET/POST | `/initiatives` | list (filters: tag, status, closed_within) / create |
| GET/PATCH | `/initiatives/{slug}` | read / update (title, description, tags, targets) |
| POST | `/initiatives/{slug}/close`, `/reopen` | |
| GET/POST | `/tasks` | list (filters: initiative, tag, status, kind, active, blocked, overdue, closed_within, q) / create |
| GET/PATCH | `/tasks/{ref}` | read (with deps, live leases, open asks) / update |
| POST | `/tasks/{ref}/done`, `/reopen`, `/close` | close takes `resolution` |
| POST/DELETE | `/tasks/{ref}/deps/{blockedByRef}` | |
| POST | `/tasks/{ref}/checks` | watch: record a check `{summary, next_check_at?}`; answers the task (§6.4) |
| POST | `/tasks/{ref}/milestones` | `{summary, data?}`; answers the task (§6.4) |
| GET | `/tasks/{ref}/events` | the task's events in order, with `limit`/`after` paging (§6.2 below) |
| GET/POST | `/leases` | list (filters: state, actor, task, resource) / start |
| POST | `/leases/{id}/renew`, `/end`, `/ack` | |
| GET/POST | `/asks` | list (state, actor, task, initiative) / raise (§6.6) |
| GET | `/asks/{id}` | read; `?wait=<seconds>` long-poll (§7.2) |
| POST | `/asks/{id}/answer`, `/withdraw` | |
| GET/POST | `/notes` | list (filters below) / post |
| GET | `/notes/{id}` | full body |
| GET | `/events` | global change log (filters: since, initiative, task, type, actor; paging: limit, after; below) |
| PUT | `/cursors/{stream}` | ack read position |
| GET | `/actors`, `/actors/{handle}` | with live leases and last_seen |
| GET | `/timeline` | data shaped for the timeline view: initiatives, tasks (planned + actual span), lease spans, markers, in a window `from`/`to` |
| GET | `/stream` | SSE live feed (§7.1) |
| POST | `/start`, `/finish`, `/ping`; GET `/catchup` | CLI shortcuts in one round trip (§8.4) |
| GET | `/meta` | server version, agent protocol version (§8.6), the caller's principal and actor |
| PUT | `/actors/me/protocol-seen` | record that the calling actor has read protocol version N (§8.6) |
| GET | `/healthz` | liveness, **unauthenticated** |
| GET | `/readyz` | 200 when the DB is reachable and migrated, 503 otherwise |
| GET/POST | `/keys`, POST `/keys/{name}/revoke` | API keys (§3.3) |
| GET | `/auth/login`, `/auth/oidc/callback`; GET `/auth/session`; POST `/auth/logout` | OIDC (§3.2); outside `/api/v1` |
| GET | `/metrics` | Prometheus text |

**Events** (`GET /api/v1/events`, `GET /api/v1/tasks/{ref}/events`, in `api/openapi.yaml`): a page of
`{items, next_after?}` ordered by the order the events were written (the `n` behind `E<n>`). Each item is `{id, ref,
at, type, actor, principal, initiative?, task?, ref_id?, summary, data}`: `id` the ULID, `ref` the short ref `E<n>`,
`actor` the handle of the actor it was written as, `principal` the authenticated principal, `initiative` a slug, `task`
a ref (`<slug>-<seq>`), `ref_id` the id of the object it is about, `summary` the one line of §4.6; the optional keys are
omitted when the event has none. Filters combine with AND: `initiative` (slug or ULID) and `task` (ref or ULID)
keep the events that name them, `type` (repeatable, OR among the values) keeps those types, `actor` one handle, and
`since` the events after an event given as its ULID or `E<n>` ref (either letter case), from an RFC 3339 time on
(`at >= since`), or after the caller's `events` cursor (`since=cursor`, §6.7: with no cursor set it reads from the
start, and reading moves nothing). An empty `type`, a `since` that is none of those forms, an `after` that is not an event ULID or
`E<n>` ref, or a `limit` outside 1 to 1000 (default 100) is **400** with a `next`. An `initiative` or `task` that names
nothing is **404**; a `type` or `actor` that matches nothing is **200** with `items: []`. `next_after` is the ULID of
the last item to send as `after` for the following page, and is omitted on the last page (a page that ends exactly at
the last event has none). `GET /tasks/{ref}/events` is `GET /events?task={ref}` with only `limit` and `after`; an
unknown task is **404**.

**Note filters**: `since=<ulid|RFC3339|cursor>`, `task=<ref>`, `initiative=<slug>`,
`resource=<r>` (repeatable), `broadcast=only|exclude`, `mention=<handle>`, `author=<handle>`,
`fields=summary` (default for lists; bodies via `/notes/{id}` or `fields=full`); §6.7 describes the endpoints.

### 6.3 Initiatives

`GET/POST /api/v1/initiatives`, `GET`/`PATCH /api/v1/initiatives/{slug}` and `POST /api/v1/initiatives/{slug}/close` and
`/reopen` are in `api/openapi.yaml`.

- **`POST /api/v1/initiatives`**, body `{slug, title, description?, tags?, target_start?, target_end?}`.
  `slug` matches `[a-z0-9-]{2,40}` (`domain.ValidateSlug`). `title` is 1 to 200 characters,
  `description` Markdown of at most 20000, `tags` at most 20, each trimmed, lowercased, matching
  `[a-z0-9][a-z0-9._:-]{0,39}`, repeats dropped (`domain.NormalizeTags`). `target_start` and
  `target_end` are `YYYY-MM-DD` dates, the end not before the start. Answers **201** with the
  initiative. A bad field is **400**; a slug already taken is **409** whose `next` is
  `Read it with GET /api/v1/initiatives/<slug>, or send another slug.` A refusal changes nothing: no
  row, event or actor. In one transaction the server inserts the row and its tags, upserts the actor
  (`Board-Actor`; a session acts as the operator) and appends an `initiative_created` event whose
  `principal` is `key:<name>` or `oidc:<email>`, whose actor is that handle, whose `initiative_id`
  and `ref_id` are the new id and whose `data` is `{slug, title, tags}`.
- **`GET /api/v1/initiatives/{slug}`** reads one by slug or ULID, in either letter case; 404 with a `next` otherwise. A reference is
  resolved against the stored rows (`domain.RefLookups`), never by the shape of its text: a slug is
  any `[a-z0-9-]{2,40}`, including one that also reads as a ULID (26 digits, or a ULID in lowercase),
  and the text as given is tried first as the slug or the id it spells, then in its other case.
- **`PATCH /api/v1/initiatives/{slug}`**, body any of `title`, `description`, `tags` (which replaces the set),
  `target_start`, `target_end`. A field left out keeps its value. `null` clears `target_start` and `target_end`, and is
  **400** for `title`, `description` and `tags`. `slug` is immutable, and a body that names it, even unchanged, is
  **400** whose `next` says to leave it out. The same field rules as a create apply to the initiative as it would be
  after the patch (so a new `target_end` is checked against the stored `target_start`). A patch that changes nothing,
  a `{}` or values equal to the stored ones (tags compared after normalising), answers **200** and writes no row,
  no event and no actor. A change answers **200** with the initiative, and in one transaction updates the row, replaces
  the tags when they changed, upserts the actor and appends one `initiative_updated` event whose `principal` and actor
  are as for a create and whose `data` is `{slug, changed}`, `changed` naming the changed fields in the order `title`,
  `description`, `tags`, `target_start`, `target_end`. A bad field is **400**, an unknown initiative **404**, and a
  refusal changes nothing: no row, tag, event or actor. A closed initiative can still be edited. A zero-length body is
  **400** (the body is required JSON); `{}` is the empty patch.
- **`POST /api/v1/initiatives/{slug}/close`**, optional body `{force?: bool}`. Answers **200** with the initiative
  (`status` `closed`, `closed_at`, `closed_by` the acting actor). An initiative whose tasks are all closed, or that has
  none, closes at once. With a task that is `open` or `done` and no `force` (absent or `false`), the answer is **409**
  whose `detail` says how many tasks are not closed and whose `next` names the `force` option, and nothing changes.
  With `force: true`, in one transaction the server first closes each task that is not closed as `dropped`, in task
  order, each exactly as `POST /tasks/{ref}/close` does (a `task_closed` event with `data.resolution` `dropped` and
  `data.from` the status it left; a done task keeps `done_at` and `done_by`), and then appends one `initiative_closed`
  event whose `data` is `{slug, force, dropped}`, `dropped` the number of tasks it closed; tasks already closed keep
  their resolution. The row is updated first, so a task cannot be created in the initiative while the close runs. A
  `Board-On-Behalf-Of` header is stored on the `task_closed` and `initiative_closed` events. Closing a closed initiative
  is **409** whose `next` says to reopen it, an unknown initiative is **404**, and a refusal writes no row, event or
  actor. The operation needs both the initiatives and the tasks store and answers **503** without them.
- **`POST /api/v1/initiatives/{slug}/reopen`** takes no body. A closed initiative becomes `open` again, `closed_at` and
  `closed_by` are cleared, one `initiative_reopened` event (`data` `{slug}`) is appended and the initiative is
  answered with **200**; its tasks stay closed, each reopened one by one. An open initiative is **409** and writes
  nothing; an unknown one is **404**.
- A closed initiative refuses task writes with **409**: `POST /api/v1/tasks` naming it, `PATCH /api/v1/tasks/{ref}`
  and `POST /api/v1/tasks/{ref}/reopen` for one of its tasks (§6.4). It can still be edited itself (`PATCH` above).
- **`GET /api/v1/initiatives`** answers `{items, next_after?}`, items ordered by id (creation order).
  `tag` (repeatable) keeps initiatives carrying every tag given. `status` is `open` (the default), `closed` or
  `all`. `closed_within` is `7d`, `30d`, `90d` or a whole number of days from 1 to 36500 and needs `status` `closed`
  or `all`, keeping the initiatives closed in the last that many days (with `all` the open ones stay); with `open`,
  explicit or by default, it is a 400, as is any other `status` (letter case matters) or `closed_within`, each with a
  `next`. `limit` is 1 to 1000 (default 100; 0
  is a 400). `after` is a ULID or a slug in either letter case (`domain.ParseCursor`, resolved as above): the
  page starts after that initiative, a ULID that names no row pages by id, and any other text that
  names none is a 400. `next_after`, the id to send as `after`, is
  present exactly when more initiatives follow the page, so a `limit` equal to the number left is the
  last page and has none. Before it reads, the list expires the leases whose `expires_at` has passed (§5.1:
  `lease_expired`, once each), so with the sweeper off an initiative whose only lease ran out already shows
  `live_leases` 0. It answers **503** without the tasks and leases stores, which it counts with.
- Each item of the list is an initiative (below) plus `counts` `{open, done, closed}`, how many tasks the initiative
  holds in each status (all zero for none), and `live_leases`, how many live leases sit on those tasks (a lease past its
  `expires_at` is not live). The single-initiative reads and writes do not carry them.
- An initiative is `{id, ref, slug, title, description, status, tags, target_start?, target_end?,
  created_by, created_at, closed_at?, closed_by?}`: `ref` is the slug, `tags` are sorted, `created_by`
  is the creating actor's handle, absent fields are omitted.
- Not yet served: `?view=compact` and `Idempotency-Key` replays (§6.1) for these operations. The
  server answers 503 problem documents when it runs without a store.

### 6.4 Tasks

`POST /api/v1/tasks`, `GET`/`PATCH /api/v1/tasks/{ref}`, `POST /api/v1/tasks/{ref}/done`, `/reopen` and `/close`, and
`POST`/`DELETE /api/v1/tasks/{ref}/deps/{blockedByRef}`, `POST /api/v1/tasks/{ref}/checks` and `/milestones` and
`GET /api/v1/tasks/{ref}/events` (the events of §6.2) are in `api/openapi.yaml`, and so is the list
(`GET /api/v1/tasks`, below). Any authenticated
principal may use them. The server answers 503 problem documents when it runs without a store.

- **`POST /api/v1/tasks`**, body `{initiative, title, description?, kind?, planned_start?, planned_end?,
  cadence_seconds?, next_check_at?, external_refs?, blocked_by?}`. `initiative` is a slug or ULID, in either letter case.
  `title` is 1 to 200 characters after trimming, `description` Markdown of at most 20000. `kind` is `work` (the default)
  or `watch`. A watch task needs `cadence_seconds` (60 to 31536000), and its `next_check_at` defaults to now plus the
  cadence; either watch field on a work task is **400**. `planned_start` and `planned_end` are RFC 3339 times, the end
  not before the start. `external_refs` is at most 20 items `{label, url}`, `label` 1 to 80 characters and `url` `http`
  or `https`. `blocked_by` lists tasks (ref or ULID, either letter case, repeats counted once) the new task starts out
  blocked by. Answers **201** with the task (below). A bad field is **400**, an unknown initiative or an unknown
  blocker is **404**, and a closed initiative is **409**. A refusal changes nothing: no row, event, actor, and the
  task number is not used up. In one transaction the server upserts the actor, numbers the task `<slug>-<seq>` from the
  initiative's counter, inserts the row and appends `task_created` (`data` is `{ref, initiative, title, kind}`) and then
  one `dep_added` per blocker (§4.3). A new task cannot close a dependency cycle.
- **`GET /api/v1/tasks`** lists tasks in id order (creation order), a page at a time, each item the task document
  without `deps` and `blocking` (`TaskListItem`: the fields below plus `live_leases`). Filters combine, and none is
  needed: `initiative` (slug or ULID, either letter case) and `tag` (an initiative tag) may repeat, a task being kept
  when it belongs to any initiative given and its initiative carries every tag given (`domain.NormalizeTags` rules);
  `status` is `open`, `done`, `closed`, `not_closed` (the default) or `all`; `kind` is `work` or `watch`; `active`
  (a live lease, as in the task document), `blocked` (an open dependency or an open blocking ask) and `overdue` (§4.3, §4.6) keep the tasks whose derived state is
  `true` or `false`; `closed_within` is `7d`, `30d`, `90d` or a whole number of days from 1 to 36500 and needs `status`
  `closed` or `all`, keeping the tasks closed in the last that many days (with `all` the tasks that are not closed stay);
  `q` keeps the tasks whose title or description contains the text ignoring letter case, for any Unicode letter and not only ASCII; the same tasks match on SQLite and on Postgres, because the match runs in the service and not in the database's own case folding. `%`, `_` and `\` match themselves.
  `limit` is 1 to 1000, default 100, and `after` is the ULID or ref of the last task of the previous page, in either
  letter case; `next_after` in the answer is the ULID to send and is absent on the last page. A bad `status`, `kind`,
  `tag`, `closed_within`, `limit` or `after` (a ref naming no task and not ULID-shaped), or a flag that is not `true` or
  `false`, is **400** with a `next`; an `initiative` naming nothing is **404**. Before it reads, the list expires the
  leases whose `expires_at` has passed (§5.1: `lease_expired`, once each), so with the sweeper off a task whose lease ran
  out is already not `active` and `active=true` leaves it out.
- **`GET /api/v1/tasks/{ref}`** reads one task by `<slug>-<seq>` or ULID in either letter case; **404** with a `next`
  naming `GET /api/v1/tasks/<ref>` for a ref that names no task, however it is spelled.
- **`PATCH /api/v1/tasks/{ref}`**, body any of `title`, `description`, `planned_start`, `planned_end`,
  `cadence_seconds`, `next_check_at`, `external_refs` (which replaces the list). A field left out keeps its value. `null`
  clears `planned_start`, `planned_end` and `next_check_at` (nullable). `cadence_seconds` is not nullable on a watch task: it
  is required there and stays within 60 to 31536000, so `null`, a value under 60 or one over 31536000 is **400** and the
  stored cadence is unchanged (on a work task `null` is the no-op it already is). `null` is also **400** for `title`,
  `description` and `external_refs`. `kind` and `initiative` are immutable, and a
  body that names either, even unchanged, is **400**. The same field rules as a create apply to the task as it would be
  after the patch. A patch that changes nothing, a `{}` or values equal to the stored ones, answers **200** and writes
  no row, no event and no actor. A change writes the row and one `task_updated` event whose `data` is
  `{ref, changed}`, `changed` naming the changed fields in column order, in one transaction. A closed task is **409**
  (`next` says to reopen it) and so is a task of a closed initiative. An unknown ref is **404**.
- **`POST /api/v1/tasks/{ref}/done`, `/reopen` and `/close`** move a task between `open`, `done` and `closed`, and each
  answers **200** with the task document after the move. The allowed moves are `done` from `open` (sets `done_at` and
  `done_by`), `reopen` from `done` or `closed` (back to `open`, clearing `resolution`, `done_at`, `done_by`, `closed_at`
  and `closed_by`) and `close` from `open` or `done` (sets `closed_at`, `closed_by` and `resolution`; a task that was
  done keeps its `done_at` and `done_by`). Any other move is **409** and the problem's `detail` names the task and its
  current status (`cannot mark done task aa-1: it is closed`), which covers `done` on a done or closed task, `reopen`
  on an open task and `close` on a closed task; reopening a task of a closed initiative is **409** too. An unknown ref
  is **404**. `/done` and `/reopen` take no body. `/close` takes `{resolution}`, one of `completed`, `dropped`,
  `superseded` or `duplicate`; a missing body, a missing resolution or any other value (case counts) is **400** and
  the `next` lists the four. Each move runs in one transaction, upserts the acting actor and appends one event
  (`task_done`, `task_reopened`, `task_closed`) whose `data` is `{ref, from}` (the status it left), plus `resolution`
  for a close. A refused request changes nothing, and records no actor. A `Board-On-Behalf-Of` header on `/close`
  upserts the actor it names and stores it as the `task_closed` event's `on_behalf_of` (`operator` when an agent
  closes on instruction); without the header, or on a refused close, the event's `on_behalf_of` is null. A closed
  task is not editable (`PATCH` is **409** until it is reopened).
- **`POST /api/v1/tasks/{ref}/deps/{blockedByRef}`** makes `ref` blocked by `blockedByRef` (each a ref or ULID in either
  letter case; the two tasks may be in different initiatives) and answers **200** with the dependent task, whose `deps`
  and derived `blocked` now show it. In one transaction it upserts the actor and appends `dep_added` on the dependent
  task, `data` `{ref, blocked_by}` with the blocker's ref. A dependency that already exists is **200** too and writes
  no row, no event and no actor. A task blocked by itself is **400**. A dependency that would close a cycle of any
  length is **409**: the problem's `detail` names the path, each task blocked by the next
  (`dependency would close a cycle: aa-1 is blocked by aa-2 is blocked by aa-1`), and nothing is written. An unknown
  task, on either side, is **404**. The status of the tasks does not matter: closed tasks may be linked.
- **`DELETE /api/v1/tasks/{ref}/deps/{blockedByRef}`** removes that dependency and answers **200** with the dependent
  task after the removal, appending `dep_removed` with the same `data`. A dependency that does not exist is **404**
  (`detail` says `aa-2 is not blocked by aa-1`), and so is an unknown task; nothing is written then. A dependency is
  open while its blocker is not `closed`, so `blocked` stays true for a blocker that is only `done` and turns false once
  the blocker is closed (and true again if it is reopened); `GET` reads the two lists as `deps` and `blocking`.
- **`POST /api/v1/tasks/{ref}/checks`**, body `{summary, next_check_at?}`, records a check on a watch task and answers
  **200** with the task. `summary` is 1 to 200 characters (**400** otherwise, and for a missing body). In one
  transaction the server upserts the actor, moves `next_check_at` to the time sent, or to now plus `cadence_seconds`
  when none is sent (a time in the past is accepted, and makes the task overdue), touches `updated_at` and appends
  `check_recorded`, `data` `{ref, summary, next_check_at}`, summary `check on aa-2: <summary>`. A work task is **409**
  (`detail` names the task and says it is a work task), and so is a closed watch task; an unknown task is **404**. A
  refused check changes nothing and records no actor. `overdue` is true only for a watch task that is not closed with
  `next_check_at` before now, so a later check, or closing the task, clears it.
- **`POST /api/v1/tasks/{ref}/milestones`**, body `{summary, data?}`, records a milestone on a task of any kind and
  status (open, done or closed) and answers **200** with the task, which it does not change. `summary` is 1 to 200
  characters, `data` a JSON object; either wrong, or no body, is **400**, and an unknown task is **404**. It appends
  `milestone`, `data` `{ref, summary}` plus the caller's object under `data` when one was sent, summary
  `milestone on aa-2: <summary>`.
- **`GET /api/v1/tasks/{ref}/events`** lists the events whose task is `ref`, oldest first, so a task that was created,
  edited, marked done, reopened and closed reads `task_created`, `task_updated`, `task_done`, `task_reopened`,
  `task_closed`. Events of other tasks and of the initiative itself are not in it. See §6.2 for the item and the paging.
- **The task document** (all of the above answer it, `TaskDetail`): `{id, ref, initiative, seq, title, description, kind,
  status, resolution?, planned_start?, planned_end?, cadence_seconds?, next_check_at?, external_refs, created_by,
  created_at, updated_at, done_at?, done_by?, closed_at?, closed_by?, active, blocked, overdue, deps, blocking,
  live_leases, open_asks}`. `initiative` is the slug, `ref` is `<slug>-<seq>` and the people are actor handles. Absent optional
  fields are omitted, and a task with none of its own has `external_refs`, `deps`, `blocking`, `live_leases` and `open_asks` as `[]`.
  `deps` are the tasks it is blocked by and `blocking` the tasks that name it as a blocker, each `{ref, title,
  status}` in creation order, whatever their status. `live_leases` are its leases in state `live` whose `expires_at`
  is not before now, oldest first, each a `Lease` (`{id, ref, task, actor, intent, ttl_seconds, started_at, renewed_at,
  expires_at, state, resources}`, resources sorted); the read first expires the due leases (§5.1), so a lease past its expiry is not listed. The derived `active` is a non-empty
  `live_leases`, `open_asks` are the task's asks in state `open`, blocking or not, oldest first, each an `Ask` (§6.6); `blocked` is an open dependency or an open blocking ask (§4.3) and `overdue` is §4.6's rule at the time of the read.
- A PATCH body is read with a three-state field (`internal/server` `Maybe`): left out, `null`, or a value. Fields the
  document does not name are ignored.

### 6.5 Leases

`POST`/`GET /api/v1/leases`, `GET /api/v1/leases/{ref}` and `POST /api/v1/leases/{ref}/renew`, `/end` and `/ack` are in
`api/openapi.yaml`. `{ref}` is `L<n>` (either letter case) or the lease's ULID; one that names no lease is **404**
(`there is no lease L9`). Any authenticated principal may use them. The server answers 503 problem documents when
it runs without a store. The rules are those of `internal/leases` (§4.4). The sweeper that expires leases in the
background runs every `BOARD_SWEEP_INTERVAL` (§5.1, §13); every operation here also expires the due leases
first, so none shows a lease live past its expiry, with the sweeper off too.

- **`POST /api/v1/leases`**, body `{task, intent, ttl_seconds?, resources?}`. `task` is a task ref or ULID, `intent`
  1 to 200 characters, `ttl_seconds` an integer clamped to [60, 43200] (7200 when absent), `resources` at most 20
  strings of 1 to 100 characters, repeats dropped, kept sorted. A missing body, a missing `task`, a bad intent or a
  bad resource is **400**, an unknown task **404**, and a **closed** task **409** (`cannot start a lease on task aa-1: it is
  closed`; a done task is allowed). When the caller already holds a live lease on the task, that lease is renewed
  instead: **200**, the same lease, `lease_renewed`, its intent replaced and its resources replaced only when
  `resources` was sent. Otherwise it is **201**, a new lease and `lease_started`. The answer is `{lease, conflicts}`
  (§5.2). A refusal changes nothing: no lease, event or actor. In one transaction the server upserts the actor
  and writes the lease with its event, whose `data` is `{ref, task, intent, ttl_seconds, expires_at, resources}`.
- **`POST /api/v1/leases/{ref}/renew`**, body (optional) `{ttl_seconds?, intent?, resources?}`: pushes `expires_at`
  to now plus the ttl (the one sent, clamped, else the lease's own) and replaces the intent and the resources when
  they are sent; any actor may renew. Answers **200** with `{lease, conflicts}` and appends `lease_renewed`. An
  expired lease is **410**, whether or not the sweeper has marked it, and its problem document carries
  `expired_at`, the time the lease ran out (RFC 3339); an ended lease is **409** (`cannot renew lease L1: it is
  ended`); a bad intent or resource is **400**.
- **`POST /api/v1/leases/{ref}/end`**, body (optional) `{outcome?}`, an outcome of at most 200 characters (**400**
  otherwise): sets `state` `ended`, `ended_at` and `outcome`, and `ended_by` **only when the caller is not the
  holder**, and appends `lease_ended` whose `data.ended_by` is set under the same condition. Answers **200** with the
  lease. A lease that is not live, ended or expired, is **409** (`cannot end lease L1: it is ended`).
- **`POST /api/v1/leases/{ref}/ack`** records the operator's acknowledgement of an expired lease: `acked_at` and
  `acked_by`, and appends `lease_acked`. Answers **200** with the lease; acknowledging again changes nothing and
  writes no event. A lease that has not expired, live or ended, is **409** (`cannot acknowledge lease L1: it is
  live`). A later lease on the same task also counts as acknowledged (derived, §5.3).
- **`GET /api/v1/leases/{ref}`** reads one lease.
- **`GET /api/v1/leases`** answers `{items, next_after?}`, items ordered by lease number (oldest first). Filters
  combine with AND: `state` is `live` (the default, also when empty), `ended`, `expired` or `all` (anything else is
  **400** with a `next`); `actor` keeps one holder's handle; `task` (ref or ULID) one task's leases, **404** for a task that
  names nothing; `resource` the leases that hold that resource. A filter that matches nothing is **200** with
  `items: []`. `limit` is 1 to 1000 (default 100; 0 is **400**). `after` is an `L<n>` ref or a lease ULID: the page
  starts after that lease; any other text is **400**. `next_after`, the ULID of the last item, is present exactly when
  more leases follow, so a `limit` equal to the number left is the last page and has none.
- **The lease document** (`Lease`): `{id, ref, task, actor, intent, ttl_seconds, started_at, renewed_at, expires_at,
  state, resources, ended_at?, ended_by?, outcome?, acked_at?, acked_by?}`. `task` is the task's ref, `actor` the
  holder's handle, `state` `live`, `ended` or `expired`, and absent optional fields are omitted. The task document
  (§6.4) lists a task's live leases in `live_leases` and is `active` while it has one. Marking the task done or
  closing it leaves its leases live: they end by `/end` or by running out.

- **`GET /api/v1/now`**, `?resources=r1,r2` (optional), is the check-in of the caller. It answers exactly `{actor, my_leases,
  others_leases, conflicts, my_asks, notes, notes_more, attention}`, every list `[]` when empty. Like every lease read it
  expires the due leases first, so a lease past its expiry is left out and its one `lease_expired` event is written, with the
  sweeper off too; it writes nothing else, does not upsert the actor and **moves no cursor** (the `notes` cursor stays where
  it was, set or not). `actor` is the caller's handle. `my_leases` are the caller's live leases (`Lease` documents, oldest
  first) with the task's `title` added beside the task `ref` and `expires_at`; `others_leases` are the live leases of other
  actors, oldest first. `conflicts` are `{lease, holder, task, intent, expires_at, resources}` (§5.2), each other live lease
  once in lease-number order, carrying only its shared resources: those that hold a resource of one of the caller's live
  leases (another of the caller's own leases on a different task included, a lease never conflicting with itself), and those
  of other actors that hold one of the `resources` asked about. The caller's own leases never conflict with the resources
  asked about. `resources` is a comma-separated list of at most 20 resources of 1 to 100 characters (repeats dropped); an
  empty entry or a bad one is **400** with a `next`.
  - **`my_asks`** are `Ask` documents (§6.6), oldest first, of the asks the caller raised: every **open** one, and every
    **answered** one (with its `answer`, `answered_by` and `channel`) whose `answered_at` is at or after the earlier of the
    creation time of the note the caller's `notes` cursor holds and 24 hours before now (so an answer stays for at least a
    day, and longer while the notes cursor lies further back); with no `notes` cursor it is the 24 hours alone. A withdrawn
    ask and the asks of other actors are never listed.
  - **`notes`** are `Note` summaries (§6.7, with `has_body` and no `body`), oldest first, of the notes **after the caller's
    `notes` cursor** (all of them when none is set; a note at or before the cursor is acknowledged and absent) that concern
    the caller: a note on a task the caller holds a live lease on, a note bound to a `resource` that one of the caller's live
    leases holds, a note whose `mentions` hold the caller's handle exactly, or a broadcast (§4.7). A note on another actor's
    task that is none of these is absent. At most 20 are listed (the oldest 20, so acking the list moves the cursor on
    without skipping any) and **`notes_more`** is how many more concern the caller after them (0 when none).
  - **`attention`** is the `counts` of §5.3 and §6.8 for the whole board, with no filter: `{open_asks, done_not_closed,
    expired_leases, overdue_watches, conflicts, total}`, the same numbers `GET /api/v1/attention?fields=counts` answers.
  - It is a 503 unless the lease, ask, note, cursor and attention stores are all behind the server.

### 6.6 Asks

`POST`/`GET /api/v1/asks`, `GET /api/v1/asks/{ref}` and `POST /api/v1/asks/{ref}/answer` and `/withdraw` are in
`api/openapi.yaml`. `{ref}` is `A<n>` (either letter case) or the ask's ULID (either letter case); one that names no
ask is **404** (`there is no ask A9`). Any authenticated principal may use them. The server answers 503 problem
documents when it runs without a store. The rules are those of `internal/asks` (§4.5). Long-polling an ask (`?wait=`)
is §7.2.

- **`POST /api/v1/asks`**, body `{task?, initiative?, question, details?, options?, mode?, blocking?}`. `task` is a task
  ref or ULID and `initiative` a slug or ULID; both may be left out (a board-wide ask). `question` is 1 to 200
  characters on one line, `details` at most 20000 characters, `options` at most 10 strings of 1 to 80 characters,
  `mode` `stop` (the default) or `poll`, and `blocking` defaults to false. A missing body, a bad field, or a `task` and
  an `initiative` that disagree (the task does not belong to that initiative; the initiative of an ask on a task is
  the task's) is **400** with a `next`; an unknown task or initiative is **404**; a **closed** task or a closed
  initiative is **409** (`cannot raise an ask on task aa-1: it is closed`). Otherwise it is **201**, the ask in
  state `open`, and `ask_raised`. A refusal changes nothing: no ask, event or actor. In one transaction the server
  upserts the actor and writes the ask with its event, whose `task` and `initiative` are those of the ask and whose
  `data` is `{ref, question, mode, blocking, task?, initiative?}`.
- **`POST /api/v1/asks/{ref}/answer`**, body `{answer, channel?, on_behalf_of?}`. `answer` is 1 to 20000
  characters, `channel` `ui`, `api` or `relay` (left out it is `ui` for a browser session and `api` for a key) and
  `on_behalf_of` the handle of the person a relayed answer came from, allowed with `relay` only (**400** otherwise).
  An open ask becomes `answered` with `answer`, `answered_by` (the caller), `on_behalf_of`, `channel` and
  `answered_at`, and `ask_answered` is appended (principal and actor those of the caller, `on_behalf_of` that
  person). Answers **200** with the ask. An ask that is not open is **409** naming its state (`cannot answer ask A1: it
  is answered`).
- **`POST /api/v1/asks/{ref}/withdraw`**, body (optional) `{reason?}` of at most 200 characters (**400** otherwise).
  An open ask becomes `withdrawn` with `withdrawn_at` and `withdraw_reason`, and `ask_withdrawn` is appended. Answers
  **200** with the ask. An ask that is not open is **409** naming its state (`cannot withdraw ask A1: it is answered`).
- **`GET /api/v1/asks/{ref}`** reads one ask.
- **`GET /api/v1/asks`** answers `{items, next_after?}`, items ordered by id (the order they were raised). Filters
  combine with AND: `state` is `open` (the default, also when empty), `answered`, `withdrawn` or `all` (anything else is
  **400** with a `next`); `actor` keeps one asker's handle; `task` (ref or ULID) and `initiative` (slug or ULID) keep the
  asks that name them, **404** for one that names nothing. A filter that matches nothing is **200** with `items: []`.
  `limit` is 1 to 1000 (default 100; 0 is **400**). `after` is an `A<n>` ref or an ULID: the page starts after
  that ask; any other text is **400**. `next_after`, the ULID of the last item, is present exactly when more asks
  follow.
- **The ask document** (`Ask`): `{id, ref, task?, initiative?, asker, question, details, options, mode, blocking, state,
  answer?, answered_by?, on_behalf_of?, channel?, created_at, answered_at?, withdrawn_at?, withdraw_reason?,
  asker_waiting}`. `task` is the task's ref, `initiative` its slug, `asker` the handle of the actor that raised
  it, `state` `open`, `answered` or `withdrawn`, and absent optional fields are omitted. `asker_waiting` is derived: true
  when the ask is open, its mode is `poll` and a long poll on it (`poll_seen_at`, §4.5) was seen within the last 60
  seconds, so it is false until a long poll has run.

### 6.7 Notes

`POST`/`GET /api/v1/notes` and `GET /api/v1/notes/{ref}` are in `api/openapi.yaml`. `{ref}` is `N<n>` (either letter
case) or the note's ULID (either letter case); one that names no note is **404** (`there is no note N9`). Any
authenticated principal may use them. The server answers 503 problem documents when it runs without a store. The rules
are those of `internal/notes` (§4.7); `PUT /api/v1/cursors/{stream}`, `since=cursor` and `ack=true` are the cursors of §4.8.

- **`POST /api/v1/notes`**, body `{summary, body?, task?, initiative?, resource?, mentions?}`. `summary` is 1 to 200
  characters on one line, `body` (Markdown) at most 20000 characters, `resource` at most 100 characters (such as
  `host:build-1`) and `mentions` at most 20 actor handles. `task` is a task ref or ULID and `initiative` a slug or ULID;
  both may be left out. The initiative of a note on a task is the task's. A missing body, a bad field, or a `task` and
  an `initiative` that disagree (the task does not belong to that initiative) is **400** with a `next`; an unknown task
  or initiative is **404**. A closed task or initiative still takes notes. Otherwise it is **201**, the note, and
  `note_posted`, which every note writes, a broadcast (neither task nor initiative, with or without a resource)
  included. A refusal changes nothing: no note, event or actor. In one transaction the server upserts the actor and
  writes the note with its event, whose `task` and `initiative` are those of the note and whose `data` is `{ref,
  summary, broadcast}` plus `task`, `initiative`, `resource` and `mentions` when set; the body is never in it.
- **`GET /api/v1/notes/{ref}`** reads one note, with its body.
- **`GET /api/v1/notes`** answers `{items, next_after?}`, items ordered by id (posting order). Filters combine with AND:
  `since` is a note ULID or `N<n>` (the notes after it), an RFC 3339 time (the notes at or after that instant) or `cursor`
  (the notes after the caller's `notes` cursor, from the start when none is set); `task`
  (ref or ULID) and `initiative` (slug or ULID) keep the notes on them, a task's notes belonging to its initiative,
  **404** for one that names nothing; `resource` is repeatable and keeps the notes on any of the resources;
  `broadcast=only` keeps the broadcasts and `broadcast=exclude` drops them; `mention` keeps the notes that mention one
  handle exactly and `author` those written by one handle. A filter that matches nothing is **200** with `items: []`.
  `fields` is `summary` (the default, also when empty: no `body`, but `has_body`) or `full` (with the bodies); anything
  else is **400** with a `next`, as is a `since` that is none of the four forms, an `ack` that is not a boolean, a `broadcast` that is neither value,
  and an `after` that is not a note ULID or `N<n>` ref. `limit` is 1 to 1000 (default 100; 0 is **400**). `after` is an
  `N<n>` ref or an ULID: the page starts after that note. `next_after`, the ULID of the last item, is present exactly
  when more notes follow. `ack=true` moves the caller's `notes` cursor to the last note of the page (the ULID of the last
  item, even when more pages follow); an empty page leaves the cursor where it was, and without `ack=true` a read
  moves nothing, `since=cursor` included.
- **`PUT /api/v1/cursors/{stream}`**, body `{position}`, sets the caller's cursor on the stream and answers **200**
  `{stream, position, ref}`: `position` is the ULID the cursor now holds and `ref` its `N<n>` or `E<n>`. `notes` takes a
  note ULID or `N<n>`, `events` an event ULID or `E<n>` (either letter case). A stream other than `notes` or `events` is
  **404** (`there is no stream tasks`); a missing body, or a position that names no note (or event) of that stream, a
  time or any other text is **400**, each with a `next`, and a refusal leaves the cursor as it was. Cursors belong to the
  calling actor, one per stream. `GET /api/v1/events?since=cursor` reads the events after the caller's `events` cursor
  (none set: from the start) and moves nothing; the events cursor is set only by this `PUT`. `since=cursor` or `ack=true`
  on a server without a cursor store is **503**.
- **The note document** (`Note`): `{id, ref, author, summary, body?, has_body, task?, initiative?, resource?, mentions,
  broadcast, created_at}`. `author` is the handle of the actor that posted it, `task` the task's ref, `initiative` its
  slug, `mentions` a list (empty when none), `broadcast` is derived (no task and no initiative) and absent optional
  fields are omitted. `has_body` is true when the note has a body, whether or not the document carries it; `body` is
  present only in `GET /notes/{ref}`, in a `fields=full` list and in the `201` answer, and only when the note has one.

### 6.8 Attention

`GET /api/v1/attention` is in `api/openapi.yaml` and answers what §5.3 derives. Any authenticated principal may use it
(v1 has no ACL) and a server without a store answers **503**. It first expires the leases that ran out (§5.1), which
writes their `lease_expired` events, and judges all five sections at that one instant.

- **Parameters.** `initiative` (a slug or ULID, repeatable: an item on any of them is kept) and `tag` (repeatable: the
  initiative carries every one given); with neither the whole board is read. An `initiative` that names nothing is
  **404** (`there is no initiative zz`), a `tag` that is not a valid tag is **400**, each with a `next` line. `fields`
  is `full` (the default, also when empty) or `counts`; anything else is **400** with a `next`.
- **The answer** is `{counts, open_asks, done_not_closed, expired_leases, overdue_watches, conflicts}`. Every section is
  a list, empty (`[]`) and never absent or null when nothing needs attention, so an empty board answers 0 in every
  count and five empty lists. `fields=counts` answers `{counts}` and no other member; its numbers are those the full
  answer would hold for the same filters.
- **Counts** (`AttentionCounts`) are `{open_asks, done_not_closed, expired_leases, overdue_watches, conflicts, total}`,
  `total` the sum of the five, all integers following the filters.
- **An actor** (`AttentionActor`) is `{handle, way_back?}`; `way_back` is absent when the board has none on record.
- **Open ask**: `{ref, question, task?, asker, mode, asker_waiting, blocking, created_at, age_seconds}`. `age_seconds` is
  the whole seconds since `created_at`; `task` is absent for an ask on no task; `asker_waiting` is false for a `stop`
  ask.
- **Done task**: `{task, title, done_by, done_at, last_lease?}` with `last_lease` `{lease, holder, outcome?, ended_at}`,
  absent when no lease on the task ended and without `outcome` when the holder wrote none.
- **Expired lease**: `{lease, holder, task, title, intent, renewed_at, expired_at}`.
- **Overdue watch**: `{task, title, owner, next_check_at, last_check?}` with `last_check` `{summary, by, at}`, absent
  when no check was ever recorded; `owner` is the actor that created the watch.
- **Conflict**: `{resource, holders, tasks}`, each holder `{lease, actor, task, intent, expires_at}` in lease-number
  order and `tasks` the distinct tasks of the holders in that order.
- **Clearing.** Each section empties by the action that deals with it: an ask by answering or withdrawing it, a done task
  by closing it, an expired lease by acknowledging it or by any later lease on its task, an overdue watch by recording a
  check (which moves `next_check_at`) and a conflict by ending (or letting expire) one of its leases.

---

## 7. Live feed and long-poll

### 7.1 SSE

`GET /api/v1/stream?topics=events,attention`: Server-Sent Events, authenticated like every other route (a key
or a session cookie; no credential is 401). `topics` is a comma-separated list of `events` and `attention`, both
by default; any other topic is **400** with a `next` line naming the two. The answer is 200 with
`Content-Type: text/event-stream`, `Cache-Control: no-cache` and `X-Accel-Buffering: no`, sent at once, and every
message is flushed as it is written. A server with no change feed answers 503.

- **`events`**: one message per event row, in the order they were written: `event: board`, `id:` the event's
  ULID (`events.id`) and `data:` the event as one JSON line, the object `GET /api/v1/events` lists. A stream
  starts at the change feed's position when it connects (§7.3): every event the feed delivers from then on is
  sent, and an event the feed had already delivered is not. So an event committed before the connection that
  the feed had not delivered yet (it reads every `BOARD_STREAM_POLL` and holds rows behind a gap, §7.3) is
  still sent, and nothing committed between a client's read of the board and its stream is lost.
  **Resume:** a `Last-Event-ID` header (the ULID of the last event the client has; an `EventSource` sends it by
  itself when it reconnects) or, when the header is absent or empty, a `?last_event_id=` parameter makes the
  stream send, before any live event, every event written after that one, in order of `n`, up to the feed
  position the stream began at; the live part starts after that position, so the replay and the live events
  neither leave a gap nor repeat one. An id may also be an `E<n>` ref. A client that read an event from a
  server whose feed is further ahead than this one gets no live event it already has. When **more than 1000**
  events would replay (1000 do replay), or the id names no event (unreadable, or no such event), the stream
  sends **one** `event: reset` instead, with no `id` line and `data: {"reason":"too_many"}` or
  `{"reason":"unknown_id"}`, replays nothing and continues live; the client then refetches what it shows.
  Resume applies to the `events` topic and is ignored when the stream did not ask for it.
- **`attention`**: one message, `event: attention` with no `id` line and `data:` the counts of §5.3 for the
  whole board (the `counts` object of `GET /attention`, §6.8: `open_asks`, `done_not_closed`, `expired_leases`,
  `overdue_watches`, `conflicts`, `total`) as one JSON line, sent **once on connect** and again **whenever they
  differ from the ones last sent on that stream**; a change that leaves them equal sends nothing. They are
  recomputed after every event the change feed delivers (§7.3), whichever topics the stream asked for, and at least
  every 30 s (every `BOARD_STREAM_HEARTBEAT` when that is shorter), because an overdue watch or an expiring lease
  changes with the clock and writes no event. A stream with only this topic receives no `board` message. The
  counts are read the way `GET /attention` reads them, so the leases that ran out expire first. A read that fails
  after the stream began is skipped and tried again at the next event or recheck; one that fails on connect is a
  500 before the headers.
- **Heartbeat**: a comment line, `: ping`, every `BOARD_STREAM_HEARTBEAT` (default `25s`, §13) while the stream
  is idle, so a proxy keeps it open.
- **Lifetime**: the stream outlives the server's write timeout, because it moves its own write deadline ahead
  (10 s) before every write; a client that stops reading ends it. It ends, releasing its goroutine and its
  subscription to the change feed (§7.3), when the client disconnects, when the feed stops, and when the server
  shuts down (`http.Server.Shutdown` does not end a handler by itself, so the stream watches the shutdown).
  `board serve` refuses a `BOARD_STREAM_HEARTBEAT` that is not a Go duration above zero (one stderr line,
  exit 1). The UI uses this stream; nothing in the UI polls.

### 7.2 Ask long-poll

`GET /api/v1/asks/{id}?wait=N` (`1 ≤ N ≤ 300`; any other value, including 0, 301 and text, is 400 with a `next`) returns
the ask at once if it is not `open`, otherwise holds the request until it is answered or withdrawn (200 with the new
state) or N seconds pass (200 with the still-open ask). A `wait` outside the range is refused before the ask is looked up; a valid one on an ask that does not exist is 404.

Waiters are woken by the events feed (§7.3), not by `LISTEN/NOTIFY`: a wait subscribes to the feed, then reads the
ask, and re-reads it when an `ask_answered` or `ask_withdrawn` event of that ask arrives. An answer written through any
replica on the same database therefore ends the wait within the feed's tail interval (`BOARD_STREAM_POLL`), and one
written through the same server ends it at once. A subscription the feed ended because the waiter fell behind starts
again and re-reads, and the wait re-reads the ask at every renewal below, so no change is lost. The write deadline is
moved past the wait, so a server write timeout shorter than N does not cut it, and a server shutdown ends the wait with
the ask as it is.

`poll_seen_at` (§4.5) is stored, not held in memory, so it is right with several replicas. While a wait is held the
server sets it to now at the start of the wait, at least every 30 seconds (`DefaultAskTouch`) while the wait is held,
and at its end (also when the client has left). A call that returns at once because the ask is not open writes nothing.
`asker_waiting` is derived from it: true when the ask is `open`, its mode is `poll` and `poll_seen_at` is within the last
60 seconds, so a poll-mode asker counts as waiting for a minute after its last wait ended, and a `stop`-mode ask never
does.

### 7.3 The change feed

The source of every live signal is the `events` table, tailed by `n` (the order of insertion) by the
change feed of `internal/feed/`, one per `board serve`. It reads the rows after the last one it delivered
every `BOARD_STREAM_POLL` (default `1s`, §13) and a write made through the same server wakes it at once,
so a local write reaches a subscriber without waiting for the interval. Each subscriber (an SSE stream,
§7.1; an ask waiter, §7.2) gets every event after the position its subscription began at, in order of `n`,
once. A subscriber costs a channel, not a goroutine, and a subscriber that falls too far behind is ended
so that it resynchronises (the stream sends `reset`) instead of holding the feed back.

This is what lets several replicas share one database: nothing is sent between servers, each tails the
table and so each sees every event, whichever server took the write. On Postgres, `n` is assigned when a
row is inserted but visible when its transaction commits, so a row with a lower `n` can appear after one
with a higher `n`. The feed never delivers past such a gap, and it never guesses from the clock whether
the missing row rolled back. Every transaction that appends an event first takes a shared transaction-level
advisory lock (`pg_advisory_xact_lock_shared`), before its `n` is drawn, and holds it until it commits or
rolls back. When a read shows a gap, the feed notes which transactions hold that lock (`pg_locks`). Every
missing `n` below the highest one read was drawn by one of them or by a transaction already ended. The
feed holds the later rows, however long that takes, until each noted transaction has ended. Then it reads
the table once more: a missing row that committed is delivered in its place, and one still missing rolled
back, so the feed moves past it. On SQLite there is one writer and no such gap. `board serve` refuses a
`BOARD_STREAM_POLL` that is not a Go duration above zero (one stderr line, exit 1). A
server started with `BOARD_MIGRATE=off` on a database it cannot read yet still serves: the feed starts at the
newest event as of its first successful read.

---

## 8. CLI

One binary, same Go module: `board serve` runs the server, every other subcommand is a client
(`board admin …` excepted, below). Static builds for linux/darwin × amd64/arm64.

Configuration: `BOARD_URL`, `BOARD_ACTOR` (handle; if unset, the CLI derives one with the
built-in fallback rule, §9), `BOARD_WAY_BACK`, `BOARD_API_KEY` (the environment's key, §3.3;
never accepted as a flag, so it does not land in shell history or process lists).
`board admin …` subcommands talk to the database directly (`BOARD_DB_URL`) and need no key.

`board serve` starts the HTTP server on `BOARD_LISTEN` (default `:8080`) and shuts down gracefully on
SIGINT or SIGTERM, exiting 0. `board version` prints one line: the version read from the Go build info
embedded in the binary (a tag or pseudo-version, or `devel+<short commit>` for a plain checkout build), with
`-dirty` appended when the build came from a modified tree. A subcommand the binary does not know, a missing
subcommand, or extra arguments to `serve` or `version` exit 2 and print exactly one usage line on stderr,
`usage: board <serve|version|help|whoami|init|task|lease|now|milestone|check|events|ask|answer|note|notes|attention|admin>`.

`board serve` opens the database named by `BOARD_DB_URL` (default `sqlite:///data/board.db`) and, unless
`BOARD_MIGRATE=off`, applies the embedded goose migrations before it listens; a migration failure exits 1.
`BOARD_MIGRATE=off` starts the server on the database as it is, migrated or not. `GET /healthz` answers 200
whenever the process is up, whatever the database state. `GET /readyz` answers 200 only when the database is
reachable and carries every embedded migration, and 503 otherwise (unmigrated, behind the embedded migrations,
or unreachable); it never writes. A value of `BOARD_MIGRATE` other than `auto` or `off` exits 1.

`board serve` refuses a database whose recorded goose version is newer than the highest migration the binary
embeds (a newer board migrated it): whatever `BOARD_MIGRATE` says, it exits 1 before it listens, printing exactly
one stderr line that names both versions (`board: database schema is newer than this binary: database is at
version 9999, this binary embeds up to 2: upgrade board`). `GET /readyz` also answers 503 for such a database.

`board admin migrate` opens `BOARD_DB_URL` (default `sqlite:///data/board.db`), applies every embedded migration
that is not applied yet, and prints `ok migrated, schema version <N>` on stdout (exit 0); running it again is a
no-op that prints the same line. It refuses a newer schema the same way `serve` does (one stderr line, exit 1),
and any other failure is one stderr line and exit 1. `board admin` with no subcommand, a subcommand other than
`migrate` and `keys`, or extra arguments to `migrate`, is the exit-2 usage error (`usage: board <serve|version|help|whoami|init|task|lease|now|milestone|check|events|ask|answer|note|notes|attention|admin>`).

`board admin keys` opens `BOARD_DB_URL` (default `sqlite:///data/board.db`), needs no server and no key, and
refuses a database that is not migrated (one stderr line ending `→ board admin migrate`, exit 1). A key name is
1 to 64 characters of `a-z A-Z 0-9 . _ -`, starting with a letter or digit.

- `board admin keys create <name> [--expires 90d]` makes a key and prints exactly one stdout line, with the
  key as its last field and nowhere else: `ok key <name> created brd_…`, or `ok key <name> created exp
  <YYYY-MM-DD> brd_…` with `--expires`. The key is printed this once and never stored. `--expires` takes a
  positive count of days (`90d`) or hours (`12h`), at most 36500 days in either unit (`36500d`, `876000h`), and the flag may come before or
  after the name; without it the key never expires. `created_by` is `admin-cli`. A name that already exists
  exits 1 with the single stderr line `board: key "<name>" already exists → board admin keys list`, and
  stores nothing.
- `board admin keys list` prints one line per key, oldest first, and never the key:
  `<name> brd_<display prefix> <active|expired|revoked> created <YYYY-MM-DD>`, followed by ` exp <date>`,
  ` used <date>` and ` revoked <date>` for the dates the key has. No keys prints nothing (exit 0).
- `board admin keys revoke <name>` sets `revoked_at` and prints `ok key <name> revoked`. Revoking a
  revoked key prints `ok key <name> already revoked` (exit 0) and keeps the first `revoked_at`. An unknown
  name exits 4 with `board: key "<name>" not found → board admin keys list`.
- A malformed command line (missing or extra arguments, an unknown flag) exits 2 with the single stderr line
  `usage: board admin keys create <name> [--expires 90d] | list | revoke <name>`. An invalid name or
  `--expires` value exits 2 with one `board: …` stderr line.

### 8.1 Output rules

The CLI is written for agents first: every line costs context.

- **Compact plain text by default**: one line per item, short refs (§8.2), relative times.
  `--json` gives the full objects (ULIDs included) for machines.
- **Every write command prints exactly one confirmation line**, starting with `ok`, naming the
  object by its short ref and the one fact that matters next:
  `ok lease L7 phone-onboard-3 exp 2h`, `ok task phone-onboard-4 created`,
  `ok ask A12 raised`, `ok note N40 → host:gw1`.
- **Every error is one line, on stderr, with the next action** after `→` when there is one:
  `lease L7 expired 14m ago → board lease start phone-onboard-3 "<intent>"`,
  `no BOARD_API_KEY → export BOARD_API_KEY (this environment's key)`,
  `task phone-onboard-9 not found → board task ls phone-onboard`.
  The server's problem documents carry a machine-readable `next` field the CLI renders.
- **List caps**: each list section in `board now`, `board attention`, `board notes` and
  `board catchup` shows at most **5** items, then `+K more → <command that shows them>`.
  `--limit N` changes the cap, `--all` removes it. Plain list commands (`board task ls`, …) cap
  at 20 by the same rule.

Exit codes:

| code | meaning | Phase 0 condition |
|---|---|---|
| 0 | success | a command did what was asked (a write printed its `ok` line, a list printed, possibly nothing) |
| 1 | error (server, network, unexpected) | the server cannot be reached, `BOARD_URL` is unset, the server refused the request (a bad slug or tag), a database or schema failure of `serve` or `admin` |
| 2 | usage error (bad flags or arguments) | an unknown subcommand or flag, a missing or extra argument, an invalid `--limit` or `--expires` value, one usage line on stderr |
| 3 | timed out waiting (`--poll`), the ask stays open | `board ask <task> "<question>" --poll D` when the deadline D passes with the ask still open (it prints `A<n> still open → board ask show A<n>`; the stop-mode `board ask` never waits) |
| 4 | not found (unknown ref) | `board init show` of an unknown slug, `board task show` of an unknown task, `board admin keys revoke` of an unknown name, `board ask show`, `board ask withdraw` and `board answer` of an unknown `A<n>`, `board note show` of an unknown `N<n>`, `board ask`, `board note` and `board notes` on an unknown task or initiative |
| 5 | authentication failed (missing, revoked or expired key) | `BOARD_API_KEY` is unset, or the server answers 401 to the key |
| 6 | state conflict (expired lease, task closed, dependency cycle) | `board init close` of an initiative with open or done tasks and no `--force`, of a closed one, and `board init reopen` of an open one; `board task add` in a closed initiative and `board task edit` of a closed task; `board ask` on a closed task and `board answer` or `board ask withdraw` of an ask that is not open; the rest arrives with leases and the other task commands (a taken initiative slug is a plain error, exit 1) |
| 7 | succeeded **with a conflict warning** (`board start`, `board lease start/renew`, `board now --resources`): the action happened, live leases overlap | `board lease start` and `board lease renew` when a live lease of another holds a resource of the lease; `board now --resources` and `board now` when a conflict touches one of my live leases or a resource given; `board start` arrives later |

`features/cli_output.feature` holds one scenario per Phase 0 code 0, 1, 2, 4 and 5, and two list scenarios: 20
items print without a `more` line, 21 print 20 plus `+1 more → board init ls --all`.

The CLI words the next action itself: the `→ <next>` of a client error is a command (`→ board init ls` for an
unknown initiative, `→ board init show <slug>` for a taken slug, `→ check BOARD_API_KEY (board admin keys list
shows which keys are active)` for a refused key), not the server's HTTP-flavoured `next`, which stays in the
problem document for other clients. A status the CLI has no command for prints the server's own `next`.

### 8.2 Short refs

Every object has a short, unique ref that is typed back into any command; ULIDs stay internal
and appear only in `--json`.

| object | ref | example |
|---|---|---|
| initiative | its slug | `phone-onboard` |
| task | `<initiative-slug>-<n>` | `phone-onboard-3` |
| lease | `L<n>` | `L7` |
| ask | `A<n>` | `A12` |
| note | `N<n>` | `N40` |
| event | `E<n>` | `E1043` |
| API key | its name | `workstation` |
| actor | its handle | `deck:personal/vpn-debug` |

`<n>` is a per-table integer identity column (§4), assigned on insert, never reused. Refs are
case-insensitive on input. The API accepts refs wherever it accepts IDs (§6.1).

### 8.3 Commands

Shipped so far: `version`, `serve`, `admin migrate`, `admin keys`, `whoami`, `init add|ls|show|edit|close|reopen` and `task add|ls|show|edit|done|reopen|close|dep add|dep rm|events`, `lease start|renew|end|ls`, `now`, `milestone`, `check`, `events`, `ask`, `ask show|ls|withdraw`, `answer`, `note`, `note show` and `notes`; the other lines
below arrive with their phases. The client commands (`whoami`, `init …`, `task …`, `lease …`, `now`, `milestone`, `check`, `events`, `ask …`, `answer`, `note …`, `notes`) call the server at `BOARD_URL` with
`Authorization: Bearer $BOARD_API_KEY`, `Board-Actor: $BOARD_ACTOR` and, when there is one, `Board-Actor-Way-Back:
$BOARD_WAY_BACK`. The key is read from the environment only: no command has a flag that takes a key (a unit test
walks every registered flag and fails on a key-like name). With `BOARD_ACTOR` unset the actor is `agent:<hostname>:<ppid>`
and, unless `BOARD_WAY_BACK` is set, the way back is `pid <ppid> on <hostname>, cwd <cwd>` (§9). With
no `BOARD_API_KEY` a client command exits 5 with `no BOARD_API_KEY → export BOARD_API_KEY (this environment's
key)`, with no `BOARD_URL` it exits 1 with `no BOARD_URL → export BOARD_URL (the address of the board server)`, and
an unreachable server exits 1 with `server unreachable at <url> → check BOARD_URL and that board serve is running`.
A server problem document is printed as one line, `<detail> → <next>`, with the server's `next` except where the
CLI names its own: a 401 always ends `→ check BOARD_API_KEY (board admin keys list shows which keys are active)`, a
taken slug on `init add` ends `→ board init show <slug>` and an unknown slug on `init show` ends `→ board init ls`.
It exits by its status: 401 is 5, 404 is 4,
anything else is 1, except a 409 of `init close`, `init reopen`, `init edit`, `task add` (a closed initiative), `task edit` (a closed task or initiative) and `task done|reopen|close` (a status that does not allow the move) and `task dep add` (a cycle) and `check` (a task that is not an open watch task) and `lease start` (a closed task) and `lease renew` (an expired or ended lease) and `lease end` (a lease that is not live) and `ask` (a closed task) and `answer` and `ask withdraw` (an ask that is not open), which is 6, and except that `lease start`, `lease renew` and `now` exit 7 when they print a conflict (a taken slug on `init add` is a plain 409 and exits 1). The flags may come before or after the positional arguments, as `--flag value` or
`--flag=value`; an unknown flag or a wrong argument count exits 2 with the command's usage line on stderr.

- `board init add <slug> "<title>" [--tag t]... [--json]` creates the initiative and prints `ok initiative <slug>
  created` (`--json`: the created object instead). A taken slug, a bad slug and a bad tag each exit 1 with the server's
  message and, for a taken slug, `→ board init show <slug>`.
- `board init ls [--tag t]... [--limit N|--all] [--json]` prints one line per initiative in creation order,
  `<slug>  <status>  <title>` followed by `  [tag,tag]` when it has tags, and nothing when there are none. A tag
  given more than once keeps the initiatives that carry every tag. It shows 20 by default, `--limit N` (N ≥ 1)
  changes the cap and `--all` removes it (both together are a usage error); when more exist the last line is
  `+K more → board init ls … --all`. `--json` prints the shown initiatives as one JSON array. `--status
  open|closed|all` (default `open`) picks the initiatives by status and `--closed-within N` (N days, or `Nd`) keeps
  the ones closed in the last N days; it needs `--status closed` or `all` (with `all` the open ones stay). A status
  other than the three, a window that is not a positive number of days and a window without `--status closed|all`
  exit 2. The `+K more` line repeats `--tag`, `--status` and `--closed-within`.
- `board init show <slug> [--json]` prints the one-line form, then `targets: <start> → <end>` when it has either,
  `created by <handle> <relative time>`, `closed by <handle> <relative time>` when closed, and the description. An
  unknown slug exits 4 with `<detail> → board init ls`, whatever `next` the server sent.
- `board init edit <slug> [--title T] [--description file|-] [--tag t]... [--no-tags] [--start D] [--end D]
  [--clear-start] [--clear-end] [--json]` sends only the fields it is given (`PATCH /api/v1/initiatives/<slug>`) and
  prints `ok initiative <slug> updated` (`--json`: the object). `--description` reads the text from the named file,
  or from standard input for `-`; `--tag` (repeatable) replaces the tag set and `--no-tags` empties it; `--start` and
  `--end` take `YYYY-MM-DD` and `--clear-start` and `--clear-end` remove the date. No field to change, `--tag` with
  `--no-tags`, and a date with the flag that clears it are exit 2. A value the server refuses (a date that does not
  exist, a bad tag) exits 1 with its message; an unknown slug exits 4 with `<detail> → board init ls`; an
  unreadable description file exits 1.
- `board init close <slug> [--force] [--json]` prints `ok initiative <slug> closed`. With an open or done task left
  and no `--force` it exits 6 with `<detail> → board init close <slug> --force`; with `--force` every task not
  closed is dropped first. Closing a closed initiative exits 6 with `<detail> → board init reopen <slug>`. An
  unknown slug exits 4 with `→ board init ls`.
- `board init reopen <slug> [--json]` prints `ok initiative <slug> reopened`; reopening an open initiative exits 6
  with `<detail> → board init show <slug>`, an unknown slug exits 4 with `→ board init ls`.
- `board task add <initiative> "<title>" [--description file|-] [--kind work|watch] [--cadence D] [--after <ref>]...
  [--start T] [--end T] [--link label=url]... [--json]` creates the task and prints `ok task <ref> created`
  (`--json`: the created object). `--after` (repeatable) names the tasks it starts out blocked by, `--link` (repeatable)
  an external link, `--description` is read as for `init edit`. A watch task needs `--cadence`; a work task refuses it
  (exit 1 with the server's message, as for any value the server refuses). An unknown initiative exits 4 with
  `<detail> → board init ls`, an unknown `--after` task exits 4 with `→ board task ls <initiative>`, a closed initiative
  exits 6 with `→ board init show <initiative>`.
- `board task ls [<initiative>] [--status open|done|closed|not_closed|all] [--kind work|watch] [--active] [--blocked]
  [--overdue] [--q text] [--limit N|--all] [--json]` prints one line per task in creation order, `<ref>  <status>  <title>`
  followed by `  (watch, blocked, active, overdue)` for the marks it has, and nothing when there are none. The status is
  `not_closed` by default; `--active`, `--blocked` and `--overdue` keep the tasks that have a live lease, an open
  dependency or a missed check; `--q` keeps the tasks whose title or description contains the text, in either case.
  It shows 20 by default; the last line is `+K more → board task ls … --all` repeating the initiative and every filter.
  `--json` prints the shown tasks as one JSON array. An unknown initiative exits 4 with `→ board init ls`; a status or
  kind the server does not know, a `--limit` below 1 and `--all` with `--limit` exit 2.
- `board task show <ref> [--json]` prints the one-line form, then `initiative:`, `planned: <start> → <end>` when it has
  either, `cadence: <D>, next check <relative time>` for a watch task, a `link: <label> <url>` line per link,
  `created by`, `done by` and `closed (<resolution>) by` lines, `blocked by <ref> (<status>) <title>` per open or
  closed dependency, `blocking <ref> (<status>) <title>` per task it blocks, `lease L<n> by <handle>: <intent> (expires in
  …)` per live lease, the description, then `events:` and its last 10 events as `  E<n>  <relative time>  <handle>
  <summary>`. The ref is case-insensitive. An unknown task exits 4 with `<detail> → board task ls`.
- `board task edit <ref> [--title T] [--description file|-] [--start T] [--end T] [--cadence D] [--link label=url]...
  [--no-links] [--json]` sends only the fields given (`PATCH /api/v1/tasks/<ref>`) and prints `ok task <ref> updated`
  (`--json`: the object). `--link` (repeatable) replaces the links and `--no-links` empties them. No field to change and
  `--link` with `--no-links` are exit 2 with the usage line. An unknown task exits 4 with `→ board task ls`; a closed task
  or a task of a closed initiative exits 6 with `→ board task show <ref>`.
- `board task done <ref> [--json]`, `board task reopen <ref> [--json]` and `board task close <ref> [--resolution
  completed|dropped|superseded|duplicate] [--for operator] [--json]` call `POST /api/v1/tasks/<ref>/done|reopen|close` and
  print `ok task <ref> done`, `ok task <ref> reopened` and `ok task <ref> closed` (`--json`: the task after the move).
  `close` sends the resolution, `completed` when `--resolution` is not given. `--for operator` sends
  `Board-On-Behalf-Of: operator`, which the server stores as the `on_behalf_of` of the `task_closed` event; an agent
  uses it only when the operator told it to close. A resolution outside the four and a `--for` other than `operator` are
  exit 2 with `board: --resolution: invalid resolution …` or `board: --for: invalid actor …`, before any request is
  sent; `--resolution` and `--for` belong to `close` alone. A move the task's status does not allow (done on a done or
  closed task, reopen on an open task or in a closed initiative, close on a closed task) exits 6 with `<detail> → board
  task show <ref>`; an unknown task exits 4 with `→ board task ls`. Live leases on the task stay live.
- `board task dep add <ref> <blocked-by-ref> [--json]` and `board task dep rm <ref> <blocked-by-ref> [--json]` call
  `POST` and `DELETE /api/v1/tasks/<ref>/deps/<blocked-by-ref>` (the refs in either letter case, the two tasks possibly in
  different initiatives) and print `ok task <ref> blocked by <blocked-by-ref>` and `ok task <ref> no longer blocked by
  <blocked-by-ref>` (`--json`: the dependent task). Adding a dependency that exists prints the same line. A dependency that
  would close a cycle exits 6 with the server's path (`dependency would close a cycle: aa-1 is blocked by aa-2 is blocked by
  aa-1`) and `→ board task show <ref>`; removing a dependency that does not exist exits 4 with `→ board task show <ref>`, an
  unknown task exits 4 with `→ board task ls`, and a task blocked by itself exits 1. A wrong argument count or an unknown
  subcommand is exit 2 with the usage line.
- `board lease start <task> "<intent>" [--ttl D] [--res r]... [--json]` calls `POST /api/v1/leases` (the task as a ref in either
  letter case; `--ttl` a duration such as `90m` or `4h`, clamped by the server to 60 s to 12 h, 2 h without it; `--res` repeatable)
  and prints `ok lease L<n> <task> exp <ttl>` (`exp 2h`, `exp 1h30m`: the time from the lease's last renewal to its expiry),
  then one line per conflict (below). A start on a task where I already hold a live lease renews that lease and prints the same
  line. An unknown task exits 4 with `→ board task ls`, a closed task exits 6 with `<detail> → board task show <task>`, a bad
  `--ttl` is exit 2 with `board: --ttl: invalid duration …` before any request is sent. `--json` prints the server's
  `{lease, conflicts}` object.
- `board lease renew [<L…>|<task>] [--ttl D] [--intent text] [--json]` calls `POST /api/v1/leases/<ref>/renew` and prints
  `ok lease L<n> <task> exp <ttl>` and the conflict lines, once for the lease it names. The argument is a lease ref (`L7`, either
  letter case) or a task ref, which means my latest lease on that task. With **no argument it renews every live lease of mine**
  (listed from `GET /api/v1/leases?state=live&actor=<me>`), one `ok` line each in lease order, and `--json` then prints an array
  of `{lease, conflicts}`. `--ttl` and `--intent` are sent only when given. A lease that ran out exits 6 with
  `lease L7 expired 14m ago → board lease start <task> "<intent>"` (the task is the lease's own; `<intent>` stays the
  placeholder), a lease that ended exits 6 with the server's detail and the same start, an unknown lease exits 4 with `→ board
  lease ls`, an unknown task with `→ board task ls`, and no lease of mine on the task or no live lease with no argument exits 4
  (`you hold no live lease → board lease ls`).
- `board lease end [<L…>|<task>] ["<outcome>"] [--json]` calls `POST /api/v1/leases/<ref>/end` and prints `ok lease L<n> ended`
  (`--json`: the lease). The target is chosen as for `renew`; with no target it is my only live lease, and several live leases
  are exit 2 (`you hold 2 live leases: L1, L2 → board lease end <L…>`). A single word is the target when it looks like a lease
  ref (`L7`) or a task ref (`aa-1`) and the outcome otherwise, so `board lease end "shipped it"` ends my only lease with that
  outcome; two words are the target and the outcome. A lease that is not live exits 6 with `→ board lease ls --mine --state
  all`, an unknown lease exits 4 with `→ board lease ls`.
- `board lease ls [--state live|ended|expired|all] [--mine] [--task ref] [--res r] [--limit N|--all] [--json]` calls `GET
  /api/v1/leases` and prints one line per lease, oldest first, `L<n>  <state>  <task>  <holder>  "<intent>"  res a,b  exp
  41m` (the `res` part only when it holds resources; `expired 14m ago` or `ended 3m ago` in place of `exp` for the other
  states). The state is `live` by default, `--mine` keeps the leases I hold, `--task` and `--res` filter by task and resource;
  the cap is 20 as for the other lists with a `+K more → board lease ls … --all` line, and an unknown task exits 4 with `→ board
  task ls`. A `--state` outside the four is exit 2.
- **Conflicts.** `lease start` and `lease renew` print each conflict the server answers on its own line after the `ok` line,
  `CONFLICT <shared resources>: L<n> <holder> (<task>) "<intent>" exp <time left>` (resources joined by `,`, one line for
  each conflicting lease, in lease order), and **exit 7** when there is any, 0 when there is none. The action has happened
  either way; `--json` prints no `CONFLICT` line (the `conflicts` array carries them) and still exits 7. A no-argument renewal
  counts the conflicts of every lease it renewed.
- `board now [--resources r1,r2] [--json]` calls `GET /api/v1/now` (`--resources` is sent as the `resources` query, repeated
  flags joined by `,`) and prints the check-in: `you: <handle>`, then five sections in this order, each a heading line and one
  line per item indented by two spaces, then the `attention` line. `my leases:` lists my live leases as `L<n>  <task>
  <title>  "<intent>"  res a,b  exp 1h52m`, `others:` lists the live leases of other actors as `L<n>  <task>  <holder>  "<intent>"
  res a,b  exp 41m` and `conflicts:` lists the conflicts as the `CONFLICT` lines of `lease start` (the live leases that hold a
  resource of one of my live leases, and those of others that hold a resource given with `--resources`). `my asks:` lists
  `my_asks` of the answer, the asks I raised that are open and those answered since my `notes` cursor or in the last 24 hours, in
  the line form of `board ask ls` (`A<n>  open  <task>  <asker>  "<question>"  asked 3m ago`); an answered ask starts with
  `ANSWERED ` and ends with the answer, on one line (white space collapsed) and quoted: `ANSWERED A2  answered  aa-2  me  "Ship
  it?"  answered by nik@phone 1m ago: "yes, ship"`. `notes:` lists the notes of the answer, the unread ones that concern me, in the
  line form of `board notes` (`N<n>  3m ago  <author>  <task|initiative|broadcast>  [<resource>]  <summary>`, ending `  +` when the
  note has a body). Reading `now` moves no cursor; `board notes --since last --ack` does. The `res` part is absent for a lease
  without resources, and an empty section is its heading and `none` on one line (`others: none`, `my asks: none`, `notes: none`).
  Each section shows at most 5 lines (§8.1), then `  +K more → <command>`: `board lease ls --mine` for my leases, `board lease
  ls` for the others and the conflicts, `board ask ls --mine` for my asks, and for the notes `board notes --since last`, where `K`
  also counts the `notes_more` notes the server left out; `now` has no `--limit` or `--all`. The last line is always
  `attention: asks 2 · done 3 · expired 1 · overdue 0 · conflicts 1`, the counts of the five sections of `board attention` for the
  whole board (all zeros when nothing needs the operator). **Exit 7** when the answer holds any conflict, else 0; the output is
  printed either way, and `--json` prints the server's answer as it came (all items, no cap) and exits 7 the same. A resource list
  the server refuses (an empty entry, over 20 resources) exits 1 with its message; extra arguments or an unknown flag exit 2 with
  `usage: board now [--resources r1,r2] [--json]`.
- `board ask <task> "<question>" [--details file|-] [--option a]... [--stop] [--blocking] [--json]` calls `POST /api/v1/asks` (the task
  as a ref in either letter case, `--details` the text of a file or, with `-`, of standard input, `--option` repeatable) and prints
  `ok ask A<n> raised` (`--json`: the ask). The ask is a `stop` ask: `--stop`, the default, is accepted for clarity, and the ask is
  `blocking` only with `--blocking` (its task is then `blocked` while the ask is open). A `--details` file that cannot be read exits 1
  with `cannot read the details file <path>`, before any request. An unknown task exits 4 with `→ board task ls`, a closed task exits
  6 with `<detail> → board task show <task>`, a question or option the server refuses exits 1 with its message. A first word of
  `show`, `ls` or `withdraw` is the subcommand and any other starts a new ask; flags may precede the task.
- `board ask <task> "<question>" … --poll D [--json]` (`D` a whole number and a unit `s`, `m`, `h` or `d`; `--poll` with `--stop`, or a
  bad `D`, is exit 2 with the usage line) raises the ask as a `poll` ask, prints nothing yet, then waits for its end with repeated
  `GET /api/v1/asks/<ref>?wait=N` calls (§7.2) until `D` after the raise. Each call asks for at most 300 s (the server's cap) and
  never for more than the time left to the deadline, so the last wait is the remainder; no call is made once less than a whole
  second is left or the deadline has passed. The client's request timeout for a call is its wait plus 30 s. The end is one stdout
  line (`--json`: the last ask read, same exit codes): an answer is `ok A<n> answered by <handle>: <answer>` and exits 0, a
  withdrawal is `A<n> withdrawn` with `: <reason>` when one was given and exits 6, and the deadline is
  `A<n> still open → board ask show A<n>` and exits **3**, the ask staying open (its `asker_waiting` stays true for 60 s after the
  last wait, §7.2). Refusals of the raise are those of the stop-mode ask (4 for an unknown task, 6 for a closed one).
- `board ask show <A…> [--json]` calls `GET /api/v1/asks/<ref>` and prints the one-line form `A<n>  <state>  <task>  <asker>
  "<question>"  (blocking, poll)  <when>` (`-` for an ask on no task; the marks only when they apply; `asked 3m ago`, `answered by
  <handle> 3m ago` or `withdrawn 3m ago`), then `initiative: <slug>` when it has one, one `option: <text>` line per option, the
  details, and `answer (<channel>): <text>` (`answer (relay for <handle>): …` for a relayed one) or `reason: <text>` for a
  withdrawal that gave one. An unknown ask exits 4 with `→ board ask ls`.
- `board ask ls [--state open|answered|withdrawn|all] [--mine] [--task ref] [--limit N|--all] [--json]` calls `GET /api/v1/asks`
  and prints one line per ask in raising order, in the one-line form of `ask show`. The state is `open` by default, `--mine` keeps
  the asks I raised, `--task` those on one task; the cap is 20 as for the other lists with a `+K more → board ask ls … --all` line.
  An unknown task exits 4 with `→ board task ls`; a `--state` outside the four is exit 2.
- `board ask withdraw <A…> ["<reason>"] [--json]` calls `POST /api/v1/asks/<ref>/withdraw` and prints `ok ask A<n> withdrawn`
  (`--json`: the ask). An ask that is not open exits 6 with `<detail> → board ask show A<n>`; an unknown ask exits 4 with `→ board
  ask ls`.
- `board answer <A…> "<answer>" [--relay] [--for <handle>] [--json]` calls `POST /api/v1/asks/<ref>/answer` and prints `ok ask A<n>
  answered` (`--json`: the ask). The channel is `api`; with `--relay` it is `relay`, and `--for` names the person the relayed answer
  came from (`on_behalf_of`): `--for` without `--relay` is exit 2. An ask that is not open exits 6 with `<detail> → board ask show
  A<n>`; an unknown ask exits 4 with `→ board ask ls`.
- `board note "<summary>" [--body file|-] [--task ref] [--init slug] [--res r] [--mention h]... [--json]` calls `POST /api/v1/notes`
  and prints `ok note N<n> posted` (`--json`: the note). `--body` is the text of a file or, with `-`, of standard input (a file that
  cannot be read exits 1 with `cannot read the body file <path>`, before any request); `--task`, `--init` and `--res` each take one value;
  `--mention` is repeatable. A note with neither `--task` nor `--init` is a broadcast, with or without `--res` (§4.7). An unknown task exits
  4 with `→ board task ls`, an unknown initiative exits 4 with `→ board init ls`, and a summary or body the server refuses exits 1 with its
  message. A first word of `show` is the subcommand and any other starts a new note.
- `board notes [--since last|<time>|<N…>] [--task ref] [--init slug] [--broadcast] [--res r]... [--mention h] [--ack] [--limit N|--all] [--json]`
  calls `GET /api/v1/notes` and prints one line per matching note, oldest first:
  `N<n>  <when>  <author>  <scope>  [<resource>]  <summary>`, where `<when>` is `3m ago`, `<scope>` is the note's task ref, else its
  initiative slug, else `broadcast`, the resource appears in brackets only when the note has one, and the line ends `  +` when the note has a
  body. `--since last` reads the notes after the caller's `notes` cursor (from the start when none is set), `--since N<n>` (either letter case)
  those after that note, and `--since` with an RFC 3339 time or a `YYYY-MM-DD` date those at or after it. The filters combine: `--task` and
  `--init` keep the notes on them, `--broadcast` only the broadcasts, `--res` (repeatable) the notes on any of the resources and `--mention` the
  notes that mention one handle. The cap is **5** (§8.1), changed by `--limit N` (N ≥ 1) and removed by `--all` (both together are exit 2), and
  when more match the last line is `+K more → board notes … --all` repeating every filter. `--ack` moves the caller's `notes` cursor (`PUT
  /api/v1/cursors/notes`) to the last note **shown**, so the notes left out by the cap stay unread and `board notes --since last --ack` again
  shows the next ones; nothing moves when no note matched. `--json` prints the shown notes as one JSON array (without bodies, with
  `has_body`). An unknown task exits 4 with `→ board task ls`, an unknown initiative with `→ board init ls`.
- `board note show <N…> [--json]` calls `GET /api/v1/notes/<ref>` (either letter case) and prints the one-line form of `board notes`, then
  `initiative: <slug>` when the note is on a task, `mentions: a, b` when it has any, then the body (nothing more when it has none).
  `--json` prints the note with its body. An unknown note exits 4 with `<detail> → board notes`.
- `board milestone <task> "<summary>" [--json]` calls `POST /api/v1/tasks/<task>/milestones` and prints `ok task <ref>
  milestone recorded` (`--json`: the task). It works on a task of any kind and status. An unknown task exits 4 with
  `→ board task ls`; a summary the server refuses (empty, over 200 characters) exits 1 with its message.
- `board check <task> "<summary>" [--next D] [--json]` calls `POST /api/v1/tasks/<task>/checks` and prints `ok task <ref>
  checked` (`--json`: the task). `--next` takes a duration (`24h`) and sends `next_check_at` as that long from now; without
  it the server moves the next check on by the task's cadence. A task that is not a watch task, or is closed, exits 6 with
  `<detail> → board task show <ref>`; an unknown task exits 4 with `→ board task ls`; a bad `--next` is exit 2 with `board:
  --next: invalid duration …`, before any request is sent.
- `board task events <ref> [--limit N|--all] [--json]` and `board events [--since S] [--task ref] [--initiative slug]
  [--type t]... [--actor handle] [--limit N|--all] [--json]` call `GET /api/v1/tasks/<ref>/events` and `GET /api/v1/events`
  and print one line per event, oldest first: `E<n>  <relative time>  <handle>  <summary>` (the line `task show` prints
  under `events:`), and nothing when there are none. `--since` takes an event ref (`E42`, either letter case), an event id,
  an RFC 3339 time or a `YYYY-MM-DD` date (midnight UTC), and keeps the events after that event or from that time on;
  `--type` is repeatable and keeps the events of any of the types given; the filters combine. Both list 20 by default as the
  other list commands do: `--limit N` changes the cap, `--all` removes it, and when more exist the last line is `+K more →
  board events … --all` (or `board task events <ref> --all`) repeating every filter. `--json` prints the shown events as one
  JSON array. An unknown initiative exits 4 with `→ board init ls`, an unknown task with `→ board task ls`; a `--since` the
  server cannot read exits 1 with its message.
- Durations (`--cadence`) are a whole number and a unit, `s`, `m`, `h` or `d` (`90s`, `45m`, `2h`, `7d`); a missing unit, a
  sign, a fraction or zero is refused. Times (`--start`, `--end`) are RFC 3339 or `YYYY-MM-DD` (midnight UTC). A malformed
  duration, time, link (`label=url`) or kind is exit 2 with the single line `board: <flag>: invalid <what> …`, before any
  request is sent.
- `board whoami` prints `you: <handle> (key <name>)  way back: <way back>  server ok` and exits 0. The key name is
  the one of the key whose display prefix matches `BOARD_API_KEY` in `GET /api/v1/keys`; the call also proves the
  server answers and accepts the key. A revoked or unknown key exits 5, an unreachable server exits 1.
- The usage line for an unknown subcommand is `usage: board <serve|version|help|whoami|init|task|lease|now|milestone|check|events|ask|answer|note|notes|attention|admin>`.

```
board version | update [--check] [--version vX.Y.Z]
board help agent [--version]            # the agent protocol cheat sheet (§8.6)
board whoami                            # handle, way back, key name, server reachability
board now [--resources r1,r2]           # check-in: my leases, others' live leases, conflicts, my asks, new notes
board attention [--tag t]... [--init slug] [--limit N|--all]   # the five §5.3 sections, one line per item
board init add <slug> "<title>" [--tag t]... | ls [--tag t]... [--status open|closed|all] [--closed-within N] [--limit N|--all] | show <slug>
                | edit <slug> [--title T] [--description file|-] [--tag t]... [--no-tags] [--start D] [--end D] [--clear-start] [--clear-end]
                | close <slug> [--force] | reopen <slug>
board task add <initiative> "<title>" [--description file|-] [--kind watch --cadence 24h] [--after <ref>]... [--start T] [--end T] [--link label=url]...
board task ls [<initiative>] [--status …] [--kind k] [--active] [--blocked] [--overdue] [--q text] [--limit N|--all] | show <ref>
                | edit <ref> [--title T] [--description file|-] [--start T] [--end T] [--cadence D] [--link label=url]... [--no-links]
board task done|reopen <ref> | close <ref> [--resolution dropped] [--for operator]
board lease start <task> "<intent>" [--ttl 2h] [--res host:x --res repo:y]
board lease renew [<L…>|<task>] [--ttl 4h] [--intent …]      # no argument: all my live leases
board lease end   [<L…>|<task>] ["<outcome>"]
board ask <task> "<question>" [--details file|-] [--option a --option b] [--poll 10m | --stop] [--blocking]
board answer <A…> "<answer>" [--relay] [--for <handle>]
board ask show <A…> | ls [--state open|answered|withdrawn|all] [--mine] [--task <ref>] | withdraw <A…> ["<reason>"]
board note "<summary>" [--body file|-] [--task <ref>] [--init <slug>] [--res r] [--mention h]...
board notes [--since last|<time>|<N…>] [--task <ref>] [--init <slug>] [--broadcast] [--res r]... [--mention h] [--ack] [--limit N|--all]
board note show <N…>
board milestone <task> "<summary>"
board check <task> "<summary>" [--next 24h]
board admin keys create <name> [--expires 90d] | list | revoke <name>   # direct DB, no HTTP
board admin migrate | backup <dest>
```

`board ask --poll D` prints the answer (exit 0), the withdrawal (exit 6), or exits 3 on timeout with the ask still open.

`board attention [--tag t]... [--init slug] [--limit N|--all] [--json]` reads `GET /api/v1/attention` (§6.8) and prints the five
sections of §5.3 in this order, each a heading `<name>: <count>` (a section with nothing in it is just `<name>: 0`) and one
indented line per item, newest relevant first, with each actor shown as `handle` or `handle (way back)`:

```
open asks: 1
  A12  3m ago  phone-onboard-3  claude:ws1:3f2a  "Which database?"  poll  waiting  blocking
done, not closed: 1
  homelab-7  Rotate the prefix  done by deck:personal/vpn-debug 2h ago  L5 "prefix rotated"
expired leases: 1
  L3  trip-12  ralph:trip-m6 (tmux attach -t trip)  "attachments e2e"  renewed 3h ago
overdue watches: 1
  trip-4  poll the build  nik@phone  due 2d ago  last check "build is red" by codex@laptop 2d ago
conflicts: 1
  CONFLICT host:gw1: L7 claude:ws1:3f2a phone-onboard-3, L5 deck:personal/vpn-debug homelab-7
```

The ask line is `A<n>  <age>  <task, or board for an ask on no task>  <asker>  "<question>"` followed by the words `poll`
(a poll ask), `waiting` (its asker is polling) and `blocking` that apply. The done line is `<task>  <title>  done by <actor> <age>`
and, when a lease on the task ended, `  L<n> "<outcome>"` (`L<n> ended` when the holder wrote none). The expired line is
`L<n>  <task>  <holder>  "<intent>"  renewed <age>`. The watch line is `<task>  <title>  <owner>  due <age>` and, when a check
was ever recorded, `  last check "<summary>" by <actor> <age>`. The conflict line is `CONFLICT <resource>: ` and each holder as
`L<n> <actor> <task>`, comma separated. `--tag` (repeated: the initiative carries every tag) and `--init` narrow it as the API's
`tag` and `initiative` do; each section shows at most 5 lines, then `  +K more → board attention <same filters> --all`, and
`--limit N` or `--all` change that cap. `--json` prints the API document, every item and no cap. An unknown `--init` exits 4
with `→ board init ls`; a malformed command line (an argument, an unknown flag, `--limit 0`) exits 2 with the usage line
`usage: board attention [--tag t]... [--init slug] [--limit N|--all] [--json]`.

### 8.4 Shortcuts

The most frequent sequences are single commands. Where a shortcut would otherwise need several
calls, the server has a composite endpoint doing it in **one round trip and one transaction**.

| shortcut | does | primitives | endpoint |
|---|---|---|---|
| `board start <task> "<intent>" [--res r]… [--ttl D]` | check in and start (or renew) a lease; prints the lease line, conflicts, and the `now` summary | `whoami` + `now --resources` + `lease start` | `POST /api/v1/start` |
| `board finish [<task>] "<outcome>" [--done]` | end my lease on the task (my only live lease if omitted), write the outcome as a note on the task, optionally mark done | `lease end` + `note --task` + `task done` | `POST /api/v1/finish` |
| `board new <initiative> "<title>" --start "<intent>" [--res r]…` | create a task and start a lease on it | `task add` + `start` | `POST /api/v1/start` with `new_task` |
| `board ping ["<progress>"]` | renew all my live leases; with text, also a milestone on each leased task | `lease renew` (all) + `milestone` | `POST /api/v1/ping` |
| `board block <task> "<question>" [--poll D \| --stop]` | raise an ask on the task; `--poll` keeps the lease and waits (renewing it while waiting); `--stop` ends the lease with outcome "blocked on A…" | `ask` + `lease end` (stop) | `POST /api/v1/asks` with `lease_action` |
| `board catchup` | notes since my cursor (acknowledged), plus answers to my asks since then | `notes --since last --ack` + `asks --mine --answered-since` | `GET /api/v1/catchup` |

Shortcuts print the same `ok …` lines as the primitives they replace (one per effect, so
`board finish --done` prints two lines) and follow the same exit codes.

Example `board now` output:

```
you: claude:ws1:3f2a (key workstation)  leases: L7 phone-onboard-3 exp 1h52m
live: L5 deck:personal/vpn-debug  homelab-7 "rotate wstunnel prefix" res host:gw1 exp 41m
      L3 ralph:trip-m6            trip-12   "attachments e2e"         res repo:trip exp 3h10m
CONFLICT host:gw1: L7 (you, phone-onboard-3) and L5 deck:personal/vpn-debug (homelab-7)
asks: A12 answered: "use the rack AP"
notes since 10:42: N40 [broadcast] ralph:trip-m6 "registry slow, builds may time out"
      N41 [host:gw1] deck:personal/vpn-debug "gw1 reboot at 11:30"
      +3 more → board notes --since last
attention: asks 2 · done 3 · expired 1 · overdue 0 · conflicts 1
```

### 8.5 Installing the CLI

**Script** (mirrors deck's `install.sh`):

```sh
curl -fsSL https://raw.githubusercontent.com/<owner>/board/main/install.sh | sh
```

- Detects OS (linux, darwin) and arch (amd64, arm64), downloads `board_<os>_<arch>.tar.gz` and
  `checksums.txt` from the GitHub release over HTTPS only, verifies SHA-256, installs to
  `~/.local/bin/board` (copy then rename, so a running binary is replaced safely).
- `BOARD_VERSION=vX.Y.Z` pins a release (default latest); `BOARD_INSTALL_DIR` changes the
  directory.
- **Immutability**: before installing it asks the GitHub release API whether the release is
  immutable and refuses one that is not confirmed, as `deck update` does;
  `BOARD_REQUIRE_IMMUTABLE=0` (exactly `0`) opts out with a note. (deck's own `install.sh` does
  not check this yet; board's does.)
- Prints the installed `board version` and warns if the directory is not on `PATH`.

**`board update`** replaces the running binary with the latest (or `--version`) release, with
the same checksum and immutability checks; `--check` only reports. It refuses dev builds,
package-managed installs and unwritable directories, like `deck update`.

**`INSTALL-AGENT.md`**: an install guide for agents, with no script to pipe into a shell.
Numbered steps the agent runs and checks itself, each with the command and the expected result:

1. Detect OS and arch (`uname -s`, `uname -m`) and map them to the asset name.
2. Check prerequisites (`curl` or `gh`, `tar`, `sha256sum` or `shasum`).
3. Find the release: latest tag from the GitHub API (or the pinned version).
4. Confirm the release is immutable (API field), and stop if not.
5. Download the asset and `checksums.txt`; verify the SHA-256 of the asset.
6. Extract and install to `~/.local/bin` (or the chosen directory); make it executable.
7. Run `board version` and compare with the release tag.
8. Configure `BOARD_URL` and `BOARD_API_KEY` (from the operator's secret store, never pasted
   into files under version control), and `BOARD_ACTOR` per the identity rules (§9).
9. Run `board whoami` and expect the handle, key name and `server ok`.

The README says: *"or ask your agent: install board following
`<repo URL>/INSTALL-AGENT.md`"*.

A CI check (Lint stage) parses `install.sh` and `INSTALL-AGENT.md` and fails if they disagree on
the asset name pattern, supported OS/arch list, checksum file name, default install directory or
the environment variable names. Where releases come from while the repository is private is the
operator's deployment concern, not part of this spec.

### 8.6 Agent protocol: `board help agent`

The rules an agent follows live **in the binary**, not in a skill file each operator copies and
lets drift. `board help agent` prints a cheat sheet of about 40 lines, headed
`board protocol vN`, that is the whole working protocol:

- **identity**: export `BOARD_URL`, `BOARD_API_KEY`, `BOARD_ACTOR`, `BOARD_WAY_BACK` per the
  operator's instructions (§9); check with `board whoami`; subagents use the parent's handle;
- **start**: `board start <task> "<intent>" --res …` (or `board new`) once the task is
  understood; search before creating, never duplicate an open task;
- **ping**: `board ping` at each milestone and at least every half TTL; once per iteration in
  autonomous loops;
- **shared-resource check**: `board now --resources …` before any risky shared action
  (reboot, deploy, force-push, migration);
- **conflict handling**: exit code 7 / a `CONFLICT` line means read the other lease and its
  notes, leave a note on the resource, then wait, coordinate or ask; never proceed silently
  through a conflict on a destructive action;
- **block**: `board block <task> "<question>" --stop | --poll D`; record answers given in
  conversation with `board answer <A…> "<answer>" --relay`;
- **finish**: `board finish "<outcome>" [--done]`;
- **catchup**: `board catchup` at check-in and after long waits; open note bodies only when
  the summary concerns you;
- **close only when asked**: any actor may close, but agents do so only on the operator's
  explicit instruction, with `--for operator`.

The text is a Go `embed`ded file (`internal/cli/protocol.txt`) with its version number `N`, a
small integer bumped whenever an agent would have to behave differently (wording fixes don't
bump it). `board help agent --version` prints just `N` and records nothing. Running
`board help agent` prints the text, then records the version as read for the actor
(`PUT /actors/me/protocol-seen`, best effort: with no `BOARD_URL` or `BOARD_API_KEY`, an unreachable
server or a refused key it still prints the text, prints nothing on stderr and exits 0). Any other
argument to `board help` is the exit-2 usage error `usage: board help agent [--version]`.

**Protocol v0** was the text of Phase 0: identity, `board whoami`, the initiative commands and the
output rules.

**Protocol v1** was the text of Phase 1. It was headed `board protocol v1`, fit 45 lines of 100 columns
(a test holds the budget) and named no command or flag the build lacked (a test and the
`help_agent.feature` scenario check each `` `board …` `` span against the command tree). On top of
v0 it covers:

- the tasks: `board task ls`, `board task add`, `board task done` (agents mark done) and
  `--json`;
- the leases: `board lease start <task> "<intent>" --res … --ttl …`, `board lease renew` at each
  milestone and at least every half TTL (with no argument it renews all the actor's leases),
  `board lease end "<outcome>"`, `board lease ls`, and that an unrenewed lease expires on its own;
- `board milestone <task> "<summary>"` and `board check <task> "<summary>"` (watch tasks);
- the shared-resource check `board now --resources …` before a risky action (reboot, deploy,
  force-push, migration);
- conflict handling: a `CONFLICT <resources>: L<n> <holder> (<task>) "<intent>" exp <time>` line
  and exit 7 mean another actor holds a lease on a resource; the lease is started anyway, so read
  the intent, then wait, coordinate or ask, and never go on through a conflict on a destructive
  action;
- closing only when asked: agents never close a task unless the operator asked, and then use
  `board task close <ref> --for operator`;
- the exit codes 6 (state conflict) and 7 (conflict) next to those of v0.

**Protocol v2** is the current text. It is headed `board protocol v2`, replaces v1's wording
rather than adding to it so as to keep the same budget (45 lines of 100 columns, held by a test),
and names no command or flag the build lacks (the `help_agent.feature` scenario checks each
`` `board …` `` span against the command tree, which lists `ask`, `ask show|ls|withdraw`,
`answer`, `note`, `note show`, `notes` and `attention`). On top of v1 it covers:

- asks: `board ask <task> "<question>" --option …` raises a `stop` ask (the default) and returns, so
  the agent ends its turn and reads the answer in a later session with `board ask show A<n>` or
  `board ask ls`; `--poll D` waits instead, exit 0 answered, 6 withdrawn and **3** still open at the
  deadline (go on without the answer and check back); `--blocking` blocks the task meanwhile;
  `board ask withdraw A<n>` when it no longer matters;
- `board answer A<n> "<answer>" --relay` to record an answer a human gave in conversation;
- notes: `board note "<summary>" --task <ref> --body -` (a broadcast without `--task` or `--init`),
  `board notes --since last --ack` at check-in (`--ack` moves the cursor past the notes shown, a
  `+` marks a body, `board note show N<n>` prints it) and `board attention` for what needs a human;
- exit code 3 (an ask still open) next to the others.

The shortcuts of §8.4 (`start`, `ping`, `block`, `finish`, `catchup`, …) arrive with their
commands, each bumping the version.

**Server side.** `GET /meta` answers `{version, protocol, principal, actor}`: the server binary's
version (as `board version` prints it), the protocol version its binary embeds, the principal
that authenticated (`key:<name>` or `oidc:<email>`) and the actor the request acts as. It reads
nothing from the database. `PUT /actors/me/protocol-seen` takes `{"version": N}` (an integer of
0 or more; anything else is 400 with a `next`), upserts the calling actor and sets its
`protocol_seen` column to N when N is higher than what is stored, so the column holds the highest
version read and a repeated or older call changes nothing. It answers
`{actor, protocol_seen}` with the value stored.

**Staleness hint.** `GET /meta` returns the server's protocol version (the one its own binary
embeds). `board start` (and `board new`) prints at most one extra line when either is newer:

```
hint: board protocol v4 on the server, you have v3 → board update, then board help agent
hint: board protocol v4 changed since you last read it → board help agent
```

The hint never changes the exit code and never blocks the command.

**Tests.**
- Golden file: the exact text (`internal/cli/testdata/help-agent.golden`), updated deliberately with
  `go test -update`. A changed golden file shows up in review, where the version bump is decided.
- E2E (Godog, `features/help_agent.feature`): the text the binary prints is parsed for every
  `board …` command (written in backticks) and every `--flag` in it, and each is resolved against the
  CLI's command tree (`cli.Commands()`, which holds the flag tables the parsers read); an unknown
  command or flag fails the build. The same feature checks that reading the text records
  `protocol_seen` and that the text prints with the server unreachable; `features/meta.feature`
  covers `GET /meta`. The text
  never mentions a command that does not exist in that build.
- Line budget: the test fails above 45 lines or 100 columns.

---

## 9. Agent identity (template)

The server does not know any agent tool. Each actor sends a **handle** and a **way back**.
How an agent derives them is the operator's policy, kept in their own agent instructions
(e.g. a global `AGENTS.md` / `CLAUDE.md`), not in this repository's code. The CLI ships only a
generic fallback.

Template for the operator's instructions (first matching rule wins; adapt to your tools):

```markdown
## board identity
Before using `board`, export BOARD_URL, BOARD_API_KEY (this environment's key,
from your secret store), BOARD_ACTOR and BOARD_WAY_BACK:
1. Autonomous run (e.g. RALPHD_RUN_ID set):   ralph:<run-id>
   way back: `ralphctl status <run-id>`
2. Session manager (e.g. DECK_SESSION_NAME set): deck:<DECK_PROFILE>/<DECK_SESSION_NAME>
   way back: `deck` → profile <DECK_PROFILE>, session <DECK_SESSION_NAME>
3. Claude Code (CLAUDE_CODE_SESSION_ID set):  claude:<host>:<first 8 of session id>
   way back: `claude --resume <session id>` on <host>, cwd <cwd>
4. Fallback (CLI built-in):                    <tool|agent>:<host>:<pid>
Always include the host when the identifier is not globally unique.
Subagents never register: they act under their parent's handle.
```

The CLI's built-in fallback: `BOARD_ACTOR` if set, else `agent:<hostname>:<ppid>`, with way
back `pid <ppid> on <hostname>, cwd <cwd>`. It never reads tool-specific variables.

---

## 10. Agent skill

The skill is a thin pointer; the protocol itself is `board help agent` (§8.6), so it updates
with the binary instead of with every operator's copy. `skill/SKILL.md` (generic; operators copy
it once):

```markdown
---
name: board
description: Shared work board. Use at the start of any task, before risky shared actions,
  when you need the operator, and when you finish.
---
Run `board help agent` once per session and follow it.
Set your identity per the operator's AGENTS.md ("board identity") before the first command.
If `board` is missing, install it following <repo URL>/INSTALL-AGENT.md.
```

The previous long outline is now the content of `board help agent`.

---

## 11. Web UI

Single-page app served by the same binary. Read-mostly for agents' output, full editing for the
operator. Live via SSE (§7.1).

| view | content |
|---|---|
| **Attention** (default, `#/attention`; `#/` lands on it) | the five §5.3 sections; inline actions: answer ask, close task (with resolution), acknowledge expired lease, record check, open the actor's way back (copyable). |
| **Timeline** | Gantt-style, `vis-timeline`: groups = initiatives (collapsible) → tasks; per task a planned bar (light) and actual span (created → closed), lease spans as stacked sub-bars coloured by state (live / ended / expired), markers for milestones, asks, checks, done, close; dependency arrows. Zoom hour → quarter. Filters: tags, initiatives, actor, "show archived closed within N days". |
| **Initiatives** (`#/initiatives`) | list with tags, open/done/closed counts, live-lease count and a status filter; initiative page (`#/i/<slug>`) with its tasks table (the page's own timeline is a later phase). |
| **Task** | details, deps, live leases, asks, notes, full event timeline, external links. |
| **Notes** | the public board: newest first, summaries with expand-to-body, filters (broadcast only, task, initiative, resource, author, mention). Post a note as operator. |
| **Actors** | who is alive now (live leases, last seen), with way back. |
| **Archive** | closed tasks/initiatives, adjustable window (7/30/90 days/all), reopen. |
| **Settings → API keys** | create (key shown once), list (name, prefix, created, last used, expiry), revoke (§3.3). Login / logout via OIDC (§3.2). |

**Live refresh** is the module `web/src/lib/live.ts`: one `EventSource` per tab on `GET /api/v1/stream` (§7.1), opened
once. A view subscribes with a predicate over the `board` events that concern it (event type, initiative, task) and a
refetch; a matching event refetches that view, **at most once per 500 ms burst** (the first event of a burst starts
the timer, the others share its refetch). `attention` messages go to a listener (the nav badge) without a fetch. A
`reset`, and the stream opening again after an error, refetch every subscribed view; the first open does not. A stream
the browser gave up on (closed, not retried) is opened again after 3 s, and at once when the browser comes back online.
The **indicator** state is `live` (the stream is open), `reconnecting` (opening, or after an error) or `offline` (the
browser says it is offline, whatever the stream does). The shell shows it (`web/src/LiveIndicator.svelte`, started with
the shell) as a coloured dot plus the state's word (`live` success, `reconnecting` warning, `offline` danger), so the state
never rides on colour alone. It is a status region whose accessible name is `Live updates: <state>`, and it sits in the
phone top bar, at the foot of the tablet icon rail (word under the dot) and above the email in the desktop sidebar foot
(`data-testid="live-indicator"`, one per layout, only the one on screen is visible). Nothing in the UI polls on a timer.

**Settings → API keys** is the `#/settings/keys` view, linked from the navigation. It lists every key
from `GET /api/v1/keys` as one `<table>` that is a dense table from 768 px up and one card per key
below it (the same rows, so the `key-row`, `key-status`, `new-key` and `new-key-value` test ids keep
their meaning at every width). A row shows the name, the display prefix in monospace, the status as a
badge (`active` success, `expired` warning, `revoked` danger, an unknown status neutral), the creation
and last-use times as relative `<time>` elements (`web/src/lib/format.ts`; the absolute local time in
the `title`; `never` for a key not yet used), and the expiry as an absolute local time (`never` for none).
**New API key** (a button beside the heading from 768 px up, a primary button anchored above the tab
bar below it) opens the create form, a name and an optional expiry (`90d`, `12h`; blank for none) that
posts `POST /api/v1/keys`, in the dialog component (a bottom sheet on the phone). A refusal (a taken
name, a bad expiry) shows the problem document's detail inline in the form and keeps what was typed.
On success the form closes and the response's key appears once, in a highlighted monospace box that
wraps (the page never scrolls sideways at 390 px) with a **Copy** button and a **Done** button. Copy
writes exactly that key with the Clipboard API and confirms it with "Copied" (a refused write says so and
leaves the key selectable). The key is on the page nowhere else: Done, creating or revoking another key,
or reloading drops it. Each key that is not revoked has a Revoke button. It asks for confirmation first, in the
dialog component (a bottom sheet on the phone) that names the key: Cancel (focused first), Escape or the backdrop
revokes nothing, and confirming posts `POST /api/v1/keys/{name}/revoke`; the list then reloads and shows the key as `revoked`. Every fetch of
the list is a loading state: the first, and the reload after a create or a revoke. While any of its requests is pending the page shows skeleton
rows (`aria-busy`, in place of the table; the once-shown key box stays), and the state ends only once every pending request has succeeded or
failed (a failure shows the error). Fetches can overlap (a key created while the first fetch is still out) and land in any order: the newest
outcome to land is shown and an older one landing after it is dropped (`web/src/lib/fetches.ts`). A refused create or revoke makes no fetch. With no keys the page shows an empty state. The page calls the API with the session
cookie, and its writes send the session's `X-CSRF-Token` (§3.2). A key revoked on the page gets 401 from
its next request (`e2e/keys.spec.ts`, `features/ui_keys.feature`).

**Phase 0 pages.** The app has three views, chosen by the URL fragment: **Initiatives** (`#/initiatives`), the **initiative page** (`#/i/<slug>`, below), the **Tasks** view (`#/tasks`, below)
and **Settings → API keys** (`#/settings/keys`); the other views in the table are later phases. The Initiatives view lists the initiatives
from `GET /api/v1/initiatives?status=<filter>` (following `next_after` through the pages) as one `<table>` that is a dense table from 768 px up and one
card per initiative below it (the same rows, so the `initiative-row`, `initiative-status`,
`initiative-tags` and `email` test ids keep their meaning at every width). A row shows the title, a link to
`#/i/<slug>` (`initiative-link`), with the slug beneath it in monospace, the status as a badge (`open` success,
`closed` neutral, an unknown status neutral), the tags as chips, the task counts of the item ("2 open · 1 done ·
1 closed", "No tasks" when none; `initiative-counts`) with its live-lease count ("1 live lease", "no live leases";
`initiative-leases`) in the Tasks column, and the creation time as a `<time datetime>` whose text is relative
(`web/src/lib/format.ts`: just now, minutes, hours, yesterday, days up to 7, then the local date) and
whose `title` is the absolute local time. **New initiative** (a button beside the heading from 768 px
up, a primary button anchored above the tab bar below it) opens the create form, a slug, a title, an
optional description and optional comma-separated tags that posts `POST /api/v1/initiatives`, in the
dialog component below (a bottom sheet on the phone). A refusal (a bad or taken slug, a bad tag) shows
the problem document's detail inline in the form and keeps what was typed; success closes the form,
reloads the list and announces "Initiative <slug> created" in an `aria-live="polite"` toast that clears
itself. A form that is opened again starts clean. Every fetch of the list is a loading state: the first,
and the reload after a create. While any of its requests is pending the page shows skeleton rows (`aria-busy`, in
place of the table), and the state ends only once every pending request has succeeded or failed (a failure shows the
error); overlapping fetches land in any order, the newest outcome to land is shown and an older one landing after it is
dropped (`web/src/lib/fetches.ts`); a refused create makes no fetch. An empty list shows an empty state whose **New initiative** button opens the same form.
The page calls the API with the session cookie, and its write sends the session's `X-CSRF-Token` (§3.2)
(`e2e/initiatives.spec.ts` at 390x844 and 1440x900).

**Status filter.** Above the list, a select labelled Status holds open, closed and all (open by default). Choosing
one fetches the list again with that `status` (the page keeps the choice for the reload after a create); an empty
result says "No open initiatives", "No closed initiatives" or "No initiatives yet", and the select stays on
screen in every state.

**Initiative page** (`#/i/<slug>`, `web/src/InitiativePage.svelte`, R9). The route table (`web/src/lib/routes.ts`)
holds it as an entry with `param: true`: its `hash` `#/i/` is a prefix and the one slash-free segment after it is the
view's parameter (`routeParam`); it names `parent: 'initiatives'`, so it has no navigation entry of its own and the
Initiatives entry is the one flagged `aria-current` while it is open. A fragment with an empty or multi-segment
tail is not this route. The page reads `GET /api/v1/initiatives/<slug>` and `GET /api/v1/tasks?initiative=<slug>&status=all`
(following `next_after`); a failure of either (an unknown slug is 404) shows its problem detail in an alert and no
partial page. It shows a back link to the list, the title, the slug in monospace, the status badge, the tags, the
targets ("Targets: 2026-03-01 to 2026-04-15", "from ...", "until ...", none when unset), the creation time, the
description as rendered Markdown (`renderMarkdown`, the only HTML the page injects), the counts as three badges
(open, done, closed) with the live-lease total, and the tasks. The counts are those of all the tasks, whatever the
task filter shows. The tasks are one `<table>` from 768 px up and one card per task below it, with the columns Ref,
Title, Kind, Status, Flags (the `active`, `blocked` and `overdue` badges of the task, from the API's derived fields)
and Lease holder (the actor of the oldest live lease, "+N" for further ones). A select labelled Tasks filters them:
not closed (open and done; the default), done, closed and all; the page holds every task, so choosing needs no
fetch. A filter that keeps none says "No tasks match this filter." The logic is `web/src/lib/initiativePage.ts`
(`InitiativePageStore`, `taskRows`, `taskBadges`, `leaseHolder`, `taskCounts`, `targetsText`).

**Initiative page actions** (R9). Under the header sit an Edit button and, by status, a Close button (open) or a
Reopen button (closed). Each opens the one `Dialog` (a bottom sheet on the phone) with the writes carrying the
session's `X-CSRF-Token`; a toast confirms what was done.
*Edit* ("Edit initiative", `InitiativeEditForm.svelte`) holds the title, description, tags (one comma-separated line)
and the target start and end (date inputs) in one dialog, filled from the initiative as it is. Save sends `PATCH
/api/v1/initiatives/<slug>` with every field (`toPatch`: text trimmed, tags as a list, a blank target as `null`, which
clears it), then reads the page again without the skeleton. A refusal (a blank title, a target end before the start)
shows the server's problem detail in an alert inside the dialog, which stays open with what was typed; Cancel and Escape
leave the initiative as it was.
*Close* ("Close initiative", `InitiativeConfirm.svelte`) is a confirmation. The page holds every task, so the dialog
counts the tasks that are not closed (open or done; `notClosedCount`). With none it says what closing does and its
button is "Close initiative", which posts `{}`. With N it says "N tasks not closed. Closing drops them too: each is
closed as dropped." and the button is "Close and drop N tasks" (singular for one), which posts `{"force": true}`; the
server then closes each task `dropped` and the initiative in one transaction. The toast says "Initiative <slug> closed,
N tasks dropped" and the page shows the closed status and the counts moved to closed. A refusal (for example the
initiative was closed by someone else meanwhile, 409) shows inline and the dialog stays open.
*Reopen* ("Reopen initiative") is a confirmation that says the closed tasks stay closed; its button posts to `/reopen`.
The wording is `web/src/lib/initiativeDialogs.ts`; the calls are `InitiativesApi.patch`, `.close(slug, force)` and
`.reopen`, and `InitiativePageStore.edit`, `.close`, `.reopen` hold the refusal as `dialogError`. Specs:
`e2e/initiative_page.spec.ts` (open from the list, filter the tasks, filter the list: open, then closed, then all;
edit title, description, tags and targets; a refused edit; close with no tasks; close with an open task through force;
reopen; a refused close) at 390x844 and 1440x900; the page and its Edit and Close dialogs are also in
`e2e/a11y.spec.ts` (12 states) and `e2e/touch.spec.ts`. The initiative page also has **New task** (open initiatives
only): it opens the New task dialog below with this initiative preset, and a created task reloads the page's tasks and
counts without the skeleton.

**Initiative view libraries** (R9). `web/src/lib/markdown.ts` renders
a description as safe HTML: every character is escaped first, so raw HTML shows as text; paragraphs, headings (two
levels down), lists, quotes, fenced code, inline code, bold, italic and links are the only markup; a link whose
scheme is not http, https or mailto (after whitespace and control characters are ignored, as a browser does) is
dropped to its text, and a kept link carries `rel="noopener noreferrer"`. `web/src/lib/initiativeFilters.ts` holds the
list's status filter (open, closed, all; open by default; sent as the `status` query) and the initiative page's task
filter (not closed = open and done, done, closed, all; not closed by default). `web/src/lib/counts.ts` turns a
task `counts` object into the badges and the line "2 open · 1 done · 3 closed" ("No tasks" when empty), and a live
lease count into "no live leases", "1 live lease" or "3 live leases".

**Tasks view** (`#/tasks`, `web/src/Tasks.svelte`, R10). The route table holds it as the entry `tasks` between
Initiatives and Settings, so the phone tab bar, the tablet rail and the desktop sidebar all show it, `aria-current`
on its link. The router matches the fragment without its query (`hashPath` in `web/src/lib/routes.ts`): `#/tasks?status=done`
is the Tasks view, and `#/tasks/` is not. The view lists the tasks of every initiative from `GET /api/v1/tasks`
(following `next_after`) as one `<table>` from 768 px up and one card per task below it, with the columns Ref, Title,
Initiative (a link to `#/i/<slug>`), Kind, Status, Flags (`active`, `blocked`, `overdue` badges) and Lease holder.
Above it sit seven controls: selects for Status (not closed by default, open, done, closed, all), Kind (any, work,
watch), Initiative (any, then every initiative by slug) and Active, Blocked and Overdue (any, yes, no), and a Search
box (title or description contains the text, ignoring case; applied on Enter or when it loses focus). A **Clear
filters** button shows while any control differs from the default. The filter is the URL fragment's query
(`filterFromHash`, `tasksHash`): choosing a control rewrites the fragment in place with `history.replaceState` (no history
entry per choice) and fetches the list again, so a reload or a copied link shows the same controls and rows; editing
the fragment by hand re-reads the filter. A filter that keeps none says "No tasks match these filters." (or "No tasks
yet." with the default filter); a failed fetch shows its problem detail in an alert; while any fetch is pending the
view shows skeleton cards, and of overlapping fetches the newest outcome to land is shown (`TasksStore`,
`web/src/lib/tasks.ts`). **New task** is a button beside the heading from 768 px up and a primary button anchored above
the tab bar below it.

**New task dialog** (`web/src/NewTaskDialog.svelte`, R10). The one `Dialog` (a bottom sheet on the phone) opened from the
Tasks view and from an initiative page, which presets the initiative; the Tasks view presets none and the select asks to
choose one. The form holds: Initiative (a select of the open initiatives, plus the preset one), Title, Description,
Kind (work or watch), Cadence (shown only for a watch task: a number and a unit, `90s`, `15m`, `2h` or `1d`), Planned start
and Planned end (date inputs, sent as midnight UTC), Blocked by (a search box over the open tasks of every initiative,
at most eight matches by ref or title, each added as a removable chip) and Links (a label and an `http(s)` URL, added with
**Add link** as removable chips; a pair typed but not added is added on submit). Create posts `POST /api/v1/tasks`
with the session's `X-CSRF-Token` (`toTaskCreate`: text trimmed, blank optional fields left out, `cadence_seconds`
for a watch task only, `blocked_by` and `external_refs` when any). A cadence that is not a number and a unit, or a link
with only one part typed, is refused in the form before any request; a server refusal (a blank title, a closed
initiative, a cadence under 60 s) shows its problem detail in an alert inside the dialog, which stays open with what was
typed. On success the dialog closes, a toast says "Task <ref> created" and the list reloads. The logic is
`web/src/lib/newTask.ts` and `NewTaskStore`; the specs are `e2e/tasks.spec.ts` (create a work task with a blocker, planned
window and link, and a watch task with a refused and then a good cadence; a refused create keeps the input; every filter
and a reload keeping them; New task from the initiative page with the initiative preset) at 390x844 and 1440x900, and
the view and the sheet are also in `e2e/a11y.spec.ts`, `e2e/touch.spec.ts`, `e2e/overflow.spec.ts` and
`e2e/sameorigin.spec.ts`.

**Task view libraries** (R10). `web/src/lib/taskFilters.ts` holds the Tasks view's filter (status, kind, initiative,
active, blocked, overdue and text) and its encoding in the URL fragment's query: `#/tasks` is the default filter
(status not closed), and a field that differs adds `status=`, `kind=`, `initiative=`, `active=`, `blocked=`,
`overdue=` (`true` or `false`) or `q=`, in that order, so a reload reads the same filter back. The same filter is
turned into the `GET /tasks` query (the default status is sent as `not_closed`). `web/src/lib/taskActions.ts` is the
action table of the task page: Edit is offered unless the task is closed, Done on an open task, Reopen on a done or
closed one, Close unless closed, Milestone always, and Record check on a watch task that is not closed.
`web/src/lib/durations.ts` writes a cadence as "every 5 minutes" (the largest unit that divides it exactly) and a
lease's expiry as "expires in 41m", "expires in under a minute" (under 60 seconds left) or "expired 2m ago"; a watch
task's next check is "due in 14m", "due in under a minute" or "overdue by 2h" (`nextCheckText`).
`web/src/lib/conflicts.ts` groups the live leases (state `live` and not yet past `expires_at`) for the Now panel and the
task page. A conflict is a resource held by two or more live leases (§5.2): one actor's two leases on different tasks
that share a resource conflict too, and an ended or lapsed lease conflicts with nothing. `resourceConflicts` lists the
contested resources by name with every holder, `nowPanel` gives one row per live lease (flagged when it is in a
conflict), the conflicts and a summary such as "2 live leases, 1 conflict", and `taskConflicts` gives, per contested
resource a live lease of one task holds, the other holders (printed "repo:board: also held by bob on bb-1").

**Attention libraries** (R9). `web/src/lib/attention.ts` holds the Attention view's presentation and filter. `SECTIONS` lists the
five sections in §5.3 order (open asks, done not closed, expired leases, overdue watches, conflicts) each with its calm
empty-state text (`emptyText`), and `sectionCount` reads a section's count from the response's `counts`. `ageText` phrases an
ask's `age_seconds` as "just now" (under a minute), "5 minutes ago", "3 hours ago" or "2 days ago" (the largest whole unit),
and `askerWaitingLabel` says "asker waiting" or "asker not waiting". The filter (`tag` and `initiative`, each repeatable) lives
in the URL fragment's query as repeated keys, tags first (`#/attention?tag=work&tag=home&initiative=aa`; the default is the bare
`#/attention`), with blanks and repeats dropped, so a reload reads the same filter back; `apiQuery` sends the same repeated keys
to `GET /attention`.

**Ask and notes libraries** (R10). `web/src/lib/asks.ts` presents an ask: `orderAsks` puts the open asks first, then the answered, then
the withdrawn (the order given is kept within a state; `openAsks` keeps only the open ones for the initiative page), `stateLabel` and
`modeLabel` name the state and the mode, `blockingLabel` says "blocks its task" (an open blocking ask) or "blocked its task" (a
closed one) and is empty for a non-blocking ask, and `stateLine` says how an ask stands or ended: "asker stopped", "asker waiting" or
"asker not waiting" (poll), "answered by alice in the UI" (`ui`, `through the API` for `api`, `relayed` for `relay`, with "(on behalf
of bob)" when a person is named) or "withdrawn: reason". `canAct` offers Answer and Withdraw on an open ask only, and `blockedReasons`
lists "blocked by A3: question" for each open blocking ask (the reason in a task's `blocked` badge). `web/src/lib/notes.ts` holds the
Notes view's filter: broadcast only, task, initiative, resource, author and mention. It lives in the URL fragment's query as
`broadcast=only`, `task=`, `initiative=`, `resource=`, `author=`, `mention=` in that order, with blanks left out (the default is the
bare `#/notes`), so a reload reads the same filter back; `apiQuery` sends the same keys to `GET /notes`.

**Attention view** (`#/attention`, `web/src/Attention.svelte`, R9). It is the first entry of the route table and so the
default: the router sends every fragment the table does not name, `#/` included, to the first entry, and a login lands on
it. The view reads `GET /attention` under its filter and draws the five sections in §5.3 order (open asks, done not
closed, expired leases, overdue watches, conflicts) as `h3` headings each with its count (`section-count`); a section with
no item shows its calm empty text (`section-empty`, `SECTIONS` in `web/src/lib/attention.ts`) and a count of 0. An item is
a card (`attention-item`) with a title that links to its task (`#/t/<ref>`; a conflict links only when it names one task),
detail lines (an ask's age, mode, whether the asker waits and `blocking`; when a task was done and its last lease's
outcome; a lease's intent and expiry; when a watch's check was due and the last check; a conflict's holders) and its
actors, each as `role handle` with the way back the board has on record in monospace and a **Copy way back** button
(`copy-way-back`, named "Copy way back of <handle>") that writes it to the clipboard through `copyText` and confirms in the
polite toast; an actor with no way back says so and has no button. `rowsFor` in `web/src/lib/attentionView.ts` builds the
rows, `AttentionStore` holds the answer, drops one that lands after a newer one or for a filter no longer set, and keeps what
is shown while a refresh is out. The tag and initiative filters are toggle chips (`aria-pressed`, 44 px) drawn from the
initiatives and their tags plus whatever the fragment names, with a **Clear filters** button; a change rewrites the fragment
in place (`#/attention?tag=work&initiative=aa`) and refetches, and a reload or a pasted link reads the same filter back.
The view subscribes to `live()` (`affectsAttention`: every event but notes and keys) so a matching event refetches it
within the 500 ms debounce, with no timer of its own. `e2e/attention.spec.ts` (phone and desktop) populates the five
sections through the API (an expired lease by moving its expiry into the e2e store, as `e2e/a11y.spec.ts` does), asserts the
order, counts, empty states, actors and way backs, the clipboard text, the filters surviving a reload, `/` and `#/`
opening Attention, and an ask raised and answered through the API appearing and disappearing within 5 s without a reload.
Attention is a nav entry and so a state of `e2e/shell.spec.ts`, `a11y.spec.ts`, `touch.spec.ts` and `sameorigin.spec.ts`;
the Initiatives view moved to `#/initiatives`, which the initiative page's back link and those specs use.

**Attention inline actions** (R9, `web/src/lib/attentionActions.ts`, `web/src/AnswerForm.svelte`). `actionsFor(section)` is the
finite table of what an item offers: an open ask **Answer** and **Withdraw**, a done task **Close**, an expired lease
**Acknowledge**, an overdue watch **Record check**, a conflict nothing. Each button (`action-<kind>`, 44 px) is named "<label>
<ref>"; an item with a task link also links to its initiative (`initiative-link`, `#/i/<slug>`, the ref's slug). Answer opens
the one `Dialog` over the ask read by `GET /asks/<ref>`: the question as its heading, the details as safe Markdown
(`renderMarkdown`, so raw HTML shows as text), one button per suggested option that puts the option in the answer field, a free
text field (16 px), **Send answer**, and a link to the ask's initiative. Withdraw (optional reason, at most 200 characters),
Close (the four resolutions) and Record check (summary, 1 to 200 characters) open the same dialog forms the task page uses;
Acknowledge posts at once. `AttentionActions` runs one action: an input that cannot be sent (an empty answer, a summary over
200 characters) and any refusal of the API (409 for an ask someone else answered first) are shown inline, inside the dialog
(`dialog-error`) which stays open with the typed text, or on the item (`item-error`) for Acknowledge; a success closes the
dialog and says so in the polite toast ("Answered A1", "Withdrew A1", "Task aa-1 closed as dropped", "Lease L2
acknowledged", "Check recorded on aa-1"). Nothing refetches by itself: the acted-on item leaves its section when the stream's
event (`ask_answered`, `ask_withdrawn`, `task_closed`, `lease_acked`, `check_recorded`) refreshes the view.
`e2e/attention.spec.ts` covers the six actions and the refusal at phone and desktop, and `a11y.spec.ts` scans the Answer sheet.

**Task page** (`#/t/<ref>`, `web/src/TaskPage.svelte`, R10). The route table holds it as the entry `task` (a parameter route
like `#/i/<slug>`, one slash-free segment is the ref, either letter case) with the parent `tasks`, so the Tasks entry of
the navigation has `aria-current` and there is no navigation entry of its own; the Ref of a row in the Tasks view and in
the initiative page's table is a link to it. The page reads `GET /api/v1/tasks/<ref>` and every page of
`GET /api/v1/tasks/<ref>/events` (`TasksApi`, `TaskPageStore` in `web/src/lib/taskPage.ts`); either failing shows one
alert and no half page, and a ref that does not exist is that alert (the API's 404). It shows, top to bottom: a back link
to Tasks and a link to the task's initiative; the header (the ref in monospace, the title, the status, the resolution of
a closed task, the kind with a watch task's cadence as "watch, every 15 minutes", the `active`, `blocked` and `overdue`
badges, who created it and when); the description as safe Markdown; the planned window ("2026-03-01 to 2026-04-15",
"from ...", "until ...") and a watch task's next check; the links (`http(s)` only, `rel="noopener noreferrer"`); **Blocked
by** and **Blocking**, each task a link to its own page with its ref, title and status; the **live leases** (holder,
lease ref, intent, resources, "expires in 41m" from `expiresIn`); and the **timeline**: every event of the task newest
first, each with its summary, a relative time (the absolute local time in its `title`), the actor and the event type
(`data-type`). The actions come from `taskActions`: **Edit** (not on a closed task), **Done** (open), **Reopen** (done or
closed), **Close** (not closed). Done and Reopen act at once; **Edit** opens the one `Dialog` (a bottom sheet on the
phone) with Title, Description, Planned start and end, Links, and for a watch task Cadence and Next check (UTC), and sends
only the fields that differ from the task as it was shown (`toTaskPatch`: an emptied planned date or next check is `null`,
which clears it; a cadence cannot be cleared, so an emptied cadence, like one that is not a number and a unit, is refused in
the form and nothing is sent). Every `PATCH`-able field of R2 is editable there: the Links section lists the task's links
as editable label and URL rows with a Remove button each, plus a New link row (Add link, or Save adds a complete one), and
the whole list is sent as `external_refs` when it differs (replace, add and remove are all that one field); a half-typed
link or more than 20 links is refused in the form, and a URL the API refuses (not http or https) shows inline with
everything typed kept. The Next check field is a `datetime-local` read as UTC to the minute and sent as that instant
(`2099-02-03T10:20:00Z`); the page then shows the new check time and `task_updated` names `next_check_at` or
`external_refs`; **Close** opens a confirmation with a Resolution select
(completed, dropped, superseded, duplicate; completed first) and posts the chosen one. A refusal from the API (a stale
page, an empty title) shows its problem detail inline, in the dialog when one is open (which stays open with what was
typed) and as an alert under the header otherwise. A success closes the dialog, shows a toast ("Task aa-1 marked done",
"... reopened", "... saved", "... closed as dropped") and reads the task and its events again without the skeleton, so
the new event is the first row of the timeline. On the phone the actions sit in a bar fixed above the tab bar (44 px
buttons, always visible, never hover-only); from 768 px they are part of the header.

The page also reads the task's expired leases (`GET /api/v1/leases?task=<ref>&state=expired`, every page) and carries
the rest of the R10 actions next to the section they change, so the phone's action bar stays four buttons wide.
**Add dependency** (button beside the Blocked by heading) opens a sheet with a search field over the open tasks of every
initiative (ref or title, case-insensitively, at most 8 shown; the task itself and the tasks it already waits for are
left out; `depOptions`); picking one sends `POST /tasks/<ref>/deps/<blocker>`. A refusal such as a cycle (409) shows in the
sheet, which stays open. Each task under Blocked by has a **Remove** button (`DELETE /tasks/<ref>/deps/<blocker>`, at once,
no confirmation); `blocked` is re-read from the API, so the badge appears and disappears with the dependency. **Milestone**
(beside the Timeline heading, on a task of any status) and **Record check** (same place, only on a watch task that is not
closed, from `taskActions`) open a sheet with one Summary field (1 to 200 characters after trimming; an empty or over-long
one is refused in the sheet before any request, `summaryProblem`) and post `POST /tasks/<ref>/milestones` or `/checks`;
a check moves the task's next check to now plus its cadence, which the page prints as "due in 14m" or "overdue by 2h"
(`nextCheckText`). Each live lease has an **End lease** button: a sheet with an optional Outcome (at most 200 characters)
that posts `POST /leases/<lease>/end`; a lease that is not live any more (409) is refused in the sheet. An
**Expired leases** section appears when a lease of the task ran out and nobody acknowledged it (`expiredRows`: holder,
lease ref, intent, resources, "expired 2m ago"); its **Acknowledge** button posts `POST /leases/<lease>/ack` at once. Each
of the six actions toasts ("Task aa-1 is now blocked by bb-1", "Task aa-1 no longer waits for bb-1", "Milestone recorded
on aa-1", "Check recorded on aa-1", "Lease L3 ended", "Lease L3 acknowledged") and reads the task, its events and its
expired leases again, so `dep_added`, `dep_removed`, `milestone`, `check_recorded`, `lease_ended` and `lease_acked`
appear at the top of the timeline. The specs are `e2e/task_page.spec.ts` (the page's content and links, the action bar,
Edit with a refused then a good save, a watch task's cadence, Edit's links (replace, add, remove, a refused URL) and next
check (set, move, overdue, clear), Done and Reopen with a stale refused Done, Close with each
of the four resolutions followed by Reopen, a refused Close, Add dependency and Remove with a refused cycle and a stale
removal, Milestone with refused summaries, Record check, End lease with a stale refusal, and Acknowledge on a lease whose
expiry is moved into the past in the e2e store) at 390x844 and 1440x900; `a11y.spec.ts` scans the page and the three
sheets, and `touch.spec.ts` measures them on the phone.

**Live leases and conflicts in the UI** (R11). The Initiatives list and the initiative page already print the live lease
count of an initiative ("2 live leases", `leasesText`), and the Tasks view and the initiative page print the holder of each
task's live lease. Two more places read every live lease of the board (`GET /api/v1/leases?state=live`, every page,
`TasksApi.liveLeases`) and apply `web/src/lib/conflicts.ts` to it; the conflicts are computed in the browser from those
leases, so they need no `/now` call and no actor. **Task page**: a **Conflicts** section appears under Live leases
when a live lease of the task shares a resource with another live lease (`taskConflicts`; the section is absent
otherwise). Each conflict is one line, "repo:board: also held by bob on bb-1" (resource, the other holder, the other task),
with a link to the page of every other task. **Now panel** (`web/src/NowPanel.svelte`, `NowStore` in `web/src/lib/nowPanel.ts`),
on top of the Tasks view above the filters, whatever filter is set: a heading, a summary ("2 live leases, 1 conflict" or
"no live leases", `nowPanel`), the conflicts (per contested resource, "Conflict repo:board: held by alice on aa-1, bob on bb-1"
with a link to each task) and one row per live lease (holder, lease ref, a link to its task, intent, resources, "expires in
41m", and a `conflict` flag when the lease is in a conflict). A **Refresh** button reads the leases again; the panel loads
with the view. A failed read shows an alert in the panel. On the phone the panel is folded to its heading and summary with a
44 px **Show** / **Hide** toggle (`aria-expanded`); from 768 px it is always open and the toggle is hidden. A conflict is a
warning in the warning colour, never a lock. `e2e/conflicts.spec.ts` starts two leases on one resource through the API as
two actors and checks the conflict on both task pages (the link from one to the other included) and in the Now panel,
then ends one lease and checks that it is gone from the panel after Refresh and from the task page after a reload, and that
the panel folds on the phone and stays open on the desktop, at 390x844 and 1440x900; `a11y.spec.ts` scans the Tasks view
with the Now panel open and a conflict, and the task page with a conflict, and `touch.spec.ts` measures the open panel.

**Shell and breakpoints.** One route table (`web/src/lib/routes.ts`) drives both the hash router
and every navigation surface: an entry (id, fragment, view title, nav label, icon) yields the
view that fragment selects (anything the table does not name is its first entry, Initiatives) and
its navigation entry, so adding a view is one entry plus its component. The shell
(`web/src/Shell.svelte`) shows exactly one navigation surface per width, the others are
`display: none`: below 768 px a top bar (product name, the view's title, an account button) and a
fixed bottom tab bar (icon plus label per entry, padded by `env(safe-area-inset-bottom)`;
`web/index.html` sets `viewport-fit=cover`, and the main region is padded so the last row of a
long list scrolls clear of the bar); from 768 to 1023 px a collapsed icon rail (labels are the
links' accessible names and tooltips, the account button at its foot); from 1024 px a labelled
sidebar whose foot shows the signed-in email, a Sign out button and the account button. The
account button opens the account menu (the email and Sign out) in the dialog component below, at
every width. The first focusable element of the page is a "Skip to content" link that moves focus
to the main region without changing the fragment; the navigation entry of the view on screen has
`aria-current="page"` (a page inside a view, `#/i/<slug>` or `#/t/<ref>`, marks its parent's entry:
Initiatives or Tasks). `e2e/shell.spec.ts` lists `#/` and `#/attention` (the Attention view), `#/initiatives`, `#/tasks`, `#/i/<slug>`, `#/t/<ref>` and
`#/settings/keys` as states and, at 390x844, 820x1180 and 1440x900, asserts the nav links are
exactly the route table's (imported, not copied), the right entry carries `aria-current`, and
navigating by nav click moves it. The page never scrolls horizontally (the same spec), whatever text the API accepts: both list tables set `overflow-wrap: anywhere`,
and from 768 px use `table-layout: fixed` with fixed widths for the short columns (status, tags,
dates, prefix, actions) so a title, slug, tag or key name with no space in it (200, 40, 40 and 64
characters at most) wraps inside its own cell instead of widening the table; below 768 px the
cards wrap it the same way. `e2e/overflow.spec.ts` seeds the longest accepted values (wide and
narrow glyphs, 20 tags of 40 characters, a URL-like title, a 64-character key name) and asserts
`document.documentElement.scrollWidth <= clientWidth` and that no row, chip, dialog or toast reaches past the
viewport at 390, 820, 1024 and 1440 px, for both lists, the revoke confirmation, the create toast and
the once-shown key box.
The same spec holds a finite list of Phase 1 states to the same bar, named in `PHASE1_STATES`: the views
`#/tasks` (with the Now panel open), `#/i/<slug>` and `#/t/<ref>`, and the nine dialogs edit initiative, close
initiative, new task, edit task, close task, milestone, record check, add dependency and end lease. They are
filled with the longest accepted values (a 40-character slug, 200-character titles of wide and narrow
glyphs, descriptions of 20000 characters (the longest accepted) with long unbroken runs, 80-character link labels, 200-character summaries, intents and outcomes,
20 resources of 100 characters, a conflict and an expired lease on the same task), and no state scrolls
sideways or reaches past the viewport at the four widths.

**Session states.** The page is always in one state of `SessionController` (`web/src/lib/session.ts`):
*loading* (it asks `GET /auth/session`), *signed-in* (the app shows), *redirecting* (the session answered
401, so the page goes to `/auth/login`, which starts the OIDC flow), *error* (the session could not be
read) and *signed-out*. Loading and redirecting show a centred card with the product name and a spinner.
Error shows a card with the `detail` of the problem document the session answered with (its `title` when
there is no detail, else "The session could not be read (HTTP n)."; "The server is not reachable." when no
answer came) and a **Retry** button that returns to loading and asks
again; a server that is down never starts a login redirect. **Sign out** (the sidebar foot at 1024 px and
up, the account menu below it, both showing the signed-in email) posts `POST /auth/logout` with the
session's `X-CSRF-Token`, then shows the **Signed out** view at `#/signed-out`: a card with a **Sign in**
button, a plain link to `/auth/login`. That view never starts a login redirect by itself: a page opened
on `#/signed-out` neither loads the session nor follows a 401. Leaving the view with Back loads the
session again, and a signed-in page that is sent to `#/signed-out` returns to `#/` (the Attention view). A failed logout
keeps the app and shows "Sign out failed". A reload of `#/signed-out` is answered by the server (the
fragment never reaches it), which sends an anonymous navigation to the login page (§3.2)
(`e2e/session.spec.ts`: sign in, Sign out, no navigation to `/auth/login`, the old cookie gets 401,
Sign in lands on Attention).

**Dialogs and sheets.** Every modal surface in the UI is the one `Dialog` component
(`web/src/Dialog.svelte`); its focus rules are pure functions in `web/src/lib/focustrap.ts`.
It renders `role="dialog"` and `aria-modal="true"` with an accessible label (`aria-label`).
When it opens, focus moves into it: to the element the caller prefers, else the first focusable
element, else the dialog container. Tab and Shift+Tab are trapped: Tab on the last focusable
element wraps to the first and Shift+Tab on the first wraps to the last. Escape closes it, and so
does a click on the backdrop. On close, focus returns to the element that had it when the dialog
opened (the opener), if that element is still in the page. Below 768 px the dialog is a bottom
sheet, full width and anchored to the bottom edge; from 768 px up it is a centred dialog. It fades
in (and the sheet slides up); under `prefers-reduced-motion: reduce` every animation and
transition is off. Interactive elements show a visible `:focus-visible` ring from the token
stylesheet.

R12 holds the nine Phase 1 dialogs to the same bar in `e2e/dialogs.spec.ts`, as a finite, named list: edit
initiative, close initiative, new task, edit task, close task, milestone, record check, add dependency and end
lease. For each, at 390x844 and 1440x900, the keyboard alone (Tab to the opener, Enter) opens it, focus moves
inside, Tab and Shift+Tab never leave the dialog and come round to the stop they started on, and Escape closes
it with focus back on the opener.

**Theme.** The account menu has a Theme control with three settings: **System** (the default),
**Light** and **Dark**. System follows the browser's `prefers-color-scheme`, Light and Dark ignore
it; the effective theme is `light` or `dark` and is the `data-theme` attribute of `<html>`, which
selects the token set in `web/src/styles.css`. The setting is stored in `localStorage` under
`board.theme`; a stored value that is not one of the three (or storage that cannot be read) is
System. The mapping of setting and system preference to the effective theme, the persistence and
the controller that applies it are `web/src/lib/theme.ts`; while the setting is System the page
follows a change of the system preference without a reload. **Pre-paint rule:** `web/index.html`
holds a small inline script in `<head>`, before the app bundle, that applies the same rule and
sets `data-theme` on `<html>` and the content of `<meta name="theme-color">` to the effective
theme's surface colour (`#f6f7f9` light, `#14161a` dark), so the first paint is already in the
right theme and there is no flash; `theme.test.ts` runs that script against the module's mapping
(`e2e/theme.spec.ts` emulates a dark system).

**Installable app.** `web/public/` is copied verbatim into the build (so into the embedded files
and served by the existing static lookup, no Go change): `manifest.json` (`name` and `short_name`
`board`, `start_url` and `scope` `./`, `display: standalone`, `theme_color` and `background_color`
`#f6f7f9`, and three icons: `icon.svg`, `icon-192.png`, `icon-512.png`), `favicon.svg` and
`apple-touch-icon.png` (180 px). `web/index.html` links the manifest, the SVG favicon and the
apple-touch-icon by relative URLs; the manifest link carries `crossorigin="use-credentials"`,
because every route except `/healthz` needs the session and browsers fetch a manifest without
cookies otherwise. No service worker is registered: the app is installable, not offline-capable.
The PNGs are rendered from `icon.svg`. `e2e/manifest.spec.ts` signs in, reads the three links from
the page, and checks the manifest is same-origin JSON with those fields and that the five icon
URLs answer 200 with an `image/*` content type.

**Same origin.** The UI requests nothing from another origin: no web font, CDN, remote icon or
script. `e2e/sameorigin.spec.ts` signs in through the mock provider, then records every request the
page issues while it loads `#/` (Attention), `#/initiatives`, `#/tasks`, `#/settings/keys` and `#/signed-out` (a full page load each) at
390x844 and 1440x900, and requires every http(s)/ws(s) URL to have the board server's
origin. R12 extends it to the Phase 1 views as a finite, named list (`PHASE1_STATES`): `#/tasks`,
`#/i/<slug>` and `#/t/<ref>` (each a full page load over a seeded watch task with a link, a blocker, a
dependent, a milestone, a live lease and a conflict), and again with a dialog opened on the page (new
task, edit initiative, edit task, add dependency, end lease): 8 states at both viewports (16 more
visits). A planted remote image turns the check red.

**Touch and phone ergonomics.** At the phone width (390x844) every visible interactive element
(`a, button, input, select, textarea, [role=button]`) in the shell, the forms, the lists and the
dialogs has a hit area of at least 44x44 CSS px, and every visible `input`, `select` and `textarea`
has a computed `font-size` of at least 16 px, so iOS does not zoom on focus. The hit area is the
element's own box: a visually hidden control does not count as met by being 1 px, so the Theme
radios are transparent inputs laid over their 44 px-high labels rather than `sr-only`. Only the
skip link, clipped away (`clip-path: inset(50%)`) until it takes focus, is outside the rule while
hidden. Nothing depends on hover: every action is a button or a link. `e2e/touch.spec.ts` measures
the Initiatives, initiative page, Settings -> API keys and Signed out views and the new-initiative sheet, the new-key
sheet, the revoke confirmation, the account menu and the initiative page's Edit and Close sheets opened over them, and fails with the offending
elements named when one is smaller or a field's text is under 16 px. It also holds the Phase 1 views and dialogs to the
same bar as a finite, named list (`PHASE1_STATES`): the views `#/tasks`, `#/i/<slug>` and `#/t/<ref>`, and the 9 dialogs
edit initiative, close initiative, new task, edit task, close task, milestone, record check, add dependency and end lease,
each opened over a seeded initiative with a watch task, links, a dependency, an expired and a live lease and a conflict.
A link is at least 44 px wide as well as high, so a short link label (a two-letter reference) still meets the bar.

**Accessibility bar.** axe-core (`@axe-core/playwright`, pinned exactly in `e2e/package.json`) reports
zero violations of impact `serious` or `critical`, with no rule disabled and no element excluded, on
every one of ten states: the Initiatives view, the initiative page, the Settings -> API keys view, the Signed out view,
the new-initiative sheet, the new-key sheet, the revoke confirmation, the account menu and the initiative
page's Edit and Close sheets opened over them. Each is scanned at 390x844 and 1440x900 in the light and the dark theme (the browser's
colour scheme drives the System setting), in `e2e/a11y.spec.ts`. The same spec holds the Phase 1 views and
dialogs to the bar as a finite, named list (`PHASE1_STATES`): the views `#/tasks`, `#/i/<slug>` and `#/t/<ref>`, and the
9 dialogs edit initiative, close initiative, new task, edit task, close task, milestone, record check, add dependency and
end lease, each opened over a seeded initiative with a watch task, a link, a dependency, a milestone, a check, an
expired and a live lease and a conflict. A test names the 3 views and the 9 dialogs, so a state cannot drop out of the
list unnoticed. A failing scan names the rule, the impact and the offending elements. Colour contrast is met by the design
tokens in both themes, never by a per-element override.

Global: tag filter persisted in the URL, keyboard navigation, no external
network requests (all assets embedded).

---

## 12. Stack

### 12.1 Server and CLI

- Go 1.25+, one module, one static binary (`CGO_ENABLED=0`).
- HTTP: standard library `net/http` (1.22+ routing), handlers generated from OpenAPI
  (`oapi-codegen`, strict server). No web framework.
- Logging: `log/slog` JSON. Metrics: Prometheus text exposition (minimal dependency).

### 12.2 Database

- `database/sql` with drivers: **SQLite** via `modernc.org/sqlite` (pure Go), **Postgres** via
  `jackc/pgx/v5/stdlib`.
- `BOARD_DB_URL` selects the driver: `sqlite:///data/board.db`, `postgres://…`.
- **Dialect layer**: one small Go interface, `store.Dialect`, with a `SQLite` and a `Postgres`
  implementation, for the differences that matter: placeholder style and `Rebind`, `INSERT ... RETURNING`,
  upsert (`ON CONFLICT ... DO UPDATE`), partial index creation, boolean encoding, `LISTEN/NOTIFY`
  availability, and recognising a missing-table or unique-violation error. Timestamps are `BIGINT`
  milliseconds and JSON is text in both, so neither needs per-dialect handling. Queries are hand-written
  portable SQL with `?` markers; no ORM.
- **Migrations**: `pressly/goose` v3 with Go-embedded SQL files in `internal/store/migrations/common`
  (DDL identical in both databases) plus `sqlite` and `postgres` (only where the DDL must differ: today
  the identity column `n` and the primary key of the tables that have one). A dialect applies its own
  directory merged with `common`, one version sequence across both. Applied on start
  (`BOARD_MIGRATE=auto`, default) or by `board admin migrate`. A database whose goose version is above the
  highest embedded one is refused (§8).
- SQLite settings: WAL, `busy_timeout=5000`, `foreign_keys=ON`, `synchronous=NORMAL`, applied to every
  connection through the DSN, with one writer connection (`MaxOpenConns=1`, `BEGIN IMMEDIATE`) plus a read
  pool whose connections are `query_only`. `Store.Writer()` and `Store.Reader()` return the two pools; on
  Postgres both are one pool. SQLite is single-replica.
- **Supported = in the CI matrix.** v1 supports SQLite and Postgres (16+). MySQL/MariaDB or
  others become supported only when the whole e2e suite runs against them in CI.

### 12.3 Web UI

- Vite + TypeScript + Svelte 5, `vis-timeline` for the timeline.
- Built to static assets, embedded in the Go binary with `go:embed`. The binary serves it at `/`.
- `web/` is the app; `npm run build` writes it to `internal/webui/dist`, which
  `internal/webui` embeds. `GET /` answers with the embedded `index.html` and every other
  `GET` the server does not route itself is looked up in the embedded files (404 when
  absent). A checkout whose web/ has not been built embeds a one-line placeholder page
  instead, so the Go packages still compile; the container build and `ci/suite.sh` build
  the real UI first.
- Asset URLs are relative, and the API base comes from the page's own path
  (`web/src/lib/apibase.ts`): the mount prefix of the page plus `/api`, always a
  path, never an absolute or protocol-relative URL, so the UI works behind a
  path-prefixing proxy.
- Styling is Tailwind CSS v4, built through `@tailwindcss/vite` (both pinned to exact versions in
  `web/package.json`; no PostCSS config, no CDN). One stylesheet, `web/src/styles.css`, holds the design
  tokens as CSS custom properties: surface, raised surface, border, text, muted text, accent, danger,
  success, warning, radius, a spacing scale (`--space-1`..`--space-8`) and the sans and mono font stacks
  (system stacks; no web fonts). The full set is defined once for the light theme (`:root`,
  `[data-theme='light']`) and once for the dark theme (`[data-theme='dark']`), the theme being the
  `data-theme` attribute on `<html>`. Its `@theme inline` block maps Tailwind's colour, radius, spacing
  and font utilities onto those properties, so components use utilities or `var(--token)` and never a
  literal colour, and a theme switch needs no rebuild. Vitest builds the UI into a scratch directory
  (`web/scripts/buildcheck`) and fails when a token is missing from either theme's block, or when a
  `src`/`href` of the built `index.html` or a `url(...)` of the built CSS is absolute, root-relative or
  protocol-relative.
- API types generated from the same OpenAPI document (`openapi-typescript`).

### 12.4 Container

- Multi-stage build; final image `FROM scratch` (or distroless static) with the binary, CA
  certificates and tzdata (embedded in the binary with `-tags timetzdata`). Runs as a non-root
  uid (65532). Multi-arch: linux/amd64, linux/arm64.
- Built by cross-compiling: every builder stage is `FROM --platform=$BUILDPLATFORM` and the Go
  build takes `GOOS=$TARGETOS GOARCH=$TARGETARCH`; the final stage only `COPY`s, so no platform is
  built under emulation. Every `FROM` is pinned by digest. CI (`ci/image-build.sh`,
  `ci/image-check.sh`, docs/ci.md) builds both platforms on every build, loads and runs the amd64
  image (`board version`, `GET /healthz`) and inspects both platforms' manifests; it pushes
  nothing (the registry push is the release job's, Phase 4). `ci/repocheck` fails a final stage
  that is not scratch or distroless static, a root user, or a builder stage without
  `--platform=$BUILDPLATFORM`.
- One volume (`/data`) for the SQLite file when SQLite is used.

### 12.5 Release artifacts

Per release: `board_<os>_<arch>.tar.gz` for linux/darwin × amd64/arm64, `checksums.txt`
(SHA-256), SBOMs, the multi-arch image, and `install.sh` / `INSTALL-AGENT.md` from the tagged
commit. Releases are immutable (§8.5, §15.3).

---

## 13. Configuration and deployment

All configuration is by environment variable:

| var | default | meaning |
|---|---|---|
| `BOARD_LISTEN` | `:8080` | listen address |
| `BOARD_DB_URL` | `sqlite:///data/board.db` | §12.2 |
| `BOARD_MIGRATE` | `auto` | `auto` \| `off` |
| `BOARD_OPERATOR_HANDLE` | `operator` | the operator actor; a session acts as this actor. Phase 0 does not seed the row at start-up: it is created by the first write a session makes (§4.1) |
| `BOARD_LEASE_DEFAULT_TTL` | `2h` | |
| `BOARD_LEASE_MAX_TTL` | `12h` | |
| `BOARD_SWEEP_INTERVAL` | `15s` | how often `board serve` expires due leases and deletes expired sessions (§5.1). A Go duration above zero; `off` (also `0` or `0s`) turns the sweeper off, and reads still expire leases lazily. Any other value exits 1 |
| `BOARD_STREAM_POLL` | `1s` | how often the change feed (§7.3) reads new `events` rows; a write through the same server wakes it at once. A Go duration above zero; any other value exits 1 |
| `BOARD_STREAM_HEARTBEAT` | `25s` | how often an idle `GET /api/v1/stream` (§7.1) sends a `: ping` comment. A Go duration above zero; any other value exits 1 |
| `BOARD_BASE_PATH` | `/` | serve under a sub-path behind a proxy |
| `BOARD_LOG_LEVEL` | `info` | |
| `BOARD_PUBLIC_URL` | (required with the issuer) | external base URL; used for the OIDC redirect URI and the CSRF origin check |
| `BOARD_OIDC_ISSUER` | (unset: browser login off) | issuer URL (discovery); setting it turns browser login on |
| `BOARD_OIDC_CLIENT_ID` | (required with the issuer) | |
| `BOARD_OIDC_CLIENT_SECRET` | (required with the issuer) | or `BOARD_OIDC_CLIENT_SECRET_FILE` |
| `BOARD_OIDC_ALLOWED_EMAILS` | (required with the issuer) | comma-separated; these log in as the operator |
| `BOARD_SESSION_TTL` | `168h` | absolute session lifetime (§3.2) |
| `BOARD_SESSION_IDLE` | `24h` | idle timeout (§3.2) |

Browser login is on when `BOARD_OIDC_ISSUER` is set: the other four OIDC settings and
`BOARD_PUBLIC_URL` are then required, a partial or malformed set refuses to start (one stderr line,
exit 1), and `BOARD_OIDC_CLIENT_SECRET_FILE` may replace the secret variable. With the issuer unset
browser login is off and only API keys authenticate (§3.3); there is no unauthenticated mode either way,
and tests use a mock provider (§3.2, §15). The board does no TLS itself: it must be served over HTTPS by a terminating
proxy, since the session cookie is `Secure`. Register the redirect URI
`<BOARD_PUBLIC_URL>/auth/oidc/callback` with the provider. Backups: the SQLite file (use the online backup command
`board backup <dest>`, which runs `VACUUM INTO`) or the Postgres database by its own tooling.

The operator's own deployment (host, network, image registry, backup schedule) is described
outside this repository.

---

## 14. Performance targets

Reference host: a 4-core arm64 board with 4 GB RAM, SQLite on flash.

| target | value |
|---|---|
| idle RSS | ≤ 40 MB |
| image size | ≤ 30 MB |
| cold start to `/readyz` | ≤ 1 s with an empty DB, ≤ 3 s with 1 M events |
| `GET /now`, `GET /attention` p99 | ≤ 20 ms at 1 M events, 50 k notes, 200 live leases |
| lease renew p99 | ≤ 10 ms |
| sustained load | 200 req/s mixed (80 % reads) without errors; 500 concurrent SSE clients |
| CLI | `board now` round trip ≤ 100 ms on a LAN; binary start ≤ 20 ms |

Hot-path rules: `last_seen_at` updates are coalesced (at most one write per actor per 30 s; Phase 0 updates it on writes only and does not coalesce, §4.1);
attention counts are cached and invalidated by the writes that change them; reads never take
the SQLite writer connection. A benchmark suite (`go test -bench`) with a seeded large dataset
runs in the extended lane and fails on a regression > 20 % against the committed baseline.

---

## 15. Testing, CI and quality gates

### 15.1 Principle

**board follows deck's CI/CD and quality gating exactly.** The reference is the deck
repository: its `Jenkinsfile`, `Jenkinsfile.release`, `ci/` scripts (`run.sh`, `Dockerfile`,
`jenkins-image.sh`, `lint.sh`, `golangci.sh`, `suite.sh`, `quality.sh`, `quality.json`,
`qualitycheck/`, `crapgate/`, `covgate/`, `allure-report.sh`, `allure-smoke.sh`,
`jenkins-report.sh`, `summary.sh`, `stability.sh`, `release-build.sh`) and its `docs/ci.md`.
board copies those mechanisms (renaming `DECK_*` to `BOARD_*`) rather than inventing new ones.
The only additions are what a web UI and a second database need: the Playwright UI e2e layer
(patterned on s3-browser's `playwright.config.ts` and `e2e/`), the database matrix, and the
frontend's coverage/CRAP/mutation gates.

### 15.2 Test layers

| layer | tool | what |
|---|---|---|
| Unit | `go test` | domain logic (lease state machine, conflicts, attention, dialect SQL builders, ULIDs/refs, identity fallback), with coverage. |
| API + CLI e2e (behaviour) | Godog feature files (Gherkin), as deck's `features/` | the real binary started on a free port against each database; steps drive the HTTP API and the CLI binary. Scenarios are the product's acceptance tests. |
| Database matrix | testcontainers-go | every e2e scenario runs against **SQLite and Postgres**; a scenario that passes on one and fails on the other fails the build. `BOARD_DB_LEG=sqlite\|postgres` selects the leg of a `features/` run (SQLite by default); the Postgres leg starts a digest-pinned Postgres 16 container through the docker socket, one database per scenario. A scenario tagged `@postgres` runs on that leg only, and one tagged `@dedicated-database` gets a private container it may stop (the `/readyz` 503 case). `ci/suite.sh` runs both legs, writes one JUnit per leg and fails on a scenario that disagrees between them. |
| UI e2e | Playwright (TypeScript), s3-browser's reporter set (html, list, allure-playwright, json, junit) and its rule of **no retries in Playwright** (flakiness is handled by the suite's retry-once policy and recorded) | the embedded UI against a seeded server: attention actions, timeline rendering and filters, notes board, archive window, SSE live updates, Swagger UI reachable. Screenshots on failure. |
| Frontend unit | Vitest + `@vitest/coverage-v8` | stores, API client wrappers, timeline data shaping. |
| Benchmarks | `go test -bench` | §14, extended lane only. |

The Playwright layer lives in `e2e/` (own `package.json`, exact pins). `e2e/playwright.config.ts`
sets `retries: 0` and the five reporters, picks a free port, and starts the real binary through
`e2e/serve.sh`: it builds the checkout's `board`, migrates a fresh temporary SQLite store, seeds
it with an API key, and serves on that port with the embedded UI. `E2E_OUT` moves every
reporter's output (default `e2e/out/`). One spec runs: `smoke.spec.ts` loads `/` and asserts the
shell rendered (the placeholder page fails it). Run one spec with
`ci/run.sh sh -c 'cd e2e && npx playwright test smoke.spec.ts'` (after `npm ci` in `e2e/`).
`ci/suite.sh` runs the whole Playwright suite after the web build under the suite-level policy:
a failed run is repeated once as a whole (every spec runs again, not only the failed ones), a
pass on the repeat is written to `flaky-playwright.txt`, a second failure fails the suite; the JUnit file is `junit-playwright.xml`.
`ci/suitecheck` fails if the config's `retries` is not 0.

Headline scenarios that define the product (must exist from the first iteration):

1. Two agents start leases on different tasks declaring the same `host:` resource → both see
   the conflict in `board now`, the operator sees it in attention; one ends → conflict clears.
2. An agent starts a lease with TTL 1 min and stops renewing → after expiry the lease is
   `expired`, appears in attention with holder and way back, and renewing returns 410.
3. An agent raises a polling ask; the operator answers in the UI → the agent's
   `board ask --poll` returns the answer; a second ask answered via `--relay` records
   `on_behalf_of=operator`, `channel=relay`.
4. Agent posts notes (task-bound, resource-bound, broadcast); another agent reads
   `notes --since last` → sees exactly the relevant summaries, `--ack` advances its cursor.
5. Task marked done by an agent appears in attention; the operator closes it → leaves the
   timeline, appears in the archive within the chosen window.
6. Auth, against a **mock OIDC provider** started by the test harness (no external identity
   provider in CI), on both database legs: allowed email logs in and acts as operator;
   disallowed or unverified email gets 403; bad `state`, reused `nonce`, missing PKCE verifier
   and a tampered ID token are rejected; a cookie write without CSRF token or with a foreign
   `Origin` is rejected; logout invalidates the session; idle and absolute expiry. Keys:
   `board admin keys create` → key works; revoke (UI, API and admin CLI) → 401; expired key →
   401; the key is never retrievable after creation; events record `principal` and actor
   separately; `/healthz` answers without credentials and nothing else does (every OpenAPI
   operation is probed unauthenticated and must return 401).

### 15.3 Pipeline

Identical to deck's `Jenkinsfile` (multibranch job, `@Library('homelab')`, agent label
`linux && amd64 && docker`, `disableConcurrentBuilds`, clean workspace):

| stage | as in deck | board specifics |
|---|---|---|
| CI image | `ci/jenkins-image.sh`, Go cache volume | image also carries Node (frontend build, Vitest, Playwright browsers) |
| Lint | `ci/run.sh ci/lint.sh`: gofmt, go vet, mod tidy drift, golangci-lint | plus ESLint, `svelte-check`, `tsc --noEmit`, OpenAPI lint, generated-code drift check, `install.sh` ↔ `INSTALL-AGENT.md` agreement (§8.5), shellcheck |
| Suite | `ci/run.sh ci/suite.sh`: gotestsum, fail-once-retry-once, flaky list, JUnit, coverage merged from the unit pass and the Godog scenarios' `-cover` board binary (a leg that ran scenarios without leaving feature counters fails the suite) | adds the Postgres leg of the matrix, Vitest, Playwright (retry-once at the suite level: a failed run is repeated whole, every spec runs again, unlike the Go and Godog reruns, which select the failed tests by name) |
| Allure smoke | `ci/allure-smoke.sh` over the Godog formatter fixture | same |
| Quality | `ci/run.sh ci/quality.sh` with `ci/quality.json` | gates in §15.4. The stage first builds the root `Dockerfile` image for linux/amd64 and linux/arm64 and checks it (`ci/image-build.sh`, `ci/image-check.sh`, §12.4, not pushed); the amd64 image feeds the image gates. The two mutation rows are reported as judged by the Mutation stage (`--mutation-deferred`), whose reports exist only after it and not on a branch build |
| Stability | extended lane only, `ci/stability.sh 3` | same |
| Mutation | — (not in deck) | every `main` build and the extended lane (branches skip it), §15.4; `ci/mutation.sh` judges efficacy and break against the same `ci/quality.json` thresholds and fails the build below them |

Extended lane (nightly on `main` + `EXTENDED` parameter): `-race`, fresh vulnerability
databases, three clean-state stability runs, benchmarks, mutation. SBOM: CycloneDX for the
module, the frontend and the image (`syft`/Trivy), generated on every build and archived;
attached to releases. Reports exactly as deck:
JUnit, Allure with per-branch history, CI summary, build summary line, README badges (`build`,
`tests`, `coverage`, `quality`), one Telegram message per build (sound only on red `main` or
nightly). A fully green `main` build archives `ci-results/release-candidate.json`.

Release: `Jenkinsfile.release` as deck's (manual job, `VERSION` + optional `CI_BUILD`, releases
exactly the candidate commit, draft-then-publish GitHub release with checksums, immutable
releases required). board's release additionally builds and pushes the multi-arch container
image (tagged `VERSION` and digest-pinned in the release notes); the registry is a job
parameter, not hard-coded.

### 15.4 Quality gates

All gates are **on from the first commit**. Thresholds live in `ci/quality.json`, read by
`ci/qualitycheck`, as in deck.

| gate | threshold |
|---|---|
| Go coverage | total ≥ 85 %, every package ≥ 80 %, fixture floor 50 % (deck's values) |
| **CRAP (Go)** | deck's `ci/crapgate`, **ceiling 8**, **fixture ceiling 8** (deck uses 10). **No baseline or exemption file**: greenfield, every function must pass. |
| **CRAP (TS/Svelte)** | scored like s3-browser's `scripts/crap` (ESLint complexity × Vitest coverage), **ceiling 8**, **no baseline file** (s3-browser's `crap-baseline.json` mechanism is not used). `web/scripts/crap` reads the ceiling from `ci/quality.json` (`crap_ts.ceiling`) and scores every function of `web/src`; Svelte logic that needs scoring lives in `web/src/lib`. |
| Frontend coverage | statements/lines ≥ 85 %, per file ≥ 80 % (aligned with the Go floors), over `web/src/lib` as Vitest measures it; read from Vitest's `coverage-summary.json` |
| golangci-lint | on (deck's config as the starting point) |
| govulncheck | on |
| Trivy | HIGH, CRITICAL fail the gate (source and the built image) |
| Mutation | Go: gremlins over `internal/domain` and `internal/store`, efficacy ≥ 80 % (killed over killed plus lived, per package, read from `ci/quality.json`), a run that generates no mutant fails (`ci/mutation.sh`, evaluated by `ci/mutationgate`); frontend: Stryker over `web/src/lib`, as s3-browser (`thresholds.break` 80 in `web/stryker.config.mjs`, per-module break 80 from `ci/quality.json`; `ci/mutation.sh` runs it after gremlins). Every `main` build and the extended lane. |
| SBOM | CycloneDX generated for module, frontend and image; a missing or empty SBOM fails the build |
| OpenAPI | spec lints clean; the generated Go server code and TypeScript types have no drift |

A gate is never weakened to make a build pass; a threshold change is a reviewed commit to
`ci/quality.json` with its reason. A gate whose input does not exist (no frontend, no image,
no OpenAPI document) fails; it never passes. `ci/qualitycheck` tests that every gate above is
present, enabled and at its threshold.

---

## 16. Delivery phases

### Phase 0: skeleton and full CI/CD (no features)

Everything below exists and the gates measure real code before any product feature is built.

- **Module layout**: `cmd/board/` (main), `internal/server/` (HTTP, generated handlers),
  `internal/store/` (dialect layer, queries), `internal/store/migrations/{common,sqlite,postgres}/`,
  `internal/domain/`, `internal/cli/`, `api/openapi.yaml`, `web/` (Vite + Svelte + TS),
  `features/` (Godog), `e2e/` (Playwright), `ci/` (deck-equivalent scripts), `Dockerfile`,
  `Jenkinsfile`, `Jenkinsfile.release`.
- **One binary** `board`: `board serve` and CLI subcommands, version stamped from build info.
- `/healthz`, `/readyz`.
- **Authentication** (§3) complete: OIDC login with the mock provider in tests, sessions, CSRF,
  API keys with `board admin keys` and the UI page. The vertical slice is authenticated from the
  start.
- **OpenAPI** spec checked in, Go server code and TS types generated, drift check; Swagger UI at
  `/api/docs`.
- **goose migrations + dialect layer** running on SQLite and Postgres.
- **One thin vertical slice**: initiatives create / list / get, via API, via CLI
  (`board init add`, `board init ls`, `board init show`), and a minimal Svelte page listing and
  creating initiatives. Enough real code for every gate to measure. The slice already follows the
  CLI output rules (§8.1): `ok` confirmation lines, one-line errors with `→ next`, the exit codes,
  list caps.
- **`board help agent`** (§8.6) with protocol **v0**: only what Phase 0 has (identity,
  `board whoami`, initiatives; Phase 1 publishes v1, see §8.6), plus `GET /meta`, the `protocol_seen` column, the golden test and
  the command/flag existence e2e. Each later phase extends the text with its commands and bumps
  the version, so the cheat sheet never describes commands that do not exist yet.
- Svelte/Vite shell embedded via `go:embed`.
- **Multi-arch container image** (amd64 + arm64), built in CI.
- **deck-equivalent `Jenkinsfile` + `ci/`**: lint (Go + TS), unit, coverage gates, CRAP 8 for Go
  and TS, mutation, Allure, API e2e matrix SQLite + Postgres, CLI e2e, Playwright UI e2e, Trivy
  HIGH/CRITICAL, SBOM, badges, Telegram notification, release candidate file.
- `Jenkinsfile.release`: placeholder that validates parameters and the candidate and stops
  (`DRY_RUN` behaviour only).

**Exit criteria**: a green multibranch build on `main` with **every gate enforced**: no gate
disabled, skipped or warn-only, and `ci/quality.json` at the §15.4 thresholds.

### Later phases

| phase | scope |
|---|---|
| 1 | **Phase 1 is shipped.** Core model: tasks (incl. watch, deps), leases (start/renew/end/expire sweep), events, resources and conflicts, short refs for every object (§8.2); `board now` (lease/conflict parts), task and lease CLI. |
| 2 | Asks (stop / poll long-poll, relay answers), notes (task / initiative / resource / broadcast, since-filters, cursors), attention view (API, CLI, UI page), SSE live feed (§7.1). |
| 3 | Timeline UI (`vis-timeline`, archive window, filters), remaining UI views. |
| 4 | CLI phase: shortcuts and their composite endpoints (§8.4), the full protocol text built on them and the staleness hint in `board start` (§8.6), `install.sh`, `INSTALL-AGENT.md` and the CI agreement check, `board update`, the real `Jenkinsfile.release` (immutable GitHub releases with binaries, checksums, SBOMs, image); the thin skill pointer (`skill/SKILL.md`, §10), identity template, idempotency; performance targets verified (§14); operator's own deployment. |

Each phase ends on a green `main` build with all gates still enforced.

## 17. Out of scope / later

- **Task queue**: tasks marked `queueable`, agents claim via lease with a `claim` flag, ready =
  open + no open deps + no live lease. Designed separately.
- **Roles / ACL** on top of v1 authentication: key scopes (read / write / operator-only actions
  such as close and answer), per-key allowed actor handles.
- **MCP endpoint** exposing the same operations as tools.
- **Asks and autonomous runners**: how a runner's own "operator input required" review relates
  to board asks (poll vs defer vs stop). Discussed separately.
- Notifications out of the board (webhooks on attention changes).
- MySQL/MariaDB support (only with CI coverage, §12.2).

---

## 18. Open questions

1. Should `done` require the actor to hold (or have held) a lease on the task, or may any actor
   mark any task done? (v1: anyone, recorded.)
2. Should an initiative have an owner actor, or only tags?
3. Retention: events and notes are kept forever in v1. Do we need an archive/compaction policy
   once the table is large?
4. Should expired-lease acknowledgement be automatic after N days?
