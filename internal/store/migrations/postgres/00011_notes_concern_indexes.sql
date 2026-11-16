-- +goose Up
-- The notes that mention anyone (SPEC 6.5): a check-in counts the notes that
-- mention one handle among them, and the index keeps that off the whole table.
-- SQLite's migration of the same version also reshapes the task and initiative
-- indexes, which only its planner needs.
CREATE INDEX notes_mentioning ON notes (id) WHERE mentions <> '[]';

-- +goose Down
DROP INDEX notes_mentioning;
