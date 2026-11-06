package server

import (
	"context"
	"errors"
	"net/http"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/n-orlov/board/internal/domain"
	"github.com/n-orlov/board/internal/initiatives"
)

func (p problemResponse) VisitListInitiativesResponse(w http.ResponseWriter) error { return p.write(w) }
func (p problemResponse) VisitCreateInitiativeResponse(w http.ResponseWriter) error {
	return p.write(w)
}
func (p problemResponse) VisitGetInitiativeResponse(w http.ResponseWriter) error { return p.write(w) }
func (p problemResponse) VisitPatchInitiativeResponse(w http.ResponseWriter) error {
	return p.write(w)
}

// initiativeOps is the /api/v1/initiatives operations (SPEC 6.2). Any
// authenticated principal may use them: v1 has no ACL.
type initiativeOps struct{ inits *initiatives.Store }

// ready answers 503 when the operations have no store behind them.
func (o initiativeOps) ready() (problemResponse, bool) {
	return failure(http.StatusServiceUnavailable, "initiatives are not available"), o.inits == nil
}

// withNext is p with the suggested next action next.
func (p problemResponse) withNext(next string) problemResponse {
	p.next = next
	return p
}

func date(d string) *openapi_types.Date {
	if d == "" {
		return nil
	}
	t, err := domain.ParseDate(d)
	if err != nil {
		return nil
	}
	return &openapi_types.Date{Time: t}
}

func dateText(d *openapi_types.Date) string {
	if d == nil {
		return ""
	}
	return d.Format(domain.DateLayout)
}

