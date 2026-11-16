-- +goose Up
-- Indexes for the notes a check-in counts (SPEC 6.5, 14). SQLite's planner takes
-- an index on a nullable column for `col IS NULL`, and that index holds almost
-- every note: counting the broadcasts (`task_id IS NULL AND initiative_id IS
-- NULL`) walked it and read each row, ten times slower than the partial index
-- 00006 made for them. The task and initiative indexes now hold only the notes
-- that have one, which every `= ?` and `IN (...)` still uses, and the planner
-- is left with the broadcast index for the NULL case.
--
-- The count of the notes of a resource and of the notes that mention anyone
-- reads task_id, initiative_id and resource of each, so their indexes carry
-- those columns and the count never touches the table: `notes_resource` also
-- holds the task and initiative, and `notes_mentioning` holds only the notes
-- that mention anyone, with every column the count tests.
DROP INDEX notes_initiative_id;
DROP INDEX notes_task_id;
DROP INDEX notes_resource;
CREATE INDEX notes_initiative_id ON notes (initiative_id, id) WHERE initiative_id IS NOT NULL;
CREATE INDEX notes_task_id ON notes (task_id, id) WHERE task_id IS NOT NULL;
CREATE INDEX notes_resource ON notes (resource, id, task_id, initiative_id);
CREATE INDEX notes_mentioning ON notes (id, task_id, initiative_id, resource, mentions) WHERE mentions <> '[]';

-- +goose Down
DROP INDEX notes_mentioning;
DROP INDEX notes_resource;
DROP INDEX notes_task_id;
DROP INDEX notes_initiative_id;
CREATE INDEX notes_resource ON notes (resource, id);
CREATE INDEX notes_task_id ON notes (task_id, id);
CREATE INDEX notes_initiative_id ON notes (initiative_id, id);
