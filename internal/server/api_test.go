package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/n-orlov/board/api"
	"go.yaml.in/yaml/v3"
)

func TestSpecDocumentsAreServedToAnAuthenticatedCaller(t *testing.T) {
	rec := httptest.NewRecorder()
	NewHandler(ok, acceptKey).ServeHTTP(rec, authed("/api/openapi.yaml"))
	if rec.Code != http.StatusOK || rec.Body.String() != string(api.YAML()) {
		t.Fatalf("GET /api/openapi.yaml = %d, body is not the checked-in document", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/yaml" {
		t.Errorf("GET /api/openapi.yaml Content-Type = %q, want application/yaml", ct)
	}
	var fromYAML map[string]any
	if err := yaml.Unmarshal(rec.Body.Bytes(), &fromYAML); err != nil {
		t.Fatal(err)
	}

	rec = httptest.NewRecorder()
	NewHandler(ok, acceptKey).ServeHTTP(rec, authed("/api/openapi.json"))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/openapi.json = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("GET /api/openapi.json Content-Type = %q, want application/json", ct)
	}
	var fromJSON map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &fromJSON); err != nil {
		t.Fatal(err)
	}
	// Both documents declare the same paths.
	yamlPaths, jsonPaths := fromYAML["paths"].(map[string]any), fromJSON["paths"].(map[string]any)
	if len(yamlPaths) == 0 || len(yamlPaths) != len(jsonPaths) {
		t.Fatalf("YAML declares %d paths, JSON %d", len(yamlPaths), len(jsonPaths))
	}
	for p := range yamlPaths {
		if _, ok := jsonPaths[p]; !ok {
			t.Errorf("JSON document lacks path %s", p)
		}
	}
}

var (
	externalURL = regexp.MustCompile(`https?://`)
	assetRef    = regexp.MustCompile(`(?:src|href)="([^"]+)"`)
)

func TestDocsPageIsSwaggerUIFromEmbeddedAssetsAndReferencesNoExternalHost(t *testing.T) {
	h := NewHandler(ok, acceptKey)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authed("/api/docs"))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/docs = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html" {
		t.Errorf("GET /api/docs Content-Type = %q, want text/html", ct)
	}
	page := rec.Body.String()
	if externalURL.MatchString(page) {
		t.Errorf("GET /api/docs references an external host: %q", page)
	}
	refs := assetRef.FindAllStringSubmatch(page, -1)
	if len(refs) == 0 {
		t.Fatalf("GET /api/docs references no asset: %q", page)
	}
	served := ""
	for _, ref := range refs {
		url := ref[1]
		if !strings.HasPrefix(url, "/") || strings.HasPrefix(url, "//") {
			t.Errorf("asset URL %q is not a path on this host", url)
			continue
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, authed(url))
		if rec.Code != http.StatusOK || rec.Body.Len() == 0 {
			t.Errorf("GET %s = %d with %d bytes, want 200 with a body", url, rec.Code, rec.Body.Len())
		}
		served += rec.Body.String()[:min(rec.Body.Len(), 1<<20)]
		if want := map[string]string{".js": "text/javascript", ".css": "text/css"}[url[strings.LastIndex(url, "."):]]; rec.Header().Get("Content-Type") != want {
			t.Errorf("GET %s Content-Type = %q, want %q", url, rec.Header().Get("Content-Type"), want)
		}
	}
	if !strings.Contains(served, "SwaggerUIBundle") {
		t.Error("the served assets do not contain Swagger UI")
	}
}

func TestDocsInitScriptNamesNoExternalHostAndLoadsTheSpecFromThisOne(t *testing.T) {
	rec := httptest.NewRecorder()
	NewHandler(ok, acceptKey).ServeHTTP(rec, authed("/api/docs/assets/init.js"))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET init.js = %d, want 200", rec.Code)
	}
	if externalURL.MatchString(rec.Body.String()) {
		t.Errorf("init.js references an external host: %q", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "'/api/openapi.json'") {
		t.Errorf("init.js does not load /api/openapi.json: %q", rec.Body.String())
	}
}

func TestUnknownDocsAssetIs404AProblem(t *testing.T) {
	rec := httptest.NewRecorder()
	NewHandler(ok, acceptKey).ServeHTTP(rec, authed("/api/docs/assets/nope.js"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /api/docs/assets/nope.js = %d, want 404", rec.Code)
	}
	if doc := problem(t, rec); doc["status"].(float64) != 404 {
		t.Errorf("problem = %v", doc)
	}
}

func TestDocsAssetsNeedACredential(t *testing.T) {
	for _, path := range []string{"/api/docs", "/api/docs/assets/init.js", "/api/docs/assets/nope.js"} {
		if rec := request(path, nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("GET %s with no credential = %d, want 401", path, rec.Code)
		}
	}
}

func TestSpecDocumentsRefuseACallerWithoutCredentials(t *testing.T) {
	for _, path := range []string{"/api/openapi.yaml", "/api/openapi.json", "/api/docs"} {
		rec := httptest.NewRecorder()
		NewHandler(ok, acceptKey).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("GET %s without credentials = %d, want 401", path, rec.Code)
		}
	}
}

