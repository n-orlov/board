package domain

import "strings"

// ActorKinds are the actor kinds a task can carry as a derived tag (SPEC 5.5),
// in the order the tags are listed.
var ActorKinds = []string{"ralph", "claude", "pi", "codex", "deck"}

// Where a task's tag comes from (SPEC 5.5).
const (
	TagSourceTask       = "task"
	TagSourceInitiative = "initiative"
	TagSourceActor      = "actor"
)

// ActorKind returns the actor kind of a lease holder's handle: the part before
// the first ":" when it is one of ActorKinds, else "". A handle with no ":" has
// no kind, so "ralph" alone is nothing and "ralph:x" is ralph.
func ActorKind(handle string) string {
	prefix, _, found := strings.Cut(handle, ":")
	if !found {
		return ""
	}
	for _, k := range ActorKinds {
		if prefix == k {
			return k
		}
	}
	return ""
}
