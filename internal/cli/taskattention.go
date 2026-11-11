package cli

import (
	"encoding/json"
	"slices"
	"strings"
	"time"
)

// taskReason is one reason a task needs attention, as the API carries it.
type taskReason struct {
	Reason       string `json:"reason"`
	Blocking     bool   `json:"blocking"`
	AskerWaiting bool   `json:"asker_waiting"`
}

// strandedLease is the expired lease behind the stranded reason.
type strandedLease struct {
	Ref       string         `json:"ref"`
	Holder    attentionActor `json:"holder"`
	Intent    string         `json:"intent"`
	ExpiredAt string         `json:"expired_at"`
}

// marker is the `!` word a reason has on a task line.
func (r taskReason) marker() string {
	if r.Reason == "asking" && r.Blocking {
		return "!asking(blocking)"
	}
	return "!" + r.Reason
}

// markers is the markers of a task's reasons, in the order the API gives them.
func (t task) markers() []string {
	out := make([]string, 0, len(t.Attention))
	for _, r := range t.Attention {
		out = append(out, r.marker())
	}
	return out
}

// taskAttentionLine is the `attention:` line of `task show`, empty for a task that needs none.
func taskAttentionLine(t task) string {
	if len(t.Attention) == 0 {
		return ""
	}
	return "attention: " + strings.Join(t.markers(), " ") + "\n"
}

// strandedText is the `stranded:` line of `task show`, empty for a task that is not stranded.
func strandedText(t task, now time.Time) string {
	s := t.StrandedLease
	if s == nil {
		return ""
	}
	return "stranded: " + s.Ref + " by " + s.Holder.Handle + ": \"" + s.Intent + "\" expired " +
		ago(now, s.ExpiredAt) + " → board lease ack " + s.Ref + "\n"
}

// attentionRank is where a task's first reason puts it: stranded, asking (blocking first), done.
func attentionRank(t task) int {
	if len(t.Attention) == 0 {
		return 4
	}
	switch r := t.Attention[0]; {
	case r.Reason == "stranded":
		return 0
	case r.Reason == "asking" && r.Blocking:
		return 1
	case r.Reason == "asking":
		return 2
	default:
		return 3
	}
}

// orderAttention puts the tasks that need attention in the order of §8.3: stranded, then
// asking with blocking first, then done, the most recently changed first within a group.
// A task that ties on both keeps its place.
func orderAttention(items []json.RawMessage) ([]json.RawMessage, error) {
	type keyed struct {
		raw  json.RawMessage
		rank int
		at   time.Time
	}
	keys := make([]keyed, 0, len(items))
	for _, raw := range items {
		var t task
		if err := json.Unmarshal(raw, &t); err != nil {
			return nil, err
		}
		at, _ := time.Parse(time.RFC3339, t.UpdatedAt)
		keys = append(keys, keyed{raw, attentionRank(t), at})
	}
	slices.SortStableFunc(keys, func(a, b keyed) int {
		if a.rank != b.rank {
			return a.rank - b.rank
		}
		return b.at.Compare(a.at)
	})
	out := make([]json.RawMessage, len(keys))
	for i, k := range keys {
		out[i] = k.raw
	}
	return out, nil
}
