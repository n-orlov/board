package initiatives

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"

	"github.com/n-orlov/village-hall/internal/audit"
	"github.com/n-orlov/village-hall/internal/domain"
)

// EventUpdated is the type of the event an edit appends.
const EventUpdated = "initiative_updated"

// Field is one field of a Patch. The zero value leaves the stored value, Set
// replaces it with Value and Set with Null clears it (where the field may be
// empty).
type Field[T any] struct {
	Set   bool
	Null  bool
	Value T
}

// Patch is the request to edit an initiative. The slug is immutable and has no
// field here.
type Patch struct {
	Title       Field[string]
	Description Field[string]
	Tags        Field[[]string]
	TargetStart Field[string]
	TargetEnd   Field[string]
}

// editable is the part of an initiative a Patch can change.
type editable struct {
	Title, Description, Start, End string
	Tags                           []string
}

func (i Initiative) editable() editable {
	return editable{Title: i.Title, Description: i.Description, Tags: i.Tags, Start: i.TargetStart, End: i.TargetEnd}
}

func dateField(cur string, f Field[string]) string {
	switch {
	case !f.Set:
		return cur
	case f.Null:
		return ""
	}
	return f.Value
}

// apply returns cur with the patch laid over it. A field that cannot be empty
// refuses null.
func (p Patch) apply(cur editable) (editable, error) {
	for _, c := range []struct {
		name      string
		set, null bool
	}{
		{"title", p.Title.Set, p.Title.Null},
		{"description", p.Description.Set, p.Description.Null},
		{"tags", p.Tags.Set, p.Tags.Null},
	} {
		if c.set && c.null {
			return editable{}, InvalidError{Msg: c.name + " cannot be null", Next: "Send " + c.name + " as a value, or leave it out to keep it."}
		}
	}
	if p.Title.Set {
		cur.Title = p.Title.Value
	}
	if p.Description.Set {
		cur.Description = p.Description.Value
	}
	if p.Tags.Set {
		cur.Tags = p.Tags.Value
	}
	cur.Start = dateField(cur.Start, p.TargetStart)
	cur.End = dateField(cur.End, p.TargetEnd)
	return cur, nil
}

// validate checks every field as a create does and returns the edit with its
// title trimmed and its tags normalised.
func (e editable) validate() (editable, error) {
	tags, tagErr := domain.NormalizeTags(e.Tags)
	for _, c := range []struct {
		err  error
		next string
	}{
		{domain.ValidateTitle(e.Title), "Send a title of 1 to 200 characters."},
		{domain.ValidateDescription(e.Description), "Shorten the description to at most 20000 characters."},
		{tagErr, NextTags},
		{domain.ValidateTargets(e.Start, e.End), "Send target_start and target_end as YYYY-MM-DD dates, the end not before the start, or null to clear one."},
	} {
		if c.err != nil {
			return editable{}, InvalidError{Msg: c.err.Error(), Next: c.next}
		}
	}
	e.Title = strings.TrimSpace(e.Title)
	e.Tags = tags
	return e, nil
}

// changed names the fields that differ between before and after, in the order
// of the document.
func changed(before, after editable) []string {
	out := []string{}
	for _, c := range []struct {
		name string
		same bool
	}{
		{"title", before.Title == after.Title},
		{"description", before.Description == after.Description},
		{"tags", slices.Equal(before.Tags, after.Tags)},
		{"target_start", before.Start == after.Start},
		{"target_end", before.End == after.End},
	} {
		if !c.same {
			out = append(out, c.name)
		}
	}
	return out
}

// Patch applies the edit to the initiative ref names and returns it. A patch
// that changes something writes the row, the tags and an initiative_updated
// event whose data.changed names the changed fields, in one transaction. A patch
// that changes nothing writes nothing, not even an actor. It returns
// ErrNotFound for an unknown initiative and ErrInvalid for a value that is
// wrong.
func (s Store) Patch(ctx context.Context, actor audit.Actor, principal, ref string, p Patch) (Initiative, error) {
	var id string
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var err error
		id, err = s.patch(ctx, tx, actor, principal, ref, p)
		return err
	})
	if err != nil {
		return Initiative{}, err
	}
	return s.getBy(ctx, "i.id", id)
}

func (s Store) patch(ctx context.Context, tx *sql.Tx, actor audit.Actor, principal, ref string, p Patch) (string, error) {
	cur, err := s.find(ctx, tx, ref)
	if err != nil {
		return "", err
	}
	next, err := p.apply(cur.editable())
	if err != nil {
		return "", err
	}
	next, err = next.validate()
	if err != nil {
		return "", err
	}
	names := changed(cur.editable(), next)
	if len(names) == 0 {
		return cur.ID, nil
	}
	return cur.ID, s.update(ctx, tx, actor, principal, cur, next, names)
}

// update writes the new fields of cur, replaces its tags when they changed and
// appends the initiative_updated event.
func (s Store) update(ctx context.Context, tx *sql.Tx, actor audit.Actor, principal string, cur Initiative, next editable, names []string) error {
	d := s.Store.Dialect()
	now := s.now()
	actorID, err := audit.UpsertActor(ctx, tx, d, actor, now)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, d.Rebind(`UPDATE initiatives SET title = ?, description = ?, target_start = ?, target_end = ? WHERE id = ?`),
		next.Title, next.Description, optional(next.Start), optional(next.End), cur.ID)
	if err != nil {
		return fmt.Errorf("update initiative: %w", err)
	}
	if slices.Contains(names, "tags") {
		if err := s.replaceTags(ctx, tx, cur.ID, next.Tags); err != nil {
			return err
		}
	}
	return audit.AppendAs(ctx, tx, d, actorID, now, audit.Event{
		Principal: principal, Type: EventUpdated, InitiativeID: cur.ID, RefID: cur.ID,
		Summary: "initiative " + cur.Slug + " updated: " + strings.Join(names, ", "),
		Data:    map[string]any{"slug": cur.Slug, "changed": names},
	})
}

// replaceTags makes tags the whole tag set of the initiative.
func (s Store) replaceTags(ctx context.Context, tx *sql.Tx, id string, tags []string) error {
	d := s.Store.Dialect()
	if _, err := tx.ExecContext(ctx, d.Rebind(`DELETE FROM initiative_tags WHERE initiative_id = ?`), id); err != nil {
		return fmt.Errorf("clear tags: %w", err)
	}
	for _, tag := range tags {
		if _, err := tx.ExecContext(ctx, d.Rebind(d.Insert("initiative_tags", []string{"initiative_id", "tag"})), id, tag); err != nil {
			return fmt.Errorf("tag initiative: %w", err)
		}
	}
	return nil
}
