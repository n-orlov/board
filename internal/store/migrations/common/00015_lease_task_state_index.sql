-- +goose Up
-- An index that leads with the task and then the lease state and expiry (SPEC
-- 4.4), so "does this task hold a live lease" is a lookup of that task's leases
-- on both databases; without it SQLite takes leases(state, expires_at) and walks
-- every live lease once per task the Now read considers. Indexes only: no row
-- changes. The DDL is the same in both dialects.
CREATE INDEX leases_task_state_expires_at ON leases (task_id, state, expires_at);

-- +goose Down
DROP INDEX leases_task_state_expires_at;
