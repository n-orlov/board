-- +goose Up
-- Asks (SPEC 4.5). Timestamps are UTC milliseconds (BIGINT), `options` is a
-- JSON text list. `n` numbers the asks and forms the short ref A<n>.
-- poll_seen_at is when a long poll on the ask last held or returned; it is
-- null until one has. withdrawn_at and withdraw_reason record a withdrawal.
-- This DDL differs per dialect only in how `n`, the primary key and
-- `blocking` are declared.

CREATE TABLE asks (
    n               BIGINT GENERATED ALWAYS AS IDENTITY UNIQUE,
    id              TEXT    PRIMARY KEY,
    task_id         TEXT    REFERENCES tasks (id),
    initiative_id   TEXT    REFERENCES initiatives (id),
    actor_id        TEXT    NOT NULL REFERENCES actors (id),
    question        TEXT    NOT NULL,
    details         TEXT    NOT NULL DEFAULT '',
    options         TEXT    NOT NULL DEFAULT '[]',
    mode            TEXT    NOT NULL DEFAULT 'stop' CHECK (mode IN ('stop', 'poll')),
    blocking        BOOLEAN NOT NULL DEFAULT FALSE,
    state           TEXT    NOT NULL DEFAULT 'open' CHECK (state IN ('open', 'answered', 'withdrawn')),
    answer          TEXT,
    answered_by     TEXT    REFERENCES actors (id),
    on_behalf_of    TEXT    REFERENCES actors (id),
    channel         TEXT    CHECK (channel IN ('ui', 'api', 'relay')),
    created_at      BIGINT  NOT NULL,
    answered_at     BIGINT,
    withdrawn_at    BIGINT,
    withdraw_reason TEXT,
    poll_seen_at    BIGINT
);
CREATE INDEX asks_state_created_at ON asks (state, created_at);
CREATE INDEX asks_task_id ON asks (task_id);
CREATE INDEX asks_actor_id ON asks (actor_id);

-- +goose Down
DROP INDEX asks_actor_id;
DROP INDEX asks_task_id;
DROP INDEX asks_state_created_at;
DROP TABLE asks;
