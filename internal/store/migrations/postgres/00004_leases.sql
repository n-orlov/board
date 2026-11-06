-- +goose Up
-- Leases and the resources they hold (SPEC 4.4). Timestamps are UTC
-- milliseconds (BIGINT). `n` numbers the leases and forms the short ref L<n>.
-- acked_at and acked_by record the operator's acknowledgement of an expired
-- lease. This DDL differs per dialect only in how `n` and the primary key are
-- declared.

CREATE TABLE leases (
    n           BIGINT GENERATED ALWAYS AS IDENTITY UNIQUE,
    id          TEXT    PRIMARY KEY,
    task_id     TEXT    NOT NULL REFERENCES tasks (id),
    actor_id    TEXT    NOT NULL REFERENCES actors (id),
    intent      TEXT    NOT NULL,
    ttl_seconds INTEGER NOT NULL CHECK (ttl_seconds BETWEEN 60 AND 43200),
    started_at  BIGINT  NOT NULL,
    renewed_at  BIGINT  NOT NULL,
    expires_at  BIGINT  NOT NULL,
    state       TEXT    NOT NULL DEFAULT 'live' CHECK (state IN ('live', 'ended', 'expired')),
    ended_at    BIGINT,
    ended_by    TEXT    REFERENCES actors (id),
    outcome     TEXT,
    acked_at    BIGINT,
    acked_by    TEXT    REFERENCES actors (id)
);
CREATE INDEX leases_state_expires_at ON leases (state, expires_at);
CREATE INDEX leases_task_id ON leases (task_id);
CREATE INDEX leases_actor_id ON leases (actor_id);

CREATE TABLE lease_resources (
    lease_id TEXT NOT NULL REFERENCES leases (id),
    resource TEXT NOT NULL,
    PRIMARY KEY (lease_id, resource)
);
CREATE INDEX lease_resources_resource ON lease_resources (resource);

-- +goose Down
DROP INDEX lease_resources_resource;
DROP TABLE lease_resources;
DROP INDEX leases_actor_id;
DROP INDEX leases_task_id;
DROP INDEX leases_state_expires_at;
DROP TABLE leases;
