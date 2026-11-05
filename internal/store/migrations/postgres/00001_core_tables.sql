-- +goose Up
-- Timestamps are UTC milliseconds since the Unix epoch (BIGINT). JSON columns are
-- text. `n` is the never-reused integer identity behind the short ref (SPEC 4).
-- This DDL differs per dialect only in how `n` and the primary key are declared.

CREATE TABLE actors (
    n             BIGINT GENERATED ALWAYS AS IDENTITY UNIQUE,
    id            TEXT    PRIMARY KEY,
    handle        TEXT    NOT NULL UNIQUE,
    kind          TEXT    NOT NULL CHECK (kind IN ('operator', 'agent')),
    display_name  TEXT,
    host          TEXT,
    way_back      TEXT,
    meta          TEXT    NOT NULL DEFAULT '{}',
    first_seen_at BIGINT  NOT NULL,
    last_seen_at  BIGINT  NOT NULL,
    protocol_seen INTEGER
);

CREATE TABLE initiatives (
    n            BIGINT GENERATED ALWAYS AS IDENTITY UNIQUE,
    id           TEXT    PRIMARY KEY,
    slug         TEXT    NOT NULL UNIQUE,
    title        TEXT    NOT NULL,
    description  TEXT    NOT NULL DEFAULT '',
    status       TEXT    NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'closed')),
    target_start TEXT,
    target_end   TEXT,
    created_by   TEXT    NOT NULL REFERENCES actors (id),
    created_at   BIGINT  NOT NULL,
    closed_at    BIGINT,
    closed_by    TEXT    REFERENCES actors (id)
);

CREATE TABLE events (
    n             BIGINT GENERATED ALWAYS AS IDENTITY UNIQUE,
    id            TEXT    PRIMARY KEY,
    at            BIGINT  NOT NULL,
    actor_id      TEXT    NOT NULL REFERENCES actors (id),
    principal     TEXT    NOT NULL,
    on_behalf_of  TEXT    REFERENCES actors (id),
    initiative_id TEXT    REFERENCES initiatives (id),
    task_id       TEXT,
    type          TEXT    NOT NULL,
    ref_id        TEXT,
    summary       TEXT    NOT NULL,
    data          TEXT    NOT NULL DEFAULT '{}'
);

CREATE TABLE api_keys (
    n              BIGINT GENERATED ALWAYS AS IDENTITY UNIQUE,
    id             TEXT    PRIMARY KEY,
    name           TEXT    NOT NULL UNIQUE,
    key_hash       TEXT    NOT NULL UNIQUE,
    display_prefix TEXT    NOT NULL,
    created_at     BIGINT  NOT NULL,
    created_by     TEXT    NOT NULL,
    last_used_at   BIGINT,
    expires_at     BIGINT,
    revoked_at     BIGINT
);

-- +goose Down
DROP TABLE api_keys;
DROP TABLE events;
DROP TABLE initiatives;
DROP TABLE actors;
