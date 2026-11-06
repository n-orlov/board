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
  on the server whatever the cookie says, and both delete its row:
  - **absolute**: `BOARD_SESSION_TTL` (default `168h`, 7 days) after login; activity does not extend it;
  - **idle**: `BOARD_SESSION_IDLE` (default `24h`) since the session was last used (a request
    refreshes its last-seen time, written at most once per tenth of the idle timeout).
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
  the signed-in page (the initiatives page) with the session's email, 401 (the session ended while
  the page was open) sends the page to `/auth/login` under the app's mount prefix, and any other
  failure is shown as an error without a redirect, so a down server cannot cause a login loop.
  `e2e/login.spec.ts` logs in through the mock provider and lands on the initiatives page.

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
  takes effect at once) and bumps `last_used_at` at most once per key per 30 s.
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
in every dialect (`store.EncodeTime` / `store.DecodeTime`); dates (`target_start`, `target_end`) are
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
| status | `open` \| `closed` | closing an initiative requires all its tasks closed (or `force=true`, which closes them `dropped`) |
| target_start, target_end | date, nullable | planning window for the timeline |
| created_by, created_at, closed_at, closed_by | | |

`initiative_tags(initiative_id, tag)`, PK both columns, index on `tag`.

### 4.3 `tasks`

| column | type | notes |
|---|---|---|
| id | ulid | |
| initiative_id | fk | |
| seq | int | per-initiative counter → `ref` |
| title | text | |
| description | text | Markdown |
| kind | `work` \| `watch` | |
| status | `open` \| `done` \| `closed` | |
| resolution | `completed` \| `dropped` \| `superseded` \| `duplicate`, nullable | set on close |
| planned_start, planned_end | ts, nullable | planned bar on the timeline |
| cadence_seconds | int, nullable | watch tasks only |
| next_check_at | ts, nullable | watch tasks only |
| external_refs | json text | list of `{label, url}` (tickets, PRs, builds) |
| created_by, created_at, updated_at, done_at, done_by, closed_at, closed_by | | |

Transitions: `open → done`, `done → open` (reopen), `open|done → closed`, `closed → open`
(reopen from archive). Every transition writes an event.

Derived, not stored: **active** (has a live lease), **blocked** (has an open dependency or an
open blocking ask), **overdue** (watch with `next_check_at < now`).

`task_deps(task_id, blocked_by_task_id)`: PK both, no self-reference, cycles rejected on insert.
A dependency is **open** while the blocking task is not `closed` (done-not-closed still blocks).

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

`lease_resources(lease_id, resource)`: PK both, index on `resource`.

An actor may hold several live leases (different tasks). Starting a lease on a task where the
same actor already has a live lease **renews** that lease instead (idempotent start).

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
`lease_started`, `lease_renewed`, `lease_ended`, `lease_expired`,
`ask_raised`, `ask_answered`, `ask_withdrawn`,
`check_recorded` (watch), `milestone` (an actor marks a notable point), `note_posted`.
`key_created` and `key_revoked` record an API key being made or revoked over HTTP (§3.3); they have no
`initiative_id` or `task_id`.

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

Broadcast = `task_id IS NULL AND initiative_id IS NULL`. A note bound to a task also writes a
`note_posted` event on that task.

### 4.8 `cursors`

`cursors(actor_id, stream, position)`: optional server-side read cursor per actor and stream
(`notes`, `events`), so an agent may ask "since my last read" without keeping state itself
(`since=cursor`). Updated only when the reader asks (`ack=true`).

### 4.9 `api_keys`, `sessions`

`api_keys(n, id, name unique, key_hash unique, display_prefix, created_at, created_by,
last_used_at, expires_at, revoked_at)`. `key_hash` = SHA-256 of the full key. `created_by` is the creating principal, `key:<name>` or `oidc:<email>` for a key made over
HTTP and `admin-cli` for one made by `board admin keys create`. `expires_at` is when the key stops working; the
verifier answers 401 `key expired` from that instant on.

`sessions(id_hash pk, email, csrf_token, created_at, last_seen_at, expires_at)`. Expired rows
are deleted by the same sweeper as leases. `sessions` has no `n`: it is keyed by `id_hash`, has no short ref
and is indexed on `expires_at` for that sweep.

