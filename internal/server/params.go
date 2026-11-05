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
