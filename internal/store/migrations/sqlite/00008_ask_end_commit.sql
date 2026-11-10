-- +goose Up
-- asks.end_commit_ns (SPEC 4.5): the server's real clock, in UTC nanoseconds,
-- read inside the transaction that answered or withdrew the ask as the last
-- thing before it commits, after every other statement and clock read of it.
-- It is null while the ask is open, and for an end recorded before this column
-- existed. An end whose mark is at or after an instant had not committed by
-- then, however early its answered_at or withdrawn_at; `board ask --poll`
-- judges its deadline by it (SPEC 7.2, 8.3).
ALTER TABLE asks ADD COLUMN end_commit_ns BIGINT;

-- +goose Down
ALTER TABLE asks DROP COLUMN end_commit_ns;