Phase 0 creates `actors`, `initiatives`, `initiative_tags`, `events`, `api_keys` and `sessions`; the other
tables of this section arrive with the phases that use them. `events.task_id` is a plain nullable column until
`tasks` exists. The indexes `events(task_id, id)` and `events(initiative_id, id)` exist, as does
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
  `lease_expired`. It runs every 15 s and also lazily on every read that returns leases, so a
  read never shows a lease as live past its expiry. Expiry is idempotent and safe with several
  replicas (conditional update `WHERE state='live' AND expires_at < ?`).

### 5.2 Conflicts

Computed on read: resources held by more than one live lease. Starting or renewing a lease
returns the conflicting live leases (holder handle, task ref, intent, expires_at) in the
response body, and the CLI prints them prominently. Optional resource prefixes
(`host:` vs `repo:`) carry no special meaning to the server.

### 5.3 Attention view

`GET /api/v1/attention` returns, each list newest-relevant first, each item with the
**actors involved** (handle + way back):

1. **Open asks**: question, asker, mode (and, for `poll`, whether the asker is still polling:
   a poll is in flight or ended within the last 60 s), blocking flag, age.
2. **Done, not closed** tasks: who marked done, when, last outcome text.
3. **Expired leases** not yet acknowledged: holder, task, intent, last renewal. The operator
   acknowledges (`POST /api/v1/leases/{id}/ack`) after looking; a later lease by anyone on the
   same task also clears it.
4. **Overdue watches**: task, `next_check_at`, last check event.
5. **Live conflicts**: resource, holders, tasks.

Filters: `tag`, `initiative`. Counts per section are in the response header object so the UI
and CLI can show badges without the lists.

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
  `GET /readyz`, `GET /api/openapi.yaml`, `GET /api/openapi.json`, `GET /api/docs`, `GET /api/docs/assets/{name}`, the browser-login operations, the three `/api/v1/keys` operations (§3.3), the three `/api/v1/initiatives` operations (§6.3) and the
  RFC 9457 `Problem` schema. The binary embeds the document and serves it at
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
  the meta header are stored on the actor row by that upsert. `Board-On-Behalf-Of` is accepted and ignored in Phase 0
  (no operation yet records an on-behalf-of actor). Session requests always act as the operator.
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
| GET | `/now` | the agent check-in bundle: my live leases, live leases by others, conflicts touching my resources, open asks I raised (and answers), notes since my cursor that concern me (my tasks, my resources, mentions, broadcasts), attention counts. `?resources=` to check before starting. |
| GET | `/attention` | §5.3 |
| GET/POST | `/initiatives` | list (filters: tag, status, closed_within) / create |
| GET/PATCH | `/initiatives/{slug}` | read / update (title, description, tags, targets) |
| POST | `/initiatives/{slug}/close`, `/reopen` | |
| GET/POST | `/tasks` | list (filters: initiative, tag, status, kind, active, blocked, overdue, closed_within, q) / create |
| GET/PATCH | `/tasks/{ref}` | read (with deps, live leases, open asks) / update |
| POST | `/tasks/{ref}/done`, `/reopen`, `/close` | close takes `resolution` |
| POST/DELETE | `/tasks/{ref}/deps/{blockedByRef}` | |
| POST | `/tasks/{ref}/checks` | watch: record a check `{summary, next_check_at?}` |
| POST | `/tasks/{ref}/milestones` | `{summary, data?}` |
| GET | `/tasks/{ref}/events` | timeline |
| GET/POST | `/leases` | list (filters: state, actor, task, resource) / start |
| POST | `/leases/{id}/renew`, `/end`, `/ack` | |
| GET/POST | `/asks` | list (state, actor, task) / raise |
| GET | `/asks/{id}` | read; `?wait=<seconds>` long-poll (§7.2) |
| POST | `/asks/{id}/answer`, `/withdraw` | |
| GET/POST | `/notes` | list (filters below) / post |
| GET | `/notes/{id}` | full body |
| GET | `/events` | global change log (filters: since, initiative, task, type, actor) |
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

**Note filters**: `since=<ulid|RFC3339|cursor>`, `task=<ref>`, `initiative=<slug>`,
`resource=<r>` (repeatable), `broadcast=only|exclude`, `mention=<handle>`, `author=<handle>`,
`fields=summary` (default for lists; bodies via `/notes/{id}` or `fields=full`).

### 6.3 Initiatives

