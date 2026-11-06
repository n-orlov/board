package tasks

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/n-orlov/board/internal/domain"
	"github.com/n-orlov/board/internal/store"
)

// Event types a task write appends.
const (
	EventCreated  = "task_created"
	EventUpdated  = "task_updated"
	EventDone     = "task_done"
	EventReopened = "task_reopened"
	EventClosed   = "task_closed"
)

// Errors a caller tells apart.
var (
	ErrInvalid          = errors.New("invalid task")
	ErrNotFound         = errors.New("task not found")
	ErrNoInitiative     = errors.New("initiative not found")
	ErrInitiativeClosed = errors.New("initiative is closed")
	ErrClosed           = errors.New("task is closed")
	ErrConflict         = errors.New("task status conflict")
)

// InvalidError is a field the request got wrong. It satisfies errors.Is(err,
// ErrInvalid), its text is the sentence to show the caller and Next says what
// to send instead.
type InvalidError struct{ Msg, Next string }

// Error is the sentence to show the caller.
func (e InvalidError) Error() string { return e.Msg }

// Is makes errors.Is(err, ErrInvalid) true for every InvalidError.
func (e InvalidError) Is(target error) bool { return target == ErrInvalid }

// ConflictError is a transition the task's current status does not allow. It
// satisfies errors.Is(err, ErrConflict) and its text names the current status.
type ConflictError struct{ Ref, Status, Action string }

// Error names the task, the refused action and the current status.
func (e ConflictError) Error() string {
	return fmt.Sprintf("cannot %s task %s: it is %s", e.Action, e.Ref, e.Status)
}

// Is makes errors.Is(err, ErrConflict) true for every ConflictError.
func (e ConflictError) Is(target error) bool { return target == ErrConflict }

// Task is one tasks row. Ref is `<initiative slug>-<seq>`; Initiative is the
// slug. The people are actor handles.
type Task struct {
	ID             string
	Ref            string
	Initiative     string
	InitiativeID   string
	Seq            int
	Title          string
	Description    string
	Kind           string
	Status         string
	Resolution     string
	PlannedStart   *time.Time
	PlannedEnd     *time.Time
	CadenceSeconds *int64
	NextCheckAt    *time.Time
	ExternalRefs   []domain.ExternalRef
	CreatedBy      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DoneAt         *time.Time
	DoneBy         string
	ClosedAt       *time.Time
	ClosedBy       string
}

// fields are the editable columns of a task.
type fields struct {
	Title, Description                string
	PlannedStart, PlannedEnd, NextChk *time.Time
	Cadence                           *int64
	Refs                              []domain.ExternalRef
}

func (t Task) fields() fields {
	return fields{t.Title, t.Description, t.PlannedStart, t.PlannedEnd, t.NextCheckAt, t.CadenceSeconds, t.ExternalRefs}
}

// validate checks the fields of a task of the given kind.
func (f fields) validate(kind string) error {
	for _, c := range []struct {
		err  error
		next string
	}{
		{domain.ValidateTitle(f.Title), "Send a title of 1 to 200 characters."},
		{domain.ValidateDescription(f.Description), "Shorten the description to at most 20000 characters."},
		{domain.ValidateWatchFields(kind, f.Cadence, f.NextChk), "Send cadence_seconds (60 to 31536000) and next_check_at on a watch task only."},
		{domain.ValidatePlanned(f.PlannedStart, f.PlannedEnd), "Send a planned_end that is not before planned_start."},
		{domain.ValidateExternalRefs(f.Refs), "Send at most 20 external_refs, each with a label of 1 to 80 characters and an http or https url."},
	} {
		if c.err != nil {
			return InvalidError{Msg: c.err.Error(), Next: c.next}
		}
	}
	return nil
}

// ms truncates t to the millisecond the database keeps, in UTC.
func ms(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := store.DecodeTime(store.EncodeTime(*t))
	return &v
}

func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return store.EncodeTime(*a) == store.EncodeTime(*b)
}

func sameInt(a, b *int64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func sameRefs(a, b []domain.ExternalRef) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// changed names the fields that differ between before and after, in column
// order.
func changed(before, after fields) []string {
	out := []string{}
	for _, c := range []struct {
		name string
		same bool
	}{
		{"title", before.Title == after.Title},
		{"description", before.Description == after.Description},
		{"planned_start", sameTime(before.PlannedStart, after.PlannedStart)},
		{"planned_end", sameTime(before.PlannedEnd, after.PlannedEnd)},
		{"cadence_seconds", sameInt(before.Cadence, after.Cadence)},
		{"next_check_at", sameTime(before.NextChk, after.NextChk)},
		{"external_refs", sameRefs(before.Refs, after.Refs)},
	} {
		if !c.same {
			out = append(out, c.name)
		}
	}
	return out
}

// optionalTime is the column value of an optional timestamp.
func optionalTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return store.EncodeTime(*t)
}

func optionalInt(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func timePtr(n sql.NullInt64) *time.Time {
	if !n.Valid {
		return nil
	}
	t := store.DecodeTime(n.Int64)
	return &t
}

func encodeRefs(refs []domain.ExternalRef) (string, error) {
	if refs == nil {
		refs = []domain.ExternalRef{}
	}
	b, err := json.Marshal(refs)
	if err != nil {
		return "", fmt.Errorf("encode external_refs: %w", err)
	}
	return string(b), nil
}
