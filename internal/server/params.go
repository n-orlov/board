package server

import (
	"errors"
	"net/http"

	"github.com/oapi-codegen/runtime"
)

// bindFailed answers a query parameter the generated binder could not read:
// RequiredParamError when it was missing, InvalidParamFormatError otherwise.
// ci/openapi-gen.sh rewrites the block oapi-codegen emits for this into a call
// of it, so an operation with several filters stays under the CRAP ceiling.
func (siw *ServerInterfaceWrapper) bindFailed(w http.ResponseWriter, r *http.Request, name string, err error) {
	var requiredError *runtime.RequiredParameterError
	if errors.As(err, &requiredError) {
		siw.ErrorHandlerFunc(w, r, &RequiredParamError{ParamName: name})
		return
	}
	siw.ErrorHandlerFunc(w, r, &InvalidParamFormatError{ParamName: name, Err: err})
}

// queryBinder binds the query parameters of one request. check records and answers the first
// parameter that cannot be bound and ignores the ones after it, so an operation with many
// parameters has one branch for them all (ci/openapi-gen.sh rewrites the generated code to it).
type queryBinder struct {
	siw    *ServerInterfaceWrapper
	w      http.ResponseWriter
	r      *http.Request
	failed bool
}

func (siw *ServerInterfaceWrapper) queryBinder(w http.ResponseWriter, r *http.Request) *queryBinder {
	return &queryBinder{siw: siw, w: w, r: r}
}

// check answers err for the parameter name unless an earlier parameter has already failed.
func (b *queryBinder) check(name string, err error) {
	if err == nil || b.failed {
		return
	}
	b.failed = true
	b.siw.bindFailed(b.w, b.r, name, err)
}
