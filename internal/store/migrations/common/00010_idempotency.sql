-- +goose Up
-- The Idempotency-Key rows (SPEC 6.1). One row is one key of one principal:
-- `key_hash` is the SHA-256 of the principal and the key, so the key itself is
-- never stored, and `fingerprint` is the SHA-256 of the request it first came
-- with (method, target, actor and body). A row is `running` from the moment the
-- first request claims the key until the server has its answer, then `done`
-- with the answer's `status`, `content_type` and `body`; the body is sealed with
-- a key derived from the idempotency key, so a database reader cannot read it
-- (a created API key travels in one). Timestamps are UTC milliseconds (BIGINT).
-- The sweeper deletes rows older than 24 hours. The DDL is the same in both
-- dialects.
CREATE TABLE idempotency_keys (
    principal    TEXT    NOT NULL,
    key_hash     TEXT    NOT NULL,
    fingerprint  TEXT    NOT NULL,
    state        TEXT    NOT NULL CHECK (state IN ('running', 'done')),
    status       INTEGER,
    content_type TEXT,
    body         TEXT,
    created_at   BIGINT  NOT NULL,
    PRIMARY KEY (principal, key_hash)
);
CREATE INDEX idempotency_keys_created_at ON idempotency_keys (created_at);

-- +goose Down
DROP INDEX idempotency_keys_created_at;
DROP TABLE idempotency_keys;