func optionalText(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func apiInitiative(i initiatives.Initiative) Initiative {
	return Initiative{
		Id: i.ID, Ref: i.Slug, Slug: i.Slug, Title: i.Title, Description: i.Description,
		Status: InitiativeStatus(i.Status), Tags: i.Tags,
		TargetStart: date(i.TargetStart), TargetEnd: date(i.TargetEnd),
		CreatedBy: i.CreatedBy, CreatedAt: i.CreatedAt, ClosedAt: i.ClosedAt, ClosedBy: optionalText(i.ClosedBy),
	}
}

// invalid is the 400 for a request field the store refused.
func invalid(err error) (problemResponse, bool) {
	var bad initiatives.InvalidError
	if !errors.As(err, &bad) {
		return problemResponse{}, false
	}
	return failure(http.StatusBadRequest, bad.Msg).withNext(bad.Next), true
}

// listQuery reads the query parameters of a list, or the problem to answer with.
func listQuery(p ListInitiativesParams) (initiatives.Query, *problemResponse) {
	q := initiatives.Query{}
	if p.Tag != nil {
		q.Tags = *p.Tag
	}
	if p.Limit != nil {
		q.Limit = *p.Limit
		if q.Limit == 0 {
			q.Limit = -1 // an explicit 0 is out of range, not the default
		}
	}
	if p.After != nil {
		c, err := domain.ParseCursor(*p.After)
		if err != nil {
			bad := failure(http.StatusBadRequest, err.Error()).withNext(nextCursor)
			return q, &bad
		}
		q.After = c
	}
	return q, nil
}

const nextCursor = "Leave out 'after' to start at the first page, or send the next_after of the previous page."

// listFailure is the problem a failed list is answered with, or the error when
// the failure is the server's.
func listFailure(err error) (ListInitiativesResponseObject, error) {
	if errors.Is(err, initiatives.ErrBadCursor) {
		return failure(http.StatusBadRequest, err.Error()).withNext(nextCursor), nil
	}
	if r, ok := invalid(err); ok {
		return r, nil
	}
	return nil, err
}

// ListInitiatives lists a page of initiatives.
func (o initiativeOps) ListInitiatives(ctx context.Context, req ListInitiativesRequestObject) (ListInitiativesResponseObject, error) {
	if r, off := o.ready(); off {
		return r, nil
	}
	q, bad := listQuery(req.Params)
	if bad != nil {
		return *bad, nil
	}
	page, err := o.inits.List(ctx, q)
	if err != nil {
		return listFailure(err)
	}
	items := make([]Initiative, len(page.Items))
	for i, it := range page.Items {
		items[i] = apiInitiative(it)
	}
	return ListInitiatives200JSONResponse{Items: items, NextAfter: optionalText(page.NextAfter)}, nil
}

// newInitiative is the store's request for a create body.
func newInitiative(b *InitiativeCreate) initiatives.New {
	in := initiatives.New{Slug: b.Slug, Title: b.Title, TargetStart: dateText(b.TargetStart), TargetEnd: dateText(b.TargetEnd)}
	if b.Description != nil {
		in.Description = *b.Description
	}
	if b.Tags != nil {
		in.Tags = *b.Tags
	}
	return in
}

// CreateInitiative creates an initiative and appends its event.
func (o initiativeOps) CreateInitiative(ctx context.Context, req CreateInitiativeRequestObject) (CreateInitiativeResponseObject, error) {
	if r, off := o.ready(); off {
		return r, nil
	}
	who, r, ok := caller(ctx)
	if !ok {
		return r, nil
	}
	if req.Body == nil {
		return failure(http.StatusBadRequest, "the request needs a JSON body with a slug and a title"), nil
	}
	created, err := o.inits.Create(ctx, who.AuditActor(), who.Principal, newInitiative(req.Body))
	switch {
	case errors.Is(err, initiatives.ErrExists):
		return failure(http.StatusConflict, "an initiative called "+req.Body.Slug+" already exists").
			withNext("Read it with GET /api/v1/initiatives/" + req.Body.Slug + ", or send another slug."), nil
	case err != nil:
		if r, ok := invalid(err); ok {
			return r, nil
		}
		return nil, err
	}
	return CreateInitiative201JSONResponse(apiInitiative(created)), nil
}

// missingInitiative is the 404 for a ref that names no initiative.
func missingInitiative(ref string) problemResponse {
	return failure(http.StatusNotFound, "there is no initiative "+ref).
		withNext("List initiatives with GET /api/v1/initiatives, or create one with POST /api/v1/initiatives.")
}

// GetInitiative reads one initiative by slug or ULID.
func (o initiativeOps) GetInitiative(ctx context.Context, req GetInitiativeRequestObject) (GetInitiativeResponseObject, error) {
	if r, off := o.ready(); off {
		return r, nil
	}
	got, err := o.inits.Get(ctx, req.Slug)
	switch {
	case errors.Is(err, initiatives.ErrNotFound):
		return missingInitiative(req.Slug), nil
	case err != nil:
		return nil, err
	}
	return GetInitiative200JSONResponse(apiInitiative(got)), nil
}

func initiativeField[T any](m Maybe[T]) initiatives.Field[T] {
	return initiatives.Field[T]{Set: m.Set, Null: m.Null, Value: m.Value}
}

// initiativePatch is the store's request for a patch body.
func initiativePatch(b *InitiativePatch) initiatives.Patch {
	return initiatives.Patch{
		Title: initiativeField(b.Title), Description: initiativeField(b.Description), Tags: initiativeField(b.Tags),
		TargetStart: initiativeField(b.TargetStart), TargetEnd: initiativeField(b.TargetEnd),
	}
}

// initiativePatchRefusal is the 400 for a patch body that cannot be applied, or
// nil: no body, or the immutable slug.
func initiativePatchRefusal(b *InitiativePatch) *problemResponse {
	if b == nil {
		r := failure(http.StatusBadRequest, "the request needs a JSON body with the fields to change").
			withNext("Send the fields to change, such as {\"title\": \"...\"}.")
		return &r
	}
	if b.Slug.Set {
		r := failure(http.StatusBadRequest, "slug cannot be changed").
			withNext("Leave slug out. To use another slug, create a new initiative.")
		return &r
	}
	return nil
}

// PatchInitiative edits an initiative.
func (o initiativeOps) PatchInitiative(ctx context.Context, req PatchInitiativeRequestObject) (PatchInitiativeResponseObject, error) {
	if r, off := o.ready(); off {
		return r, nil
	}
	who, r, ok := caller(ctx)
	if !ok {
		return r, nil
	}
	if refusal := initiativePatchRefusal(req.Body); refusal != nil {
		return *refusal, nil
	}
	got, err := o.inits.Patch(ctx, who.AuditActor(), who.Principal, req.Slug, initiativePatch(req.Body))
	switch {
	case errors.Is(err, initiatives.ErrNotFound):
		return missingInitiative(req.Slug), nil
	case err != nil:
		if r, ok := invalid(err); ok {
			return r, nil
		}
		return nil, err
	}
	return PatchInitiative200JSONResponse(apiInitiative(got)), nil
}
