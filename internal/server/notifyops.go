package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/n-orlov/village-hall/internal/notify"
)

func (p problemResponse) VisitListNotificationsResponse(w http.ResponseWriter) error {
	return p.write(w)
}

// notifyOps is GET /api/v1/notifications (SPEC 6.9). Any authenticated principal
// may use it: v1 has no ACL.
type notifyOps struct{ notify *notify.Store }

// ready answers 503 when the operation has no store behind it.
func (o notifyOps) ready() (problemResponse, bool) {
	return failure(http.StatusServiceUnavailable, "notifications are not available"), o.notify == nil
}

func apiNotification(n notify.Notification) Notification {
	out := Notification{
		Id: n.ID, Kind: NotificationKind(n.Kind), State: NotificationState(n.State), Event: n.Event,
		Attempts: n.Attempts, CreatedAt: n.CreatedAt, LastError: optionalText(n.LastError), Payload: n.Payload,
	}
	if !n.NextAttemptAt.IsZero() {
		out.NextAttemptAt = &n.NextAttemptAt
	}
	if !n.SentAt.IsZero() {
		out.SentAt = &n.SentAt
	}
	return out
}

// ListNotifications lists a page of the alert outbox, newest first.
func (o notifyOps) ListNotifications(ctx context.Context, req ListNotificationsRequestObject) (ListNotificationsResponseObject, error) {
	if r, off := o.ready(); off {
		return r, nil
	}
	var state *string
	if req.Params.State != nil {
		state = ptr(string(*req.Params.State))
	}
	page, err := o.notify.List(ctx, notify.Query{State: state, After: text(req.Params.After), Limit: pageLimit(req.Params.Limit)})
	var bad notify.InvalidError
	if errors.As(err, &bad) {
		return failure(http.StatusBadRequest, bad.Msg).withNext(bad.Next), nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]Notification, len(page.Items))
	for i, n := range page.Items {
		items[i] = apiNotification(n)
	}
	return ListNotifications200JSONResponse(NotificationList{Items: items, NextAfter: optionalText(page.NextAfter)}), nil
}
