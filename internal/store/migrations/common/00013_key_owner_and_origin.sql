-- +goose Up
-- Key ownership and origin (SPEC 3.1, 4.1, 4.6, 4.9). `api_keys.owner` is the
-- operator actor that owns the key; `events.origin` is the operator actor
-- behind a write (a session's own operator, or the owner of the key it came
-- through); `actors.origin` is the owner of the key behind an agent's latest
-- write. All three are nullable actor ids: null means no operator is known.
-- The DDL and the backfill are the same in both dialects.
ALTER TABLE api_keys ADD COLUMN owner TEXT REFERENCES actors (id);
ALTER TABLE events ADD COLUMN origin TEXT REFERENCES actors (id);
ALTER TABLE actors ADD COLUMN origin TEXT REFERENCES actors (id);

-- Every existing key belongs to the operator row (the earliest one, should a
-- database somehow hold several). With no operator row the owner stays null.
UPDATE api_keys SET owner =
  (SELECT id FROM actors WHERE kind = 'operator' ORDER BY first_seen_at, id LIMIT 1);

-- A session's events belong to the operator it acted as: the earliest operator
-- row, since before this migration at most one existed.
UPDATE events SET origin =
  (SELECT id FROM actors WHERE kind = 'operator' ORDER BY first_seen_at, id LIMIT 1)
WHERE principal LIKE 'oidc:%';

-- A key's events belong to the key's owner.
UPDATE events SET origin =
  (SELECT k.owner FROM api_keys k WHERE 'key:' || k.name = events.principal)
WHERE principal LIKE 'key:%';

-- An agent's origin is the origin of its latest write through a key.
UPDATE actors SET origin =
  (SELECT e.origin FROM events e WHERE e.actor_id = actors.id AND e.principal LIKE 'key:%' ORDER BY e.n DESC LIMIT 1)
WHERE kind = 'agent';

-- +goose Down
ALTER TABLE actors DROP COLUMN origin;
ALTER TABLE events DROP COLUMN origin;
ALTER TABLE api_keys DROP COLUMN owner;
