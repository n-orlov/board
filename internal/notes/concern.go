package notes

import (
	"context"
	"fmt"
	"strings"
)

// Concern selects the notes that concern an actor (SPEC 6.5): a note on one of
// Tasks (refs or ULIDs) or TaskIDs (ULIDs the caller already holds, which cost
// no lookup), on one of Resources, one mentioning Handle, or a broadcast, among
// those after the note After (a ULID; empty for all).
type Concern struct {
	Handle    string
	Tasks     []string
	TaskIDs   []string
	Resources []string
	After     string
}

// Concerning returns the first limit notes that concern c, oldest first and
// without bodies, and how many more there are after them. A task that names
// nothing is ErrNotFound.
func (s Store) Concerning(ctx context.Context, c Concern, limit int) ([]Note, int, error) {
	parts, err := s.concernParts(ctx, c)
	if err != nil {
		return nil, 0, err
	}
	items, err := s.rows(ctx, parts.filter(), limit, false)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.countConcerning(ctx, parts)
	if err != nil {
		return nil, 0, err
	}
	return items, total - len(items), nil
}

// countConcerning counts the notes the parts select. The OR of the four reasons
// is a table scan for the database (a note's mentions cannot be searched through
// an index), and a check-in made it on every call, so each reason is counted
// through its own index instead, as the branches of one compound select that
// never overlap: the broadcasts, and for each other reason the notes that are no
// broadcast and no note of the reasons before it. The branches run through
// covering indexes, so none reads the table (migration 00011).
func (s Store) countConcerning(ctx context.Context, p concernParts) (int, error) {
	others := p.reasons[1:]
	branches := make([]string, 0, len(p.reasons))
	var args []any
	for i, r := range others {
		where, branchArgs := r.sql, append([]any{}, r.args...)
		if !r.notBroadcast {
			where = `NOT (` + broadcastSQL + `) AND (` + r.sql + `)`
		}
		for _, earlier := range others[:i] {
			where += ` AND NOT COALESCE((` + earlier.sql + `), FALSE)`
			branchArgs = append(branchArgs, earlier.args...)
		}
		branches = append(branches, `SELECT k.id FROM notes k WHERE `+where+p.after.and())
		args = append(args, branchArgs...)
		args = append(args, p.after.args...)
	}
	branches = append(branches, `SELECT k.id FROM notes k WHERE `+broadcastSQL+p.after.and())
	args = append(args, p.after.args...)
	query := `SELECT COUNT(*) FROM (` + strings.Join(branches, ` UNION ALL `) + `) n`
	var total int
	if err := s.Store.Reader().QueryRowContext(ctx, s.Store.Dialect().Rebind(query), args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("count notes: %w", err)
	}
	return total, nil
}

// firstRows returns the first limit notes the filter selects, oldest first and
// without bodies, and how many more it selects after them.
func (s Store) firstRows(ctx context.Context, f *filter, limit int) ([]Note, int, error) {
	items, err := s.rows(ctx, f, limit, false)
	if err != nil {
		return nil, 0, err
	}
	query := `SELECT COUNT(*) FROM notes k`
	if len(f.conds) > 0 {
		query += ` WHERE ` + strings.Join(f.conds, ` AND `)
	}
	var total int
	if err := s.Store.Reader().QueryRowContext(ctx, s.Store.Dialect().Rebind(query), f.args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count notes: %w", err)
	}
	return items, total - len(items), nil
}

// reason is one way a note concerns an actor: a condition and its arguments.
// notBroadcast says the condition already keeps broadcasts out.
type reason struct {
	sql          string
	args         []any
	notBroadcast bool
}

// concernParts is a Concern as SQL: the reasons, the broadcast first and then
// the others in the order their counts are made (tasks, resources, mention),
// whose OR is a note's concerning the actor, and the after cursor's conditions,
// which the whole is AND-ed with.
type concernParts struct {
	reasons []reason
	after   filter
}

// filter is the one WHERE the parts make: the reasons OR-ed, AND-ed with after.
func (p concernParts) filter() *filter {
	conds := make([]string, len(p.reasons))
	var args []any
	for i, r := range p.reasons {
		conds[i] = r.sql
		args = append(args, r.args...)
	}
	f := &filter{}
	f.add(`(`+strings.Join(conds, ` OR `)+`)`, args...)
	f.conds = append(f.conds, p.after.conds...)
	f.args = append(f.args, p.after.args...)
	return f
}

// and is the filter's conditions to append to a WHERE that already has one:
// empty when the filter has none.
func (f filter) and() string {
	if len(f.conds) == 0 {
		return ``
	}
	return ` AND ` + strings.Join(f.conds, ` AND `)
}

// concernParts is the reasons of a Concern with the after cursor's conditions.
func (s Store) concernParts(ctx context.Context, c Concern) (concernParts, error) {
	p := concernParts{reasons: []reason{{sql: broadcastSQL}}}
	ids, err := s.taskIDs(ctx, c.Tasks)
	if err != nil {
		return concernParts{}, err
	}
	if ids = append(ids, c.TaskIDs...); len(ids) > 0 {
		marks := strings.TrimSuffix(strings.Repeat("?, ", len(ids)), ", ")
		args := make([]any, len(ids))
		for i, id := range ids {
			args[i] = id
		}
		// A note with a task is no broadcast, so this reason needs no test for it.
		p.reasons = append(p.reasons, reason{sql: `k.task_id IN (` + marks + `)`, args: args, notBroadcast: true})
	}
	others := &filter{}
	others.resources(c.Resources)
	if err := others.mention(c.Handle); err != nil {
		return concernParts{}, err
	}
	p.reasons = append(p.reasons, splitReasons(others)...)
	if c.After != "" {
		if err := after(&p.after, c.After); err != nil {
			return concernParts{}, err
		}
	}
	return p, nil
}

// splitReasons gives each condition of f the arguments it holds, in order.
func splitReasons(f *filter) []reason {
	out := make([]reason, len(f.conds))
	used := 0
	for i, cond := range f.conds {
		n := strings.Count(cond, "?")
		out[i] = reason{sql: cond, args: f.args[used : used+n]}
		used += n
	}
	return out
}

func (s Store) taskIDs(ctx context.Context, refs []string) ([]string, error) {
	ids := make([]string, len(refs))
	for i, ref := range refs {
		t, err := s.findTask(ctx, s.Store.Reader(), ref)
		if err != nil {
			return nil, err
		}
		ids[i] = t.ID
	}
	return ids, nil
}

// After returns the first limit notes after the note cursor (a ULID; empty for
// all of them), oldest first and without bodies, and how many more there are
// after them. It is what a catch-up reads.
func (s Store) After(ctx context.Context, cursor string, limit int) ([]Note, int, error) {
	f := &filter{}
	if cursor != "" {
		if err := after(f, cursor); err != nil {
			return nil, 0, err
		}
	}
	return s.firstRows(ctx, f, limit)
}