`GET/POST /api/v1/initiatives` and `GET /api/v1/initiatives/{slug}` are in `api/openapi.yaml`. The
list filters `status` and `closed_within`, PATCH, close and reopen (§6.2) come with later phases.

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
- **`GET /api/v1/initiatives`** answers `{items, next_after?}`, items ordered by id (creation order).
  `tag` (repeatable) keeps initiatives carrying every tag given. `limit` is 1 to 1000 (default 100; 0
  is a 400). `after` is a ULID or a slug in either letter case (`domain.ParseCursor`, resolved as above): the
  page starts after that initiative, a ULID that names no row pages by id, and any other text that
  names none is a 400. `next_after`, the id to send as `after`, is
  present exactly when more initiatives follow the page, so a `limit` equal to the number left is the
  last page and has none.
- An initiative is `{id, ref, slug, title, description, status, tags, target_start?, target_end?,
  created_by, created_at, closed_at?, closed_by?}`: `ref` is the slug, `tags` are sorted, `created_by`
  is the creating actor's handle, absent fields are omitted.
- Not yet served: `?view=compact` and `Idempotency-Key` replays (§6.1) for these operations. The
  server answers 503 problem documents when it runs without a store.

---

## 7. Live feed and long-poll

### 7.1 SSE

`GET /api/v1/stream?topics=events,attention`: Server-Sent Events. Each event carries the
`events.id` as the SSE `id`, so `Last-Event-ID` resumes without gaps (replayed from the table).
`attention` pushes updated counts when they change. Heartbeat comment every 25 s. The UI uses
this; nothing in the UI polls.

### 7.2 Ask long-poll

`GET /api/v1/asks/{id}?wait=N` (N ≤ 300) returns immediately if the ask is not `open`,
otherwise holds the request until it is answered/withdrawn or N seconds pass. Implemented by an
in-process notifier; with several replicas on Postgres, `LISTEN/NOTIFY` wakes the other
replicas, and on SQLite (single replica) the in-process notifier suffices. A poll in flight
marks the ask as "asker waiting" in the attention view.

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
`usage: board <serve|version|help|whoami|init|admin>`.

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
`migrate` and `keys`, or extra arguments to `migrate`, is the exit-2 usage error (`usage: board <serve|version|help|whoami|init|admin>`).

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
  `ok ask A12 raised (stop)`, `ok note N40 → host:gw1`.
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
| 3 | timed out waiting (`--poll`), the ask stays open | not yet reachable: arrives with `board ask` |
| 4 | not found (unknown ref) | `board init show` of an unknown slug, `board admin keys revoke` of an unknown name |
| 5 | authentication failed (missing, revoked or expired key) | `BOARD_API_KEY` is unset, or the server answers 401 to the key |
| 6 | state conflict (expired lease, task closed, dependency cycle) | not yet reachable: arrives with leases and tasks (a taken initiative slug is a plain error, exit 1) |
| 7 | succeeded **with a conflict warning** (`board start`, `board lease start/renew`, `board now --resources`): the action happened, live leases overlap | not yet reachable: arrives with leases |

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

Shipped so far: `version`, `serve`, `admin migrate`, `admin keys`, `whoami` and `init add|ls|show`; the other lines
below arrive with their phases. The client commands (`whoami`, `init …`) call the server at `BOARD_URL` with
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
anything else, a 409 included, is 1: Phase 0 produces only the exit codes 0, 1, 2, 4 and 5. The flags may come before or after the positional arguments, as `--flag value` or
`--flag=value`; an unknown flag or a wrong argument count exits 2 with the command's usage line on stderr.

- `board init add <slug> "<title>" [--tag t]... [--json]` creates the initiative and prints `ok initiative <slug>
  created` (`--json`: the created object instead). A taken slug, a bad slug and a bad tag each exit 1 with the server's
  message and, for a taken slug, `→ board init show <slug>`.
- `board init ls [--tag t]... [--limit N|--all] [--json]` prints one line per initiative in creation order,
  `<slug>  <status>  <title>` followed by `  [tag,tag]` when it has tags, and nothing when there are none. A tag
  given more than once keeps the initiatives that carry every tag. It shows 20 by default, `--limit N` (N ≥ 1)
  changes the cap and `--all` removes it (both together are a usage error); when more exist the last line is
  `+K more → board init ls … --all`. `--json` prints the shown initiatives as one JSON array.