// stubOperations answers every operation with the configured behaviour.
type stubOperations struct {
	mode string
}

// brokenResponse is a response object of every operation whose body cannot be
// written.
type brokenResponse struct{}

var errBroken = errors.New("cannot write")

func (brokenResponse) VisitGetHealthzResponse(http.ResponseWriter) error     { return errBroken }
func (brokenResponse) VisitGetReadyzResponse(http.ResponseWriter) error      { return errBroken }
func (brokenResponse) VisitGetOpenapiYAMLResponse(http.ResponseWriter) error { return errBroken }
func (brokenResponse) VisitGetOpenapiJSONResponse(http.ResponseWriter) error { return errBroken }
func (brokenResponse) VisitGetDocsResponse(http.ResponseWriter) error        { return errBroken }
func (brokenResponse) VisitGetDocsAssetResponse(http.ResponseWriter) error   { return errBroken }

// outcome is what every stub operation returns: an error or a response that
// fails to write.
func (s stubOperations) outcome() (any, error) {
	if s.mode == "fails" {
		return nil, errBroken
	}
	return brokenResponse{}, nil
}

func (s stubOperations) GetHealthz(context.Context, GetHealthzRequestObject) (GetHealthzResponseObject, error) {
	r, err := s.outcome()
	resp, _ := r.(GetHealthzResponseObject)
	return resp, err
}

func (s stubOperations) GetReadyz(context.Context, GetReadyzRequestObject) (GetReadyzResponseObject, error) {
	r, err := s.outcome()
	resp, _ := r.(GetReadyzResponseObject)
	return resp, err
}

func (s stubOperations) GetOpenapiYAML(context.Context, GetOpenapiYAMLRequestObject) (GetOpenapiYAMLResponseObject, error) {
	r, err := s.outcome()
	resp, _ := r.(GetOpenapiYAMLResponseObject)
	return resp, err
}

func (s stubOperations) GetOpenapiJSON(context.Context, GetOpenapiJSONRequestObject) (GetOpenapiJSONResponseObject, error) {
	r, err := s.outcome()
	resp, _ := r.(GetOpenapiJSONResponseObject)
	return resp, err
}

func (s stubOperations) GetDocsAsset(context.Context, GetDocsAssetRequestObject) (GetDocsAssetResponseObject, error) {
	r, err := s.outcome()
	resp, _ := r.(GetDocsAssetResponseObject)
	return resp, err
}

func (s stubOperations) GetDocs(context.Context, GetDocsRequestObject) (GetDocsResponseObject, error) {
	r, err := s.outcome()
	resp, _ := r.(GetDocsResponseObject)
	return resp, err
}

// TestAFailingOperationAnswersAProblemDocument sends every operation through
// the generated handler with an operation that errors, one whose response
// cannot be written, and a middleware that swaps in a foreign response: each answers 500 as a
// problem document, and the strict middleware sees every call.
func TestAFailingOperationAnswersAProblemDocument(t *testing.T) {
	paths := []string{"/healthz", "/readyz", "/api/openapi.yaml", "/api/openapi.json", "/api/docs", "/api/docs/assets/init.js"}
	for _, mode := range []string{"fails", "wrong type", "unwritable"} {
		var seen []string
		record := func(next StrictHandlerFunc, operationID string) StrictHandlerFunc {
			return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request any) (any, error) {
				seen = append(seen, operationID)
				if mode == "wrong type" {
					return struct{}{}, nil
				}
				return next(ctx, w, r, request)
			}
		}
		mux := http.NewServeMux()
		HandlerFromMux(strictOperations(stubOperations{mode: mode}, record), mux)
		for _, path := range paths {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			if rec.Code != http.StatusInternalServerError {
				t.Errorf("%s: GET %s = %d, want 500", mode, path, rec.Code)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
				t.Errorf("%s: GET %s Content-Type = %q, want application/problem+json", mode, path, ct)
			}
		}
		if len(seen) != len(paths) {
			t.Errorf("%s: the middleware saw %v, want one call per operation", mode, seen)
		}
	}
}

