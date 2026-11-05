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
  URL. Discovery via `<issuer>/.well-known/openid-configuration`; ID token signature checked
  against the issuer's JWKS (cached, refreshed on unknown `kid`).
- Flow: authorization code with **PKCE (S256)**, plus `state` and `nonce`, both single-use,
  bound to a short-lived (10 min) HttpOnly pre-login cookie. Scopes `openid email profile`.
- Callback path is fixed: **`/auth/oidc/callback`** (under `BOARD_BASE_PATH`). Logout:
  `POST /auth/logout`.
- Admission: `email_verified` must be true and the email must be in the allowed list
  (case-insensitive, exact). Anything else gets 403 and no session.
- Session: random 256-bit ID in an **HttpOnly, Secure, SameSite=Lax** cookie, path
  `BOARD_BASE_PATH`; stored server-side as SHA-256 hash (§4.9) with expiry (default 7 days
  absolute, 24 h idle), so logout and expiry are real.
- **CSRF**: every cookie-authenticated non-GET request must carry `X-CSRF-Token` matching the
  session's CSRF token (delivered to the UI by `GET /auth/session`), and an `Origin` (or
  `Referer`) equal to the public base URL. Bearer-authenticated requests are exempt (no ambient
  credential).
- Unauthenticated browser navigation redirects to login; unauthenticated API calls get 401 with
  a problem document.

### 3.3 Agents: API keys

- Sent as `Authorization: Bearer <key>`.
- Format: `brd_` + 32 random bytes from the OS CSPRNG, base32 (Crockford, lowercase), so keys
  are recognisable to secret scanners. The key is shown **once** at creation and never stored:
  the DB keeps only its SHA-256 hash, a display prefix (first 8 characters after `brd_`), name,
  `created_at`, `created_by`, `last_used_at` (coalesced, §14), `expires_at` (optional),
  `revoked_at`.
- Lookup is by hash (indexed, unique); comparison cost does not depend on the key.
- **One key per environment** (e.g. `workstation`, `ralphd`, `laptop`), not per agent, so a
  leaked environment is revoked alone. Names are unique.
- A revoked or expired key gets 401 (`key revoked` / `key expired` in the problem detail).
- Management:
  - **UI** (after SSO): Settings → API keys: create (name, optional expiry), list, revoke.
  - **Bootstrap, no HTTP**: `board admin keys create <name> [--expires 90d]`, `board admin keys
    list`, `board admin keys revoke <name>`, run inside the container against `BOARD_DB_URL`
    directly. This is how the first key is made, and the recovery path if SSO is broken.
  - API: `GET/POST /api/v1/keys`, `POST /api/v1/keys/{name}/revoke` (any principal, v1 has no ACL).
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

All timestamps are UTC, stored with millisecond precision. IDs are ULIDs (sortable by
creation time, text in every dialect). Every table except the join tables also has an integer
identity column `n` (unique, never reused) from which the **short ref** is formed (§8.2):
initiative `slug`, task `<initiative-slug>-<n>` (per-initiative counter `seq`, e.g.
`phone-onboard-3`), lease `L<n>`, ask `A<n>`, note `N<n>`, event `E<n>`.

### 4.1 `actors`

| column | type | notes |
|---|---|---|
| id | ulid | |
| handle | text, unique | stable identity string (§9) |
| kind | `operator` \| `agent` | exactly one `operator` row is seeded |
| display_name | text | optional |
| host | text | optional, from the handle |
| way_back | text | optional: command/URL to reach this actor |
| meta | json text | free-form environment facts the agent chose to send |
| first_seen_at, last_seen_at | ts | `last_seen_at` bumps on any API call with this handle (coalesced, §14) |
| protocol_seen | int, nullable | highest agent protocol version this actor has read (`board help agent`, §8.6) |

An actor row is created on first use of a handle (upsert). Actors are never deleted.

### 4.2 `initiatives`, `initiative_tags`

| column | type | notes |
|---|---|---|
| id | ulid | |
| slug | text, unique | `[a-z0-9-]{2,40}` |
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

`api_keys(id, name unique, key_hash unique, display_prefix, created_at, created_by,
last_used_at, expires_at, revoked_at)`. `key_hash` = SHA-256 of the full key.

`sessions(id_hash pk, email, csrf_token, created_at, last_seen_at, expires_at)`. Expired rows
are deleted by the same sweeper as leases.

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
- **The OpenAPI 3.1 document is the source of truth**, checked in (`api/openapi.yaml`). Server
  handler interfaces and the CLI client are generated from it (oapi-codegen); a CI check fails
  if generated code is stale. Served at `/api/openapi.yaml` and `/api/openapi.json`.
