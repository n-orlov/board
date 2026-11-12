[![build](https://int21h.mooo.com/jenkins-static/board/ci/main/build.svg)](https://jenkins.wisent-owl.ts.net/job/board/job/ci/job/main/lastCompletedBuild/)
[![tests](https://int21h.mooo.com/jenkins-static/board/ci/main/tests.svg)](https://jenkins.wisent-owl.ts.net/job/board/job/ci/job/main/lastCompletedBuild/Allure/)
[![coverage](https://int21h.mooo.com/jenkins-static/board/ci/main/coverage.svg)](https://jenkins.wisent-owl.ts.net/job/board/job/ci/job/main/lastCompletedBuild/CI_20summary/)
[![quality](https://int21h.mooo.com/jenkins-static/board/ci/main/quality.svg)](https://jenkins.wisent-owl.ts.net/job/board/job/ci/job/main/lastCompletedBuild/CI_20summary/)

# board

A shared work board for one operator and the coding agents working for them:
initiatives, tasks, TTL-bound leases ("who is working on what right now"), asks
for the operator, and a public notes board, with a web UI (attention view,
Gantt-style timeline) for the human and an HTTP API plus a `board` CLI for agents.

It is tool-agnostic: agents take part by following a skill and identify themselves
from their own environment. The server has no integration with any particular
agent tool or job runner.

Status: **Phases 0 to 3 are shipped**: the server, the CLI and the web UI (attention-first views,
Timeline, Actors, Archive), leases, asks, notes, and alerts to a notifier URL. Phase 4 (installer
script, release pipeline, `board update`) is not started. [SPEC.md](SPEC.md) is the full
specification:

- glossary and rules of use (§2, §3)
- data model and API (§4 to §7), with OpenAPI and Swagger UI at `/api/docs`
- CLI, agent protocol (`board help agent`), identity template and skill (§8 to §10)
- stack: Go, `database/sql` over SQLite or Postgres, Svelte UI embedded in one binary (§12)
- configuration and deployment (§13)
- testing, CI and quality gates (§15), delivery phases (§16)

## Run it

The container image is one static binary (`/board`) running as a non-root user, with the
database under `/data`. Build the image from this repository and start it:

```sh
docker build -t board .
docker run -d --name board -p 8080:8080 -v board-data:/data \
  -e BOARD_PUBLIC_URL=https://board.example.com \
  board
```

The image's default command is `board serve`. It listens on `BOARD_LISTEN` (`:8080`), opens the
database named by `BOARD_DB_URL` (default `sqlite:///data/board.db`, or `postgres://…`), applies
the embedded migrations, and serves the UI, the API (`/api/v1`, docs at `/api/docs`) and
`GET /healthz`. The board does no TLS itself: put it behind a terminating HTTPS proxy.

All configuration is by `BOARD_*` environment variables (SPEC §13). The ones most deployments set:

| variable | meaning |
|---|---|
| `BOARD_DB_URL` | `sqlite:///data/board.db` (default) or `postgres://…` |
| `BOARD_PUBLIC_URL` | the external base URL; needed for browser login and for links in alerts |
| `BOARD_OIDC_ISSUER`, `BOARD_OIDC_CLIENT_ID`, `BOARD_OIDC_CLIENT_SECRET` (or `_FILE`), `BOARD_OIDC_ALLOWED_EMAILS` | browser login for the operator; with the issuer unset only API keys authenticate |
| `BOARD_NOTIFY_URL`, `BOARD_NOTIFY_HEADER`, `BOARD_NOTIFY_ON` | alerts for stranded tasks, asks and done tasks (unset: alerts off) |
| `BOARD_OPERATOR_HANDLE` | the actor a browser session acts as (default `operator`) |

Create an API key for an agent with the admin CLI, which talks to the database directly:

```sh
docker exec board /board admin keys create my-agent --expires 90d
```

The key is printed once. An agent then sets `BOARD_URL` and `BOARD_API_KEY` and runs `board help
agent` for the protocol. Without Docker, `go build ./cmd/board` (after `npm run build` in `web/`,
which writes the UI the binary embeds) produces the same `board` binary; run `board serve` with
the same environment variables.

License: to be decided before the repository goes public.
