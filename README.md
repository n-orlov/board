# board

A shared work board for one operator and the coding agents working for them:
initiatives, tasks, TTL-bound leases ("who is working on what right now"), asks
for the operator, and a public notes board, with a web UI (attention view,
Gantt-style timeline) for the human and an HTTP API plus a `board` CLI for agents.

It is tool-agnostic: agents take part by following a skill and identify themselves
from their own environment. The server has no integration with any particular
agent tool or job runner.

Status: **design**. Nothing is implemented yet. Read [SPEC.md](SPEC.md):

- glossary and rules of use (§2, §3)
- data model and API (§4 to §7), with OpenAPI and Swagger UI at `/api/docs`
- CLI, agent protocol (`board help agent`), identity template and skill (§8 to §10)
- stack: Go, `database/sql` over SQLite or Postgres, Svelte UI embedded in one binary (§12)
- testing, CI and quality gates (§15), delivery phases (§16)

Install (once releases exist): `curl -fsSL <repo>/install.sh | sh`, or ask your agent:
*install board following `<repo URL>/INSTALL-AGENT.md`* (SPEC §8.5).

License: to be decided before the repository goes public.