- **Swagger UI** at `/api/docs` (assets embedded, no CDN).
- Authentication per §3: session cookie (+ `X-CSRF-Token` on writes) or `Authorization: Bearer`.
  The OpenAPI document declares both schemes (`cookieAuth`, `bearerAuth`); only `/healthz` is
  unauthenticated (`/readyz` and `/metrics` too require a key or session).
- Every key-authenticated request names its actor: header `Board-Actor: <handle>` (required), optional
  `Board-Actor-Way-Back`, `Board-Actor-Meta` (JSON), `Board-On-Behalf-Of`. The actor is
  upserted on use. Session requests always act as the operator.
- Idempotency: mutating `POST`s accept `Idempotency-Key`; a replay within 24 h returns the
  original response. Agents retry safely.
- Pagination: `limit` (default 100, max 1000) + `after=<ulid|ref>` cursor. Lists are ordered by id.
- Path and query parameters accept the short ref (§8.2) or the ULID of an object; responses
  carry both (`id`, `ref`).
- Errors carry a `next` field (the suggested next action) that the CLI prints (§8.1).
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
| GET | `/readyz` | DB reachable and migrated |
| GET/POST | `/keys`, POST `/keys/{name}/revoke` | API keys (§3.3) |
| GET | `/auth/login`, `/auth/oidc/callback`; GET `/auth/session`; POST `/auth/logout` | OIDC (§3.2); outside `/api/v1` |
| GET | `/metrics` | Prometheus text |

**Note filters**: `since=<ulid|RFC3339|cursor>`, `task=<ref>`, `initiative=<slug>`,
`resource=<r>` (repeatable), `broadcast=only|exclude`, `mention=<handle>`, `author=<handle>`,
`fields=summary` (default for lists; bodies via `/notes/{id}` or `fields=full`).

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
`usage: board <serve|version>`.

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

| code | meaning |
|---|---|
| 0 | success |
| 1 | error (server, network, unexpected) |
| 2 | usage error (bad flags or arguments) |
| 3 | timed out waiting (`--poll`); the ask stays open |
| 4 | not found (unknown ref) |
| 5 | authentication failed (missing, revoked or expired key) |
| 6 | state conflict (expired lease, task closed, dependency cycle) |
| 7 | succeeded **with a conflict warning** (`board start`, `board lease start/renew`, `board now --resources`): the action happened, live leases overlap |

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

