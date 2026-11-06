package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/n-orlov/board/internal/auth"
	"github.com/n-orlov/board/internal/domain"
	"github.com/n-orlov/board/internal/leases"
	"github.com/n-orlov/board/internal/tasks"
)

func (p problemResponse) VisitCreateTaskResponse(w http.ResponseWriter) error { return p.write(w) }
func (p problemResponse) VisitGetTaskResponse(w http.ResponseWriter) error    { return p.write(w) }
func (p problemResponse) VisitPatchTaskResponse(w http.ResponseWriter) error  { return p.write(w) }
func (p problemResponse) VisitDoneTaskResponse(w http.ResponseWriter) error   { return p.write(w) }
func (p problemResponse) VisitReopenTaskResponse(w http.ResponseWriter) error { return p.write(w) }
func (p problemResponse) VisitCloseTaskResponse(w http.ResponseWriter) error  { return p.write(w) }

// taskOps is the /api/v1/tasks operations (SPEC 6.4). Any authenticated
// principal may use them: v1 has no ACL.
type taskOps struct {
	tasks  *tasks.Store
	leases *leases.Store
}

// ready answers 503 when the operations have no store behind them.
func (o taskOps) ready() (problemResponse, bool) {
	return failure(http.StatusServiceUnavailable, "tasks are not available"), o.tasks == nil || o.leases == nil
}

const nextTaskBody = "Send a JSON body; POST /api/v1/tasks needs an initiative and a title."

// taskFailure is the problem a failed task operation is answered with, or false
// when the failure is the server's. missing is the 404 detail for ErrNotFound.
func taskFailure(err error, missing string) (problemResponse, bool) {
	var bad tasks.InvalidError
	switch {
	case errors.As(err, &bad):
		return failure(http.StatusBadRequest, bad.Msg).withNext(bad.Next), true
	case errors.Is(err, tasks.ErrNoInitiative):
		return failure(http.StatusNotFound, "there is no initiative "+afterColon(err)).
			withNext("Send the slug of an initiative from GET /api/v1/initiatives, or create one with POST /api/v1/initiatives."), true
	case errors.Is(err, tasks.ErrNotFound):
		return failure(http.StatusNotFound, missing+afterColon(err)).
			withNext("Read a task with GET /api/v1/tasks/<ref>, using a ref like <initiative>-<number> or a ULID."), true
	case errors.Is(err, tasks.ErrInitiativeClosed):
		return failure(http.StatusConflict, "the initiative "+afterColon(err)+" is closed").
			withNext("Use a task of an open initiative."), true
	case errors.Is(err, tasks.ErrConflict):
		return failure(http.StatusConflict, err.Error()).withNext(conflictNext), true
	case errors.Is(err, tasks.ErrClosed):
		return failure(http.StatusConflict, "task "+afterColon(err)+" is closed").
			withNext("Reopen it with POST /api/v1/tasks/" + afterColon(err) + "/reopen, then edit it."), true
	}
	return problemResponse{}, false
}

const conflictNext = "Read the task with GET /api/v1/tasks/<ref> to see its status, then send the transition that fits it."

// afterColon is the subject a wrapped store error carries after its sentinel's
// text, such as the ref in "task not found: aa-9".
func afterColon(err error) string {
	_, subject, _ := strings.Cut(err.Error(), ": ")
	return subject
}

func apiLease(l leases.Lease) Lease {
	out := Lease{
		Id: l.ID, Ref: l.Ref, Task: l.Task, Actor: l.Actor, Intent: l.Intent, TtlSeconds: l.TTLSeconds,
		StartedAt: l.StartedAt, RenewedAt: l.RenewedAt, ExpiresAt: l.ExpiresAt, State: LeaseState(l.State),
		Resources: l.Resources, EndedAt: l.EndedAt, EndedBy: optionalText(l.EndedBy), Outcome: optionalText(l.Outcome),
		AckedAt: l.AckedAt, AckedBy: optionalText(l.AckedBy),
	}
	if out.Resources == nil {
		out.Resources = []string{}
	}
	return out
}

func apiLinks(list []tasks.Link) []TaskLink {
	out := make([]TaskLink, len(list))
	for i, l := range list {
		out[i] = TaskLink{Ref: l.Ref, Title: l.Title, Status: TaskLinkStatus(l.Status)}
	}
	return out
}

func apiExternalRefs(refs []domain.ExternalRef) []ExternalRef {
	out := make([]ExternalRef, len(refs))
	for i, r := range refs {
		out[i] = ExternalRef{Label: r.Label, Url: r.URL}
	}
	return out
}

func resolution(s string) *TaskDetailResolution {
	if s == "" {
		return nil
	}
	r := TaskDetailResolution(s)
	return &r
}

