package domain

import "sort"

// DependencyCycle reports the cycle that making task blocked by blockedBy would
// close. blockers maps a task id to the ids it is blocked by (the existing
// dependencies, without the new one). It returns the shortest path
// [task, blockedBy, ..., task], each step "is blocked by" the next, or nil when
// the new dependency closes no cycle. A self-dependency is the path
// [task, task].
func DependencyCycle(blockers map[string][]string, task, blockedBy string) []string {
	if task == blockedBy {
		return []string{task, task}
	}
	// breadth-first from blockedBy along "is blocked by"; reaching task closes the cycle.
	prev := map[string]string{blockedBy: ""}
	queue := []string{blockedBy}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		next := append([]string(nil), blockers[cur]...)
		sort.Strings(next)
		for _, n := range next {
			if _, seen := prev[n]; seen {
				continue
			}
			prev[n] = cur
			if n == task {
				return cyclePath(prev, task, blockedBy)
			}
			queue = append(queue, n)
		}
	}
	return nil
}

// cyclePath walks prev back from task to blockedBy and returns
// [task, blockedBy, ..., task].
func cyclePath(prev map[string]string, task, blockedBy string) []string {
	rev := []string{task}
	for at := prev[task]; ; at = prev[at] {
		rev = append(rev, at)
		if at == blockedBy {
			break
		}
	}
	path := []string{task}
	for i := len(rev) - 1; i >= 0; i-- {
		path = append(path, rev[i])
	}
	return path
}