- `board init show <slug> [--json]` prints the one-line form, then `targets: <start> → <end>` when it has either,
  `created by <handle> <relative time>`, `closed by <handle> <relative time>` when closed, and the description. An
  unknown slug exits 4 with `<detail> → board init ls`, whatever `next` the server sent.
- `board whoami` prints `you: <handle> (key <name>)  way back: <way back>  server ok` and exits 0. The key name is
  the one of the key whose display prefix matches `BOARD_API_KEY` in `GET /api/v1/keys`; the call also proves the
  server answers and accepts the key. A revoked or unknown key exits 5, an unreachable server exits 1.
- The usage line for an unknown subcommand is `usage: board <serve|version|help|whoami|init|admin>`.

```
board version | update [--check] [--version vX.Y.Z]
board help agent [--version]            # the agent protocol cheat sheet (§8.6)
board whoami                            # handle, way back, key name, server reachability
board now [--resources r1,r2]           # check-in: my leases, others' live leases, conflicts, my asks, new notes
board attention
board init add <slug> "<title>" [--tag t]... | ls [--tag t]... [--limit N|--all] | show <slug> | close <slug>
board task add <initiative> "<title>" [--kind watch --cadence 24h] [--after <ref>]...
board task ls [<initiative>] [--status …] | show|done|reopen <ref> | close <ref> [--resolution dropped] [--for operator]
board lease start <task> "<intent>" [--ttl 2h] [--res host:x --res repo:y]
board lease renew [<L…>|<task>] [--ttl 4h] [--intent …]      # no argument: all my live leases
board lease end   [<L…>|<task>] ["<outcome>"]
board ask <task> "<question>" [--details file|-] [--option a --option b] [--poll 10m | --stop] [--blocking]
board answer <A…> "<answer>" [--relay]
board note "<summary>" [--body file|-] [--task <ref>] [--res r] [--mention h]
board notes [--since last|<time>|<N…>] [--task <ref>] [--broadcast] [--res r] [--ack]
board note show <N…>
board milestone <task> "<summary>"
board check <task> "<summary>" [--next 24h]
board admin keys create <name> [--expires 90d] | list | revoke <name>   # direct DB, no HTTP
board admin migrate | backup <dest>
```

`board ask --poll D` prints the answer (exit 0), or exits 3 on timeout.

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

