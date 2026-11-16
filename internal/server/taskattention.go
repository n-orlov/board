package server

import (
	"context"

	"github.com/n-orlov/village-hall/internal/attention"
	"github.com/n-orlov/village-hall/internal/domain"
)

// apiTaskAttention is the attention list of a task and the lease behind its
// stranded reason, if it has one. The list is empty, never nil, for a task that
// needs nothing.
func apiTaskAttention(r attention.TaskReasons) ([]TaskAttention, *TaskStrandedLease) {
	out := make([]TaskAttention, len(r.Reasons))
	for i, reason := range r.Reasons {
		out[i] = TaskAttention{Reason: TaskAttentionReason(reason)}
		if reason == attention.ReasonAsking {
			out[i].Blocking = flag(r.Blocking)
			out[i].AskerWaiting = flag(r.AskerWaiting)
		}
	}
	if r.Stranded == nil {
		return out, nil
	}
	l := r.Stranded
	return out, &TaskStrandedLease{
		Ref: l.Lease, Holder: apiActor(l.Holder), Intent: l.Intent, RenewedAt: l.RenewedAt, ExpiredAt: l.ExpiredAt,
	}
}

// flag is a pointer to true when on, nil otherwise, so that a flag that is not
// set is left out of the document.
func flag(on bool) *bool {
	if !on {
		return nil
	}
	return &on
}

// derived reads the reasons of the tasks in scope, none when the operations have
// no attention store. A task's reasons are read through its initiative.
func (o taskOps) derived(ctx context.Context, initiatives ...string) (attention.Derivation, error) {
	if o.reasons == nil {
		return attention.Derivation{}, nil
	}
	return o.reasons.Reasons(ctx, attention.Query{Initiatives: initiatives})
}

// needing is the refs of every task of d, the tasks that need a person.
func needing(d attention.Derivation) []string {
	refs := make([]string, len(d.Tasks))
	for i, t := range d.Tasks {
		refs[i] = t.Task
	}
	return refs
}

// attentionCounts is, per initiative slug, how many tasks of d need a person.
// A task counts once however many reasons it has.
func attentionCounts(d attention.Derivation) map[string]int {
	out := map[string]int{}
	for _, t := range d.Tasks {
		if ref, err := domain.ParseTaskRef(t.Task); err == nil {
			out[ref.Slug]++
		}
	}
	return out
}

// attentionCount is how many tasks of the initiative slug need a person, 0 when
// the operations have no attention store.
func (o initiativeOps) attentionCount(ctx context.Context, slug string) (int, error) {
	counts, err := o.attentionCounts(ctx, slug)
	return counts[slug], err
}

// attentionCounts is the counts of attentionCounts for the initiatives in scope,
// every initiative of the board when none is named. Without an attention store
// it is empty.
func (o initiativeOps) attentionCounts(ctx context.Context, slugs ...string) (map[string]int, error) {
	if o.needs == nil {
		return map[string]int{}, nil
	}
	d, err := o.needs.Reasons(ctx, attention.Query{Initiatives: slugs})
	if err != nil {
		return nil, err
	}
	return attentionCounts(d), nil
}
