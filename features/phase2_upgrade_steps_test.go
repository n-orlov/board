package features

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cucumber/godog"

	"github.com/n-orlov/board/internal/store"
)

// phase1Head is the last migration the Phase 1 release shipped.
const phase1Head = 4

// seedTable is one table of a Phase 1 database: the columns the seed writes,
// one row of values for them, and every other Phase 1 column the table has,
// which the seed leaves to the schema's defaults.
type seedTable struct {
	name     string
	cols     []string
	vals     []any
	defaults []string
	key      string
}

// phase1Seed is one row in each of six tables (and the actor they refer to),
// in an order that satisfies the foreign keys. Every value differs from its
// column's default so that a migration that rewrites a column is seen.
var phase1Seed = []seedTable{
	{name: "actors", key: "id",
		cols: []string{"id", "handle", "kind", "display_name", "host", "way_back", "meta", "first_seen_at", "last_seen_at", "protocol_seen"},
		vals: []any{"ACTOR1", "op@host", "operator", "Op", "host", "ssh op", `{"os":"linux"}`, 1000, 2000, 1}, defaults: []string{"n"}},
	{name: "initiatives", key: "id",
		cols: []string{"id", "slug", "title", "description", "status", "target_start", "target_end", "created_by", "created_at", "closed_at", "closed_by", "next_task_seq"},
		vals: []any{"INIT1", "phone-onboard", "Phone", "Markdown *text*", "closed", "2026-03-01", "2026-04-01", "ACTOR1", 3000, 4000, "ACTOR1", 7}, defaults: []string{"n"}},
	{name: "tasks", key: "id",
		cols: []string{"id", "initiative_id", "seq", "title", "description", "kind", "status", "resolution", "planned_start", "planned_end", "cadence_seconds", "next_check_at", "external_refs", "created_by", "created_at", "updated_at", "done_at", "done_by", "closed_at", "closed_by"},
		vals: []any{"TASK1", "INIT1", 3, "Wire it", "details", "watch", "closed", "superseded", 3100, 3200, 900, 3300, `["gh:1"]`, "ACTOR1", 3400, 3500, 3600, "ACTOR1", 3700, "ACTOR1"}, defaults: []string{"n"}},
	{name: "leases", key: "id",
		cols: []string{"id", "task_id", "actor_id", "intent", "ttl_seconds", "started_at", "renewed_at", "expires_at", "state", "ended_at", "ended_by", "outcome", "acked_at", "acked_by"},
		vals: []any{"LEASE1", "TASK1", "ACTOR1", "do it", 3600, 4100, 4200, 4300, "ended", 4400, "ACTOR1", "done sha", 4500, "ACTOR1"}, defaults: []string{"n"}},
	{name: "api_keys", key: "id",
		cols: []string{"id", "name", "key_hash", "display_prefix", "created_at", "created_by", "last_used_at", "expires_at", "revoked_at"},
		vals: []any{"KEY1", "laptop", "hash1", "brd_ab", 5000, "admin-cli", 6000, 7000, 8000}, defaults: []string{"n"}},
	{name: "sessions", key: "id_hash",
		cols: []string{"id_hash", "email", "csrf_token", "created_at", "last_seen_at", "expires_at"},
		vals: []any{"sess1", "op@example.com", "csrf1", 9000, 9100, 9200}},
	{name: "events", key: "id",
		cols: []string{"id", "at", "actor_id", "principal", "on_behalf_of", "initiative_id", "task_id", "type", "ref_id", "summary", "data"},
		vals: []any{"EV1", 9300, "ACTOR1", "oidc:op@example.com", "ACTOR1", "INIT1", "TASK1", "initiative_created", "INIT1", "initiative phone-onboard created", `{"slug":"phone-onboard"}`}, defaults: []string{"n"}},
}

// upgradeState is what the scenario's seed step read before the migration.
type upgradeState struct {
	before map[string][][]string
}