```
board version | update [--check] [--version vX.Y.Z]
board help agent [--version]            # the agent protocol cheat sheet (§8.6)
board whoami                            # handle, way back, key name, server reachability
board now [--resources r1,r2]           # check-in: my leases, others' live leases, conflicts, my asks, new notes
board attention
board init add <slug> "<title>" [--tag t]... | ls [--tag t] | show <slug> | close <slug>
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
bump it). `board help agent --version` prints just `N`. Running `board help agent` records the
version as read for the actor (`PUT /actors/me/protocol-seen`; offline it still prints).

**Staleness hint.** `GET /meta` returns the server's protocol version (the one its own binary
embeds). `board start` (and `board new`) prints at most one extra line when either is newer:

```
hint: board protocol v4 on the server, you have v3 → board update, then board help agent
hint: board protocol v4 changed since you last read it → board help agent
```

The hint never changes the exit code and never blocks the command.

**Tests.**
- Golden file: the exact text (`testdata/help-agent.golden`), updated deliberately with
  `go test -update`. A changed golden file shows up in review, where the version bump is decided.
- E2E (Godog): every `board …` command and every `--flag` the text mentions is parsed and
  resolved against the CLI's command tree; an unknown command or flag fails the build. The text
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

Global: tag filter persisted in the URL, keyboard navigation, dark/light theme, no external
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
- **Dialect layer**: one small Go interface for the differences that matter (placeholder style,
  `RETURNING`, upsert syntax, JSON columns stored as text, boolean/time encoding, partial index
  creation, `LISTEN/NOTIFY` availability). Queries are hand-written portable SQL; no ORM.
- **Migrations**: `pressly/goose` v3 with Go-embedded SQL files, one directory per dialect only
  where the DDL must differ; applied on start (`BOARD_MIGRATE=auto`, default) or by
  `board admin migrate`.
- SQLite settings: WAL, `busy_timeout=5000`, `foreign_keys=ON`, `synchronous=NORMAL`, one
  writer connection plus a read pool. SQLite is single-replica.
- **Supported = in the CI matrix.** v1 supports SQLite and Postgres (16+). MySQL/MariaDB or
  others become supported only when the whole e2e suite runs against them in CI.

### 12.3 Web UI

- Vite + TypeScript + Svelte 5, `vis-timeline` for the timeline.
- Built to static assets, embedded in the Go binary with `go:embed`. The binary serves it at `/`.
- API client generated from the same OpenAPI document (`openapi-typescript`).

### 12.4 Container

- Multi-stage build; final image `FROM scratch` (or distroless static) with the binary, CA
  certificates and tzdata. Runs as a non-root uid. Multi-arch: linux/amd64, linux/arm64.
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
| `BOARD_OPERATOR_HANDLE` | `operator` | the seeded operator actor; the UI acts as this actor |
| `BOARD_LEASE_DEFAULT_TTL` | `2h` | |
| `BOARD_LEASE_MAX_TTL` | `12h` | |
| `BOARD_SWEEP_INTERVAL` | `15s` | lease expiry sweep |
| `BOARD_BASE_PATH` | `/` | serve under a sub-path behind a proxy |
| `BOARD_LOG_LEVEL` | `info` | |
| `BOARD_PUBLIC_URL` | (required) | external base URL; used for the OIDC redirect URI and the CSRF origin check |
| `BOARD_OIDC_ISSUER` | (required) | issuer URL (discovery) |
| `BOARD_OIDC_CLIENT_ID` | (required) | |
| `BOARD_OIDC_CLIENT_SECRET` | (required) | or `BOARD_OIDC_CLIENT_SECRET_FILE` |
| `BOARD_OIDC_ALLOWED_EMAILS` | (required) | comma-separated; these log in as the operator |
| `BOARD_SESSION_TTL` | `168h` | absolute session lifetime |
| `BOARD_SESSION_IDLE` | `24h` | idle timeout |

The app refuses to start without the OIDC settings (there is no unauthenticated mode; tests use a
mock provider, §15). The board does no TLS itself: it must be served over HTTPS by a terminating
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

Hot-path rules: `last_seen_at` updates are coalesced (at most one write per actor per 30 s);
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
| Database matrix | testcontainers-go | every e2e scenario runs against **SQLite and Postgres**; a scenario that passes on one and fails on the other fails the build. |
| UI e2e | Playwright (TypeScript), s3-browser's reporter set (html, list, allure-playwright, json, junit) and its rule of **no retries in Playwright** (flakiness is handled by the suite's retry-once policy and recorded) | the embedded UI against a seeded server: attention actions, timeline rendering and filters, notes board, archive window, SSE live updates, Swagger UI reachable. Screenshots on failure. |
| Frontend unit | Vitest + `@vitest/coverage-v8` | stores, API client wrappers, timeline data shaping. |
| Benchmarks | `go test -bench` | §14, extended lane only. |

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
| Suite | `ci/run.sh ci/suite.sh`: gotestsum, fail-once-retry-once, flaky list, merged coverage, JUnit | adds the Postgres leg of the matrix, Vitest, Playwright (retry-once at the suite level, scenario-precise like deck's Godog rerun) |
| Allure smoke | `ci/allure-smoke.sh` over the Godog formatter fixture | same |
| Quality | `ci/run.sh ci/quality.sh` with `ci/quality.json` | gates in §15.4 |
| Stability | extended lane only, `ci/stability.sh 3` | same |
| Mutation | — (not in deck) | every `main` build and the extended lane (branches skip it), §15.4 |

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
| **CRAP (TS/Svelte)** | scored like s3-browser's `scripts/crap` (ESLint complexity × Vitest coverage), **ceiling 8**, **no baseline file** (s3-browser's `crap-baseline.json` mechanism is not used). |
| Frontend coverage | statements/lines ≥ 85 %, per file ≥ 80 % (aligned with the Go floors) |
| golangci-lint | on (deck's config as the starting point) |
| govulncheck | on |
| Trivy | HIGH, CRITICAL fail the gate (source and the built image) |
| Mutation | Go: gremlins over `internal/domain` and `internal/store`, efficacy ≥ 80 %, a run that generates no mutant fails; frontend: Stryker over the TS logic modules, as s3-browser (`thresholds.break` 80, per-module break 80). Every `main` build and the extended lane. |
| SBOM | CycloneDX generated for module, frontend and image; a missing or empty SBOM fails the build |
| OpenAPI | spec lints clean; generated server/client code has no drift |

A gate is never weakened to make a build pass; a threshold change is a reviewed commit to
`ci/quality.json` with its reason.

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
- **OpenAPI** spec checked in, server and TS client generated, drift check; Swagger UI at
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
