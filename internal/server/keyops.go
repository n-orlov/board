package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/n-orlov/village-hall/internal/auth"
)

func (p problemResponse) VisitListKeysResponse(w http.ResponseWriter) error  { return p.write(w) }
func (p problemResponse) VisitCreateKeyResponse(w http.ResponseWriter) error { return p.write(w) }
func (p problemResponse) VisitRevokeKeyResponse(w http.ResponseWriter) error { return p.write(w) }

// keyOps is the /api/v1/keys operations (SPEC 3.3). Any authenticated
// principal may use them: v1 has no ACL.
type keyOps struct{ keys *auth.Keys }

// ready answers 503 when the operations have no store behind them.
func (o keyOps) ready() (problemResponse, bool) {
	return failure(http.StatusServiceUnavailable, "API keys are not available"), o.keys == nil
}

// caller returns who made the request, or the problem to answer with.
func caller(ctx context.Context) (auth.Identity, problemResponse, bool) {
	who, ok := identityFrom(ctx)
	return who, failure(http.StatusUnauthorized, "no principal"), ok
}

func apiKey(k auth.Key, now time.Time) Key {
	return Key{
		Id: k.ID, Name: k.Name, DisplayPrefix: k.DisplayPrefix, Status: KeyStatus(k.Status(now)),
		CreatedAt: k.CreatedAt, CreatedBy: k.CreatedBy, Owner: optionalText(k.Owner), LastUsedAt: k.LastUsedAt, ExpiresAt: k.ExpiresAt, RevokedAt: k.RevokedAt,
	}
}

// ListKeys lists every key, never the key itself.
func (o keyOps) ListKeys(ctx context.Context, _ ListKeysRequestObject) (ListKeysResponseObject, error) {
	if r, off := o.ready(); off {
		return r, nil
	}
	keys, err := o.keys.List(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	items := make([]Key, len(keys))
	for i, k := range keys {
		items[i] = apiKey(k, now)
	}
	return ListKeys200JSONResponse{Items: items}, nil
}

// expiry reads the optional expires_in of a new key.
func expiry(in *string) (time.Duration, error) {
	if in == nil {
		return 0, nil
	}
	d, err := auth.ParseExpiry(*in)
	if err != nil {
		return 0, errors.New("expires_in " + err.Error())
	}
	return d, nil
}

// CreateKey makes a key and returns it this once.
func (o keyOps) CreateKey(ctx context.Context, req CreateKeyRequestObject) (CreateKeyResponseObject, error) {
	if r, off := o.ready(); off {
		return r, nil
	}
	who, r, ok := caller(ctx)
	if !ok {
		return r, nil
	}
	if req.Body == nil {
		return failure(http.StatusBadRequest, "the request needs a JSON body with a name"), nil
	}
	ttl, err := expiry(req.Body.ExpiresIn)
	if err != nil {
		return failure(http.StatusBadRequest, err.Error()), nil
	}
	plain, row, err := o.keys.CreateAs(ctx, who, req.Body.Name, ttl)
	if err != nil {
		return createFailure(err, req.Body.Name)
	}
	k := apiKey(row, time.Now())
	return CreateKey201JSONResponse(CreatedKey{
		Id: k.Id, Name: k.Name, DisplayPrefix: k.DisplayPrefix, Status: CreatedKeyStatus(k.Status),
		CreatedAt: k.CreatedAt, CreatedBy: k.CreatedBy, Owner: k.Owner, ExpiresAt: k.ExpiresAt, Key: plain,
	}), nil
}

// createFailure maps a failed creation to the problem to answer with, or to
// the error when the failure is the server's.
func createFailure(err error, name string) (CreateKeyResponseObject, error) {
	switch {
	case errors.Is(err, auth.ErrInvalidName):
		return failure(http.StatusBadRequest, err.Error()), nil
	case errors.Is(err, auth.ErrKeyExists):
		return failure(http.StatusConflict, "a key called "+name+" already exists"), nil
	}
	return nil, err
}

// RevokeKey revokes the named key; a key already revoked stays as it was.
func (o keyOps) RevokeKey(ctx context.Context, req RevokeKeyRequestObject) (RevokeKeyResponseObject, error) {
	if r, off := o.ready(); off {
		return r, nil
	}
	who, r, ok := caller(ctx)
	if !ok {
		return r, nil
	}
	k, _, err := o.keys.RevokeAs(ctx, who, strings.TrimSpace(req.Name))
	switch {
	case errors.Is(err, auth.ErrKeyNotFound):
		return failure(http.StatusNotFound, "there is no key called "+req.Name), nil
	case err != nil:
		return nil, err
	}
	return RevokeKey200JSONResponse(apiKey(k, time.Now())), nil
}