// TestDeclaredProblemResponsesWriteAProblemDocument writes every 400 and 401
// response the document declares and checks status, content type and body.
func TestDeclaredProblemResponsesWriteAProblemDocument(t *testing.T) {
	problem := Problem{Type: "about:blank", Title: "t", Status: 1}
	cases := []struct {
		name  string
		want  int
		write func(http.ResponseWriter) error
	}{
		{"docs 400", 400, GetDocs400ApplicationProblemPlusJSONResponse{BadRequestApplicationProblemPlusJSONResponse(problem)}.VisitGetDocsResponse},
		{"docs 401", 401, GetDocs401ApplicationProblemPlusJSONResponse{UnauthorizedApplicationProblemPlusJSONResponse(problem)}.VisitGetDocsResponse},
		{"asset 400", 400, GetDocsAsset400ApplicationProblemPlusJSONResponse{BadRequestApplicationProblemPlusJSONResponse(problem)}.VisitGetDocsAssetResponse},
		{"asset 401", 401, GetDocsAsset401ApplicationProblemPlusJSONResponse{UnauthorizedApplicationProblemPlusJSONResponse(problem)}.VisitGetDocsAssetResponse},
		{"asset 404", 404, GetDocsAsset404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse(problem)}.VisitGetDocsAssetResponse},
		{"json 400", 400, GetOpenapiJSON400ApplicationProblemPlusJSONResponse{BadRequestApplicationProblemPlusJSONResponse(problem)}.VisitGetOpenapiJSONResponse},
		{"json 401", 401, GetOpenapiJSON401ApplicationProblemPlusJSONResponse{UnauthorizedApplicationProblemPlusJSONResponse(problem)}.VisitGetOpenapiJSONResponse},
		{"yaml 400", 400, GetOpenapiYAML400ApplicationProblemPlusJSONResponse{BadRequestApplicationProblemPlusJSONResponse(problem)}.VisitGetOpenapiYAMLResponse},
		{"yaml 401", 401, GetOpenapiYAML401ApplicationProblemPlusJSONResponse{UnauthorizedApplicationProblemPlusJSONResponse(problem)}.VisitGetOpenapiYAMLResponse},
		{"readyz 400", 400, GetReadyz400ApplicationProblemPlusJSONResponse{BadRequestApplicationProblemPlusJSONResponse(problem)}.VisitGetReadyzResponse},
		{"readyz 401", 401, GetReadyz401ApplicationProblemPlusJSONResponse{UnauthorizedApplicationProblemPlusJSONResponse(problem)}.VisitGetReadyzResponse},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		if err := tc.write(rec); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		var got Problem
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got != problem {
			t.Errorf("%s: body %q does not decode to %+v (%v)", tc.name, rec.Body.String(), problem, err)
		}
		if rec.Code != tc.want || rec.Header().Get("Content-Type") != "application/problem+json" {
			t.Errorf("%s: %d %q, want %d application/problem+json", tc.name, rec.Code, rec.Header().Get("Content-Type"), tc.want)
		}
	}
}

// TestReadyzNotReadyIsTextAndTheHandlerHonoursABaseURL covers the 503 body and
// the generated routers.
func TestReadyzNotReadyIsTextAndTheHandlerHonoursABaseURL(t *testing.T) {
	rec := httptest.NewRecorder()
	if err := GetReadyz503TextResponse("not ready\n").VisitGetReadyzResponse(rec); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 503 || rec.Body.String() != "not ready\n" {
		t.Errorf("503 response = %d %q", rec.Code, rec.Body.String())
	}
	ssi := operations{ready: ok}
	for name, h := range map[string]http.Handler{
		"Handler":                   Handler(NewStrictHandler(ssi, nil)),
		"HandlerFromMuxWithBaseURL": HandlerFromMuxWithBaseURL(NewStrictHandler(ssi, nil), http.NewServeMux(), "/v9"),
	} {
		path := "/healthz"
		if name != "Handler" {
			path = "/v9/healthz"
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK || rec.Body.String() != "ok\n" {
			t.Errorf("%s: GET %s = %d %q, want 200 ok", name, path, rec.Code, rec.Body.String())
		}
	}
}

// TestParameterErrorsNameTheParameter pins the messages the generated router
// reports for a bad parameter, and that a wrapped cause stays reachable.
func TestParameterErrorsNameTheParameter(t *testing.T) {
	cause := errors.New("boom")
	cases := []struct {
		err    error
		want   string
		unwrap bool
	}{
		{&UnescapedCookieParamError{ParamName: "p", Err: cause}, "cookie parameter 'p'", true},
		{&UnmarshalingParamError{ParamName: "p", Err: cause}, "parameter p as JSON: boom", true},
		{&RequiredParamError{ParamName: "p"}, "Query argument p is required", false},
		{&RequiredHeaderError{ParamName: "p", Err: cause}, "Header parameter p is required", true},
		{&InvalidParamFormatError{ParamName: "p", Err: cause}, "Invalid format for parameter p: boom", true},
		{&TooManyValuesForParamError{ParamName: "p", Count: 2}, "Expected one value for p, got 2", false},
	}
	for _, tc := range cases {
		if got := tc.err.Error(); !strings.Contains(got, tc.want) {
			t.Errorf("%T: %q does not contain %q", tc.err, got, tc.want)
		}
		if tc.unwrap && !errors.Is(tc.err, cause) {
			t.Errorf("%T does not wrap its cause", tc.err)
		}
	}
}

func TestAnUnreadableRequestAnswersAProblemDocument(t *testing.T) {
	rec := httptest.NewRecorder()
	requestUnreadable(rec, httptest.NewRequest(http.MethodGet, "/", nil), errBroken)
	if rec.Code != http.StatusBadRequest || rec.Header().Get("Content-Type") != "application/problem+json" {
		t.Errorf("unreadable request = %d %q, want 400 application/problem+json", rec.Code, rec.Header().Get("Content-Type"))
	}
}
