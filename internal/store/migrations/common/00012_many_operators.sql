-- +goose Up
-- Several operators may exist (SPEC 4.1): each handle a browser session acts as
-- is its own `operator` row, so the unique partial index that allowed one is
-- dropped. The `kind` column keeps its values and every row is left as it is.
-- The DDL is the same in both dialects.
DROP INDEX actors_one_operator;

-- +goose Down
-- Restoring the index needs at most one operator row, so every operator row but
-- the earliest seen (first_seen_at, then id) goes back to an agent first.
UPDATE actors SET kind = 'agent'
WHERE kind = 'operator'
  AND id <> (SELECT id FROM actors WHERE kind = 'operator' ORDER BY first_seen_at, id LIMIT 1);
CREATE UNIQUE INDEX actors_one_operator ON actors (kind) WHERE kind = 'operator';
