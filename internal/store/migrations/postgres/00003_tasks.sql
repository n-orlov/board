-- +goose Up
-- Tasks and their dependencies (SPEC 4.3). Timestamps are UTC milliseconds
-- (BIGINT), JSON columns are text. `seq` numbers the tasks of one initiative
-- from 1 and forms the short ref <slug>-<seq>. This DDL differs per dialect
-- only in how `n` and the primary key are declared.

-- The next seq an initiative hands out. It only counts up, so a seq is never
-- reused. No task exists yet, so every initiative starts at 1.
ALTER TABLE initiatives ADD COLUMN next_task_seq INTEGER NOT NULL DEFAULT 1;

CREATE TABLE tasks (
    n              BIGINT GENERATED ALWAYS AS IDENTITY UNIQUE,
    id             TEXT    PRIMARY KEY,
    initiative_id  TEXT    NOT NULL REFERENCES initiatives (id),
    seq            INTEGER NOT NULL CHECK (seq >= 1),
    title          TEXT    NOT NULL,
    description    TEXT    NOT NULL DEFAULT '',
    kind           TEXT    NOT NULL DEFAULT 'work' CHECK (kind IN ('work', 'watch')),
    status         TEXT    NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'done', 'closed')),
    resolution     TEXT    CHECK (resolution IN ('completed', 'dropped', 'superseded', 'duplicate')),
    planned_start  BIGINT,
    planned_end    BIGINT,
    cadence_seconds BIGINT,
    next_check_at  BIGINT,
    external_refs  TEXT    NOT NULL DEFAULT '[]',
    created_by     TEXT    NOT NULL REFERENCES actors (id),
    created_at     BIGINT  NOT NULL,
    updated_at     BIGINT  NOT NULL,
    done_at        BIGINT,
    done_by        TEXT    REFERENCES actors (id),
    closed_at      BIGINT,
    closed_by      TEXT    REFERENCES actors (id),
    UNIQUE (initiative_id, seq)
);
CREATE INDEX tasks_status_initiative ON tasks (status, initiative_id);
CREATE INDEX tasks_kind_next_check_at ON tasks (kind, next_check_at);
CREATE INDEX tasks_closed_at ON tasks (closed_at);

CREATE TABLE task_deps (
    task_id            TEXT NOT NULL REFERENCES tasks (id),
    blocked_by_task_id TEXT NOT NULL REFERENCES tasks (id),
    PRIMARY KEY (task_id, blocked_by_task_id),
    CHECK (task_id <> blocked_by_task_id)
);
CREATE INDEX task_deps_blocked_by ON task_deps (blocked_by_task_id);

-- +goose Down
DROP INDEX task_deps_blocked_by;
DROP TABLE task_deps;
DROP INDEX tasks_closed_at;
DROP INDEX tasks_kind_next_check_at;
DROP INDEX tasks_status_initiative;
DROP TABLE tasks;
ALTER TABLE initiatives DROP COLUMN next_task_seq;
