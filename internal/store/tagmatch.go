package store

import (
	"strings"

	"github.com/n-orlov/village-hall/internal/domain"
)

// TagMatch is the SQL condition, with its `?` arguments, that keeps the tasks
// matching every one of tags (SPEC 6.4). taskCol is the column holding the
// task id and iniCol the one holding its initiative id; either may be NULL on
// a row, and then the sources that need it match nothing. A task matches a tag
// when the tag is one of its own (task_tags), one of its initiative's
// (initiative_tags) or, for an actor kind, derived from a holder of a lease on
// the task whose handle starts with the kind and a colon. tags are already
// normalised; with none the condition is empty.
func TagMatch(taskCol, iniCol string, tags []string) (string, []any) {
	if len(tags) == 0 {
		return "", nil
	}
	conds := make([]string, 0, len(tags))
	args := make([]any, 0, 3*len(tags))
	for _, tag := range tags {
		cond := `EXISTS (SELECT 1 FROM task_tags ttg WHERE ttg.task_id = ` + taskCol + ` AND ttg.tag = ?)` +
			` OR EXISTS (SELECT 1 FROM initiative_tags itg WHERE itg.initiative_id = ` + iniCol + ` AND itg.tag = ?)`
		args = append(args, tag, tag)
		if isActorKind(tag) {
			cond += ` OR EXISTS (SELECT 1 FROM leases lk JOIN actors ak ON ak.id = lk.actor_id WHERE lk.task_id = ` + taskCol +
				` AND SUBSTR(ak.handle, 1, ?) = ?)`
			args = append(args, len(tag)+1, tag+":")
		}
		conds = append(conds, `(`+cond+`)`)
	}
	return strings.Join(conds, ` AND `), args
}

func isActorKind(tag string) bool {
	for _, k := range domain.ActorKinds {
		if tag == k {
			return true
		}
	}
	return false
}
