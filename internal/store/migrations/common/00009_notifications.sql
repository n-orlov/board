-- +goose Up
-- The notifications outbox (SPEC 4, 5.4). One row is enqueued in the same
-- transaction as the event that triggers it, so an event never enqueues the
-- same kind twice (the unique index). Timestamps are UTC milliseconds
-- (BIGINT). `payload` is the JSON body a delivery POSTs. A row is `pending`
-- until a delivery gets a 2xx (`sent`) or runs out of retries (`failed`);
-- `claimed_until` is the short claim a delivering server holds on it. The DDL is the
-- same in both dialects.
CREATE TABLE notifications (
    id              TEXT    NOT NULL PRIMARY KEY,
    kind            TEXT    NOT NULL CHECK (kind IN ('stranded', 'asking', 'done')),
    event_id        TEXT    NOT NULL REFERENCES events (id),
    payload         TEXT    NOT NULL,
    state           TEXT    NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'sent', 'failed')),
    attempts        INTEGER NOT NULL DEFAULT 0,
    next_attempt_at BIGINT  NOT NULL,
    claimed_until   BIGINT,
    last_error      TEXT,
    created_at      BIGINT  NOT NULL,
    sent_at         BIGINT
);
CREATE UNIQUE INDEX notifications_event_id_kind ON notifications (event_id, kind);
CREATE INDEX notifications_state_next_attempt_at ON notifications (state, next_attempt_at);

-- +goose Down
DROP INDEX notifications_state_next_attempt_at;
DROP INDEX notifications_event_id_kind;
DROP TABLE notifications;