// apiTaskDetail is the document for t with its links and live leases. A task is
// active while it has a live lease.
func apiTaskDetail(t tasks.Task, links tasks.Links, live []leases.Lease) TaskDetail {
	out := TaskDetail{
		Id: t.ID, Ref: t.Ref, Initiative: t.Initiative, Seq: t.Seq, Title: t.Title, Description: t.Description,
		Kind: TaskDetailKind(t.Kind), Status: TaskDetailStatus(t.Status), Resolution: resolution(t.Resolution),
		PlannedStart: t.PlannedStart, PlannedEnd: t.PlannedEnd, CadenceSeconds: t.CadenceSeconds, NextCheckAt: t.NextCheckAt,
		ExternalRefs: apiExternalRefs(t.ExternalRefs),
		CreatedBy:    t.CreatedBy, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
		DoneAt: t.DoneAt, DoneBy: optionalText(t.DoneBy), ClosedAt: t.ClosedAt, ClosedBy: optionalText(t.ClosedBy),
		Active: len(live) > 0, Blocked: t.Blocked, Overdue: t.Overdue,
		Deps: apiLinks(links.Deps), Blocking: apiLinks(links.Blocking), LiveLeases: make([]Lease, len(live)),
	}
	for i, l := range live {
		out.LiveLeases[i] = apiLease(l)
	}
	return out
}

// detail reads what the document of t carries beyond the row.
func (o taskOps) detail(ctx context.Context, t tasks.Task) (TaskDetail, error) {
	links, err := o.tasks.Links(ctx, t)
	if err != nil {
		return TaskDetail{}, err
	}
	live, err := o.leases.LiveOf(ctx, t.ID)
	if err != nil {
		return TaskDetail{}, err
	}
	return apiTaskDetail(t, links, live), nil
}

// newTask is the store's request for a create body.
func newTask(b *TaskCreate) tasks.New {
	in := tasks.New{
		Initiative: b.Initiative, Title: b.Title, PlannedStart: b.PlannedStart, PlannedEnd: b.PlannedEnd,
		CadenceSeconds: b.CadenceSeconds, NextCheckAt: b.NextCheckAt,
	}
	if b.Description != nil {
		in.Description = *b.Description
	}
	if b.Kind != nil {
		in.Kind = string(*b.Kind)
	}
	if b.BlockedBy != nil {
		in.BlockedBy = *b.BlockedBy
	}
	if b.ExternalRefs != nil {
		in.ExternalRefs = domainRefs(*b.ExternalRefs)
	}
	return in
}

func domainRefs(refs []ExternalRef) []domain.ExternalRef {
	out := make([]domain.ExternalRef, len(refs))
	for i, r := range refs {
		out[i] = domain.ExternalRef{Label: r.Label, URL: r.Url}
	}
	return out
}

// writer is the caller of a write, or the problem to answer with: the
// operations are off, or the request carries no principal.
func (o taskOps) writer(ctx context.Context) (auth.Identity, *problemResponse) {
	if r, off := o.ready(); off {
		return auth.Identity{}, &r
	}
	who, r, ok := caller(ctx)
	if !ok {
		return auth.Identity{}, &r
	}
	return who, nil
}

// CreateTask creates a task, its events and the dependencies it starts with.
func (o taskOps) CreateTask(ctx context.Context, req CreateTaskRequestObject) (CreateTaskResponseObject, error) {
	who, bad := o.writer(ctx)
	if bad != nil {
		return *bad, nil
	}
	if req.Body == nil {
		return failure(http.StatusBadRequest, "the request needs a JSON body with an initiative and a title").withNext(nextTaskBody), nil
	}
	created, err := o.tasks.Create(ctx, who.AuditActor(), who.Principal, newTask(req.Body))
	if err != nil {
		return taskResult[CreateTaskResponseObject](err, "blocked_by names no task: ")
	}
	doc, err := o.detail(ctx, created)
	if err != nil {
		return nil, err
	}
	return CreateTask201JSONResponse(doc), nil
}

// taskResult answers a failed task operation: the problem it maps to, or the
// error itself when it is the server's.
func taskResult[R any](err error, missing string) (R, error) {
	var zero R
	if p, ok := taskFailure(err, missing); ok {
		// problemResponse implements every Visit method of the task operations
		if r, is := any(p).(R); is {
			return r, nil
		}
	}
	return zero, err
}

// GetTask reads one task by ref or ULID.
func (o taskOps) GetTask(ctx context.Context, req GetTaskRequestObject) (GetTaskResponseObject, error) {
	if r, off := o.ready(); off {
		return r, nil
	}
	got, err := o.tasks.Get(ctx, req.Ref)
	if err != nil {
		return taskResult[GetTaskResponseObject](err, "there is no task ")
	}
	doc, err := o.detail(ctx, got)
	if err != nil {
		return nil, err
	}
	return GetTask200JSONResponse(doc), nil
}

