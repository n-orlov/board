-- +goose Up
-- Notes and read cursors (SPEC 4.7, 4.8, 4.10). Timestamps are UTC
-- milliseconds (BIGINT), `mentions` is a JSON text list of actor handles. `n`
-- numbers the notes and forms the short ref N<n>. A note with no task and no
-- initiative is a broadcast. A cursor's position is the id of the last note
-- or event an actor read on that stream. This DDL differs per dialect only in
-- how `n` and the primary key are declared.

CREATE TABLE notes (
    n             INTEGER PRIMARY KEY AUTOINCREMENT,
    id            TEXT   NOT NULL UNIQUE,
    actor_id      TEXT   NOT NULL REFERENCES actors (id),
    summary       TEXT   NOT NULL,
    body          TEXT,
    task_id       TEXT   REFERENCES tasks (id),
    initiative_id TEXT   REFERENCES initiatives (id),
    resource      TEXT,
    mentions      TEXT   NOT NULL DEFAULT '[]',
    created_at    BIGINT NOT NULL
);
CREATE INDEX notes_task_id ON notes (task_id, id);
CREATE INDEX notes_initiative_id ON notes (initiative_id, id);
CREATE INDEX notes_resource ON notes (resource, id);
CREATE INDEX notes_broadcast ON notes (id) WHERE task_id IS NULL AND initiative_id IS NULL;

CREATE TABLE cursors (
    actor_id TEXT NOT NULL REFERENCES actors (id),
    stream   TEXT NOT NULL CHECK (stream IN ('notes', 'events')),
    position TEXT NOT NULL,
    PRIMARY KEY (actor_id, stream)
);

-- +goose Down
DROP TABLE cursors;
DROP INDEX notes_broadcast;
DROP INDEX notes_resource;
DROP INDEX notes_initiative_id;
DROP INDEX notes_task_id;
DROP TABLE notes;