// readColumns reads every row of table's columns as text ordered by key, each
// value tagged with its Go type so that a type change is a difference too.
func (h *harness) readColumns(ctx context.Context, s *store.Store, table, key string, cols []string) ([][]string, error) {
	rows, err := s.Reader().QueryContext(ctx, "SELECT "+strings.Join(cols, ", ")+" FROM "+table+" ORDER BY "+key)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", table, err)
	}
	defer rows.Close()
	var out [][]string
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, fmt.Errorf("scan %s: %w", table, err)
		}
		row := make([]string, len(cols))
		for i, v := range vals {
			row[i] = fmt.Sprintf("%T:%v", v, v)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// allColumns is the seeded columns followed by the ones left to defaults.
func (t seedTable) allColumns() []string {
	return append(append([]string{}, t.cols...), t.defaults...)
}

// seedPhase1 builds the schema of the Phase 1 release in the scenario's
// database, writes the seed and remembers what it reads back.
func (h *harness) seedPhase1(ctx context.Context, up *upgradeState) error {
	if err := h.freshDatabase(); err != nil {
		return err
	}
	s, err := store.Open(h.dbURL)
	if err != nil {
		return err
	}
	defer s.Close()
	if err := s.MigrateTo(ctx, phase1Head); err != nil {
		return err
	}
	v, err := s.SchemaVersion(ctx)
	if err != nil {
		return fmt.Errorf("read the schema version: %w", err)
	}
	if v != phase1Head {
		return fmt.Errorf("schema version before the seed = %d, want %d", v, phase1Head)
	}
	d := s.Dialect()
	up.before = map[string][][]string{}
	for _, t := range phase1Seed {
		if _, err := s.Writer().ExecContext(ctx, d.Rebind(d.Insert(t.name, t.cols)), t.vals...); err != nil {
			return fmt.Errorf("seed %s: %w", t.name, err)
		}
		rows, err := h.readColumns(ctx, s, t.name, t.key, t.allColumns())
		if err != nil {
			return err
		}
		if len(rows) != 1 {
			return fmt.Errorf("%s holds %d rows after the seed, want 1", t.name, len(rows))
		}
		up.before[t.name] = rows
	}
	return nil
}

// migratedPast reads the version the database records and requires it to be
// above the Phase 1 head and the whole embedded set to be applied.
func (h *harness) migratedPast(ctx context.Context, version int) error {
	s, err := store.Open(h.dbURL)
	if err != nil {
		return err
	}
	defer s.Close()
	v, err := s.SchemaVersion(ctx)
	if err != nil {
		return fmt.Errorf("read the schema version: %w", err)
	}
	if v <= int64(version) {
		return fmt.Errorf("schema version = %d, want above %d", v, version)
	}
	return s.Ready(ctx)
}

// seededRowsRead requires, for each seeded table, the same single row as before
// the migration, and in it every seeded column holding the seeded value.
func (h *harness) seededRowsRead(ctx context.Context, up *upgradeState) error {
	if up.before == nil {
		return errors.New("no Phase 1 database was seeded")
	}
	s, err := store.Open(h.dbURL)
	if err != nil {
		return err
	}
	defer s.Close()
	var problems []string
	for _, t := range phase1Seed {
		after, err := h.readColumns(ctx, s, t.name, t.key, t.allColumns())
		if err != nil {
			return err
		}
		if len(after) != 1 {
			problems = append(problems, fmt.Sprintf("%s holds %d rows, want 1", t.name, len(after)))
			continue
		}
		for i, col := range t.allColumns() {
			if after[0][i] != up.before[t.name][0][i] {
				problems = append(problems, fmt.Sprintf("%s.%s = %s, was %s before the migration", t.name, col, after[0][i], up.before[t.name][0][i]))
			}
		}
		for i, col := range t.cols {
			if got := after[0][i][strings.Index(after[0][i], ":")+1:]; got != fmt.Sprint(t.vals[i]) {
				problems = append(problems, fmt.Sprintf("%s.%s = %s, seeded %v", t.name, col, got, t.vals[i]))
			}
		}
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func registerPhase2UpgradeSteps(sc *godog.ScenarioContext, h *harness) {
	up := &upgradeState{}
	sc.Step(`^a Phase 1 database at schema version 4 holding rows in initiatives, tasks, leases, API keys, sessions and events$`,
		func(ctx context.Context) error { return h.seedPhase1(ctx, up) })
	sc.Step(`^the database has migrated past schema version (\d+)$`, h.migratedPast)
	sc.Step(`^the seeded rows of those six tables read back with identical column values$`,
		func(ctx context.Context) error { return h.seededRowsRead(ctx, up) })
}
