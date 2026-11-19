-- +goose Up
-- A task's own tags (SPEC 4.3). Additive: no existing row changes, and a task
-- with no row here has no tags. The DDL is the same in both dialects.
CREATE TABLE task_tags (
    task_id TEXT NOT NULL REFERENCES tasks (id),
    tag     TEXT NOT NULL,
    PRIMARY KEY (task_id, tag)
);
CREATE INDEX task_tags_tag ON task_tags (tag);

-- +goose Down
DROP TABLE task_tags;