func patchField[T any](m Maybe[T]) tasks.Field[T] {
	return tasks.Field[T]{Set: m.Set, Null: m.Null, Value: m.Value}
}

// taskPatch is the store's request for a patch body.
func taskPatch(b *TaskPatch) tasks.Patch {
	p := tasks.Patch{
		Title: patchField(b.Title), Description: patchField(b.Description),
		PlannedStart: patchField(b.PlannedStart), PlannedEnd: patchField(b.PlannedEnd),
		CadenceSeconds: patchField(b.CadenceSeconds), NextCheckAt: patchField(b.NextCheckAt),
		ExternalRefs: tasks.Field[[]domain.ExternalRef]{Set: b.ExternalRefs.Set, Null: b.ExternalRefs.Null},
	}
	p.ExternalRefs.Value = domainRefs(b.ExternalRefs.Value)
	return p
}

// patchRefusal is the 400 for a patch body that cannot be applied, or nil: no
// body, or a field that is immutable.
func patchRefusal(b *TaskPatch) *problemResponse {
	if b == nil {
		r := failure(http.StatusBadRequest, "the request needs a JSON body with the fields to change").
			withNext("Send the fields to change, such as {\"title\": \"...\"}.")
		return &r
	}
	if b.Kind.Set || b.Initiative.Set {
		r := failure(http.StatusBadRequest, "kind and initiative cannot be changed").
			withNext("Leave kind and initiative out. To move work, create a task in the other initiative and close this one.")
		return &r
	}
	return nil
}

// PatchTask edits a task.
func (o taskOps) PatchTask(ctx context.Context, req PatchTaskRequestObject) (PatchTaskResponseObject, error) {
	who, bad := o.writer(ctx)
	if bad != nil {
		return *bad, nil
	}
	if refusal := patchRefusal(req.Body); refusal != nil {
		return *refusal, nil
	}
	got, err := o.tasks.Patch(ctx, who.AuditActor(), who.Principal, req.Ref, taskPatch(req.Body))
	if err != nil {
		return taskResult[PatchTaskResponseObject](err, "there is no task ")
	}
	doc, err := o.detail(ctx, got)
	if err != nil {
		return nil, err
	}
	return PatchTask200JSONResponse(doc), nil
}

// transitioned answers a transition's result: the task document, or the problem.
func transitioned[R any](ctx context.Context, o taskOps, got tasks.Task, err error, ok func(TaskDetail) R) (R, error) {
	if err != nil {
		return taskResult[R](err, "there is no task ")
	}
	doc, err := o.detail(ctx, got)
	if err != nil {
		var zero R
		return zero, err
	}
	return ok(doc), nil
}

// DoneTask marks an open task done.
func (o taskOps) DoneTask(ctx context.Context, req DoneTaskRequestObject) (DoneTaskResponseObject, error) {
	who, bad := o.writer(ctx)
	if bad != nil {
		return *bad, nil
	}
	got, err := o.tasks.Done(ctx, who.AuditActor(), who.Principal, req.Ref)
	return transitioned(ctx, o, got, err, func(d TaskDetail) DoneTaskResponseObject { return DoneTask200JSONResponse(d) })
}

// ReopenTask moves a done or closed task back to open.
func (o taskOps) ReopenTask(ctx context.Context, req ReopenTaskRequestObject) (ReopenTaskResponseObject, error) {
	who, bad := o.writer(ctx)
	if bad != nil {
		return *bad, nil
	}
	got, err := o.tasks.Reopen(ctx, who.AuditActor(), who.Principal, req.Ref)
	return transitioned(ctx, o, got, err, func(d TaskDetail) ReopenTaskResponseObject { return ReopenTask200JSONResponse(d) })
}

// CloseTask closes an open or done task with the resolution in the body. The
// Board-On-Behalf-Of header, when sent, is recorded on the task_closed event.
func (o taskOps) CloseTask(ctx context.Context, req CloseTaskRequestObject) (CloseTaskResponseObject, error) {
	who, bad := o.writer(ctx)
	if bad != nil {
		return *bad, nil
	}
	resolution := ""
	if req.Body != nil {
		resolution = req.Body.Resolution
	}
	got, err := o.tasks.Close(ctx, who.AuditActor(), who.Principal, req.Ref, resolution)
	return transitioned(ctx, o, got, err, func(d TaskDetail) CloseTaskResponseObject { return CloseTask200JSONResponse(d) })
}
