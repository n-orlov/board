-- +goose Up
-- asks.ended_at (SPEC 4.5): the server's clock read just after the transaction
-- that answered or withdrew the ask committed, in UTC milliseconds. It is null
-- while the ask is open, and for an end recorded before this column existed or
-- whose stamp could not be written. It bounds when the end took effect from
-- above, which answered_at and withdrawn_at (taken inside the transaction,
-- before it commits) cannot.
ALTER TABLE asks ADD COLUMN ended_at BIGINT;

-- +goose Down
ALTER TABLE asks DROP COLUMN ended_at;
