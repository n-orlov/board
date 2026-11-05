-- +goose Up
-- Portable DDL shared by SQLite and Postgres. It runs after the per-dialect
-- tables of 00001 because it references them.

CREATE TABLE initiative_tags (
    initiative_id TEXT NOT NULL REFERENCES initiatives (id),
    tag           TEXT NOT NULL,
    PRIMARY KEY (initiative_id, tag)
);
CREATE INDEX initiative_tags_tag ON initiative_tags (tag);

CREATE TABLE sessions (
    id_hash      TEXT   PRIMARY KEY,
    email        TEXT   NOT NULL,
    csrf_token   TEXT   NOT NULL,
    created_at   BIGINT NOT NULL,
    last_seen_at BIGINT NOT NULL,
    expires_at   BIGINT NOT NULL
);
CREATE INDEX sessions_expires_at ON sessions (expires_at);

-- Exactly one operator actor may exist (SPEC 4.1).
CREATE UNIQUE INDEX actors_one_operator ON actors (kind) WHERE kind = 'operator';

CREATE INDEX events_task_id ON events (task_id, id);
CREATE INDEX events_initiative_id ON events (initiative_id, id);

-- +goose Down
DROP INDEX events_initiative_id;
DROP INDEX events_task_id;
DROP INDEX actors_one_operator;
DROP TABLE sessions;
DROP TABLE initiative_tags;
