package server

import (
	"bytes"
	"context"
	"net/http"
	"time"

	"github.com/n-orlov/board/api"
)

// docsPage is the body of GET /api/docs. It references no external host.
const docsPage = `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>Board API</title></head>
<body>
<h1>Board API</h1>
<p>The OpenAPI 3.1 document is served as
<a href="/api/openapi.yaml">YAML</a> and <a href="/api/openapi.json">JSON</a>.</p>
</body>
</html>
`

// readyTimeout bounds one readiness check.
const readyTimeout = 3 * time.Second

// strictOperations adapts ssi to the generated ServerInterface. A request the
// generated code cannot read, and an operation that fails, answer with an RFC
// 9457 problem document, like every other error.
func strictOperations(ssi StrictServerInterface, middlewares ...StrictMiddlewareFunc) ServerInterface {
	return NewStrictHandlerWithOptions(ssi, middlewares, StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  requestUnreadable,
		ResponseErrorHandlerFunc: responseFailed,
	})
}

func requestUnreadable(w http.ResponseWriter, _ *http.Request, _ error) {
	writeProblem(w, http.StatusBadRequest, "the request could not be read")
}

func responseFailed(w http.ResponseWriter, _ *http.Request, _ error) {
	writeProblem(w, http.StatusInternalServerError, "the request could not be answered")
}

// operations implements the generated strict server interface.
type operations struct {
	ready Readiness
}

var _ StrictServerInterface = operations{}

func (operations) GetHealthz(context.Context, GetHealthzRequestObject) (GetHealthzResponseObject, error) {
	return GetHealthz200TextResponse("ok\n"), nil
}

func (o operations) GetReadyz(ctx context.Context, _ GetReadyzRequestObject) (GetReadyzResponseObject, error) {
	ctx, cancel := context.WithTimeout(ctx, readyTimeout)
	defer cancel()
	if err := o.ready(ctx); err != nil {
		return GetReadyz503TextResponse("not ready\n"), nil
	}
	return GetReadyz200TextResponse("ok\n"), nil
}

func (operations) GetOpenapiYAML(context.Context, GetOpenapiYAMLRequestObject) (GetOpenapiYAMLResponseObject, error) {
	doc := api.YAML()
	return GetOpenapiYAML200ApplicationyamlResponse{Body: bytes.NewReader(doc), ContentLength: int64(len(doc))}, nil
}

func (operations) GetOpenapiJSON(context.Context, GetOpenapiJSONRequestObject) (GetOpenapiJSONResponseObject, error) {
	doc, err := api.JSON()
	if err != nil {
		return nil, err
	}
	return GetOpenapiJSON200JSONResponse(doc), nil
}

func (operations) GetDocs(context.Context, GetDocsRequestObject) (GetDocsResponseObject, error) {
	return GetDocs200TexthtmlResponse{Body: bytes.NewReader([]byte(docsPage)), ContentLength: int64(len(docsPage))}, nil
}