**Protocol v0** is the text of Phase 0: it covers only what the build has. It is headed
`board protocol v0` and holds the identity rules (`BOARD_URL`, `BOARD_API_KEY`, `BOARD_ACTOR`,
`BOARD_WAY_BACK`, the fallback handle, subagents using their parent's handle, `board whoami`),
the initiative commands (`board init ls` with `--tag`, `--all` and `--limit`, `board init show`,
`board init add` with `--tag`, and `--json`), the output and exit-code rules (§8.1) and a note on
`board help agent` itself. It names no command a Phase 0 build lacks: the sections above
(start, ping, block, finish, catchup, …) arrive with their commands, each bumping the version.

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
| **Attention** (default) | the five §5.3 sections; inline actions: answer ask, close task (with resolution), acknowledge expired lease, record check, open the actor's way back (copyable). |
| **Timeline** | Gantt-style, `vis-timeline`: groups = initiatives (collapsible) → tasks; per task a planned bar (light) and actual span (created → closed), lease spans as stacked sub-bars coloured by state (live / ended / expired), markers for milestones, asks, checks, done, close; dependency arrows. Zoom hour → quarter. Filters: tags, initiatives, actor, "show archived closed within N days". |
| **Initiatives** | list with tags, open/done/closed counts, live-lease count; initiative page with tasks table and its own timeline. |
| **Task** | details, deps, live leases, asks, notes, full event timeline, external links. |
| **Notes** | the public board: newest first, summaries with expand-to-body, filters (broadcast only, task, initiative, resource, author, mention). Post a note as operator. |
| **Actors** | who is alive now (live leases, last seen), with way back. |
| **Archive** | closed tasks/initiatives, adjustable window (7/30/90 days/all), reopen. |
| **Settings → API keys** | create (key shown once), list (name, prefix, created, last used, expiry), revoke (§3.3). Login / logout via OIDC (§3.2). |

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

**Phase 0 pages.** The app has two views, chosen by the URL fragment: **Initiatives** (`#/`, the
default, where a login lands) and **Settings → API keys** (`#/settings/keys`); the other views in
the table are later phases. The Initiatives view lists every initiative from `GET /api/v1/initiatives`
(following `next_after` through the pages) as one `<table>` that is a dense table from 768 px up and one
card per initiative below it (the same rows, so the `initiative-row`, `initiative-status`,
`initiative-tags` and `email` test ids keep their meaning at every width). A row shows the title with
the slug beneath it in monospace, the status as a badge (`open` success, `closed` neutral, an unknown
status neutral), the tags as chips and the creation time as a `<time datetime>` whose text is relative
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
`aria-current="page"`. The page never scrolls horizontally (`e2e/shell.spec.ts` at 390x844,
820x1180 and 1440x900), whatever text the API accepts: both list tables set `overflow-wrap: anywhere`,
and from 768 px use `table-layout: fixed` with fixed widths for the short columns (status, tags,
dates, prefix, actions) so a title, slug, tag or key name with no space in it (200, 40, 40 and 64
characters at most) wraps inside its own cell instead of widening the table; below 768 px the
cards wrap it the same way. `e2e/overflow.spec.ts` seeds the longest accepted values (wide and
narrow glyphs, 20 tags of 40 characters, a URL-like title, a 64-character key name) and asserts
`document.documentElement.scrollWidth <= clientWidth` and that no row, chip, dialog or toast reaches past the
viewport at 390, 820, 1024 and 1440 px, for both lists, the revoke confirmation, the create toast and
the once-shown key box.

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
session again, and a signed-in page that is sent to `#/signed-out` returns to `#/`. A failed logout
keeps the app and shows "Sign out failed". A reload of `#/signed-out` is answered by the server (the
fragment never reaches it), which sends an anonymous navigation to the login page (§3.2)
(`e2e/session.spec.ts`: sign in, Sign out, no navigation to `/auth/login`, the old cookie gets 401,
Sign in lands on Initiatives).

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
page issues while it loads `#/`, `#/settings/keys` and `#/signed-out` (a full page load each) at
390x844 and 1440x900 (6 visits), and requires every http(s)/ws(s) URL to have the board server's
origin. A planted remote image turns the check red.

**Touch and phone ergonomics.** At the phone width (390x844) every visible interactive element
(`a, button, input, select, textarea, [role=button]`) in the shell, the forms, the lists and the
dialogs has a hit area of at least 44x44 CSS px, and every visible `input`, `select` and `textarea`
has a computed `font-size` of at least 16 px, so iOS does not zoom on focus. The hit area is the
element's own box: a visually hidden control does not count as met by being 1 px, so the Theme
radios are transparent inputs laid over their 44 px-high labels rather than `sr-only`. Only the
skip link, clipped away (`clip-path: inset(50%)`) until it takes focus, is outside the rule while
hidden. Nothing depends on hover: every action is a button or a link. `e2e/touch.spec.ts` measures
the Initiatives, Settings -> API keys and Signed out views and the new-initiative sheet, the new-key
sheet, the revoke confirmation and the account menu opened over them, and fails with the offending
elements named when one is smaller or a field's text is under 16 px.

**Accessibility bar.** axe-core (`@axe-core/playwright`, pinned exactly in `e2e/package.json`) reports
zero violations of impact `serious` or `critical`, with no rule disabled and no element excluded, on
every one of seven states: the Initiatives view, the Settings -> API keys view, the Signed out view,
and the new-initiative sheet, the new-key sheet, the revoke confirmation and the account menu opened
over them. Each is scanned at 390x844 and 1440x900 in the light and the dark theme (the browser's
colour scheme drives the System setting): 7 x 2 x 2 = 28 scans, in `e2e/a11y.spec.ts`. A failing
scan names the rule, the impact and the offending elements. Colour contrast is met by the design
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
| `BOARD_SWEEP_INTERVAL` | `15s` | lease expiry sweep |
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
  `board whoami`, initiatives), plus `GET /meta`, the `protocol_seen` column, the golden test and
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
| 1 | Core model: tasks (incl. watch, deps), leases (start/renew/end/expire sweep), events, resources and conflicts, short refs for every object (§8.2); `board now` (lease/conflict parts), task and lease CLI. |
| 2 | Asks (stop / poll long-poll, relay answers), notes (task / initiative / resource / broadcast, since-filters, cursors), attention view (API, CLI, UI page). |
| 3 | Timeline UI (`vis-timeline`, archive window, filters), SSE live feed, remaining UI views. |
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
