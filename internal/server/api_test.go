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
	NewHandler(ok, acceptKey, nil, nil).ServeHTTP(rec, authed("/api/openapi.yaml"))
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
	NewHandler(ok, acceptKey, nil, nil).ServeHTTP(rec, authed("/api/openapi.json"))
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
	h := NewHandler(ok, acceptKey, nil, nil)
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
	NewHandler(ok, acceptKey, nil, nil).ServeHTTP(rec, authed("/api/docs/assets/init.js"))
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
	NewHandler(ok, acceptKey, nil, nil).ServeHTTP(rec, authed("/api/docs/assets/nope.js"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /api/docs/assets/nope.js = %d, want 404", rec.Code)
	}
	doc := problem(t, rec)
	if doc["status"].(float64) != 404 {
		t.Errorf("problem = %v", doc)
	}
	if next, _ := doc["next"].(string); !strings.Contains(next, "/api/docs") {
		t.Errorf("unknown asset problem next = %q, want advice naming /api/docs", next)
	}
}

// TestEveryUnknownDocsAssetNameAnswersAProblemWithNext holds the rule behind the
// unknown asset: any name the embedded set lacks, however spelled, is a 404
// problem document that keeps its fields and names a next action.
func TestEveryUnknownDocsAssetNameAnswersAProblemWithNext(t *testing.T) {
	for _, name := range []string{"nope.js", "INIT.JS", "init.js.map", "%2e%2e%2fetc", "swagger-ui.css.gz"} {
		rec := httptest.NewRecorder()
		NewHandler(ok, acceptKey, nil, nil).ServeHTTP(rec, authed("/api/docs/assets/"+name))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET assets/%s = %d, want 404", name, rec.Code)
			continue
		}
		doc := problem(t, rec)
		if next, _ := doc["next"].(string); strings.TrimSpace(next) == "" {
			t.Errorf("assets/%s: problem %v has no next", name, doc)
		}
		for _, f := range []string{"type", "title", "detail"} {
			if s, _ := doc[f].(string); s == "" {
				t.Errorf("assets/%s: problem %v lost %s", name, doc, f)
			}
		}
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
		NewHandler(ok, acceptKey, nil, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
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
func (brokenResponse) VisitGetAuthLoginResponse(http.ResponseWriter) error   { return errBroken }
func (brokenResponse) VisitGetAuthOidcCallbackResponse(http.ResponseWriter) error {
	return errBroken
}
func (brokenResponse) VisitGetAuthSessionResponse(http.ResponseWriter) error { return errBroken }
func (brokenResponse) VisitPostAuthLogoutResponse(http.ResponseWriter) error { return errBroken }
func (brokenResponse) VisitListKeysResponse(http.ResponseWriter) error       { return errBroken }
func (brokenResponse) VisitCreateKeyResponse(http.ResponseWriter) error      { return errBroken }
func (brokenResponse) VisitRevokeKeyResponse(http.ResponseWriter) error      { return errBroken }
func (brokenResponse) VisitListInitiativesResponse(http.ResponseWriter) error {
	return errBroken
}
func (brokenResponse) VisitCreateInitiativeResponse(http.ResponseWriter) error {
	return errBroken
}
func (brokenResponse) VisitGetInitiativeResponse(http.ResponseWriter) error { return errBroken }
func (brokenResponse) VisitGetMetaResponse(http.ResponseWriter) error       { return errBroken }
func (brokenResponse) VisitPutProtocolSeenResponse(http.ResponseWriter) error {
	return errBroken
}

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

func (s stubOperations) GetAuthLogin(context.Context, GetAuthLoginRequestObject) (GetAuthLoginResponseObject, error) {
	r, err := s.outcome()
	resp, _ := r.(GetAuthLoginResponseObject)
	return resp, err
}

func (s stubOperations) GetAuthOidcCallback(context.Context, GetAuthOidcCallbackRequestObject) (GetAuthOidcCallbackResponseObject, error) {
	r, err := s.outcome()
	resp, _ := r.(GetAuthOidcCallbackResponseObject)
	return resp, err
}

func (s stubOperations) GetAuthSession(context.Context, GetAuthSessionRequestObject) (GetAuthSessionResponseObject, error) {
	r, err := s.outcome()
	resp, _ := r.(GetAuthSessionResponseObject)
	return resp, err
}

func (s stubOperations) PostAuthLogout(context.Context, PostAuthLogoutRequestObject) (PostAuthLogoutResponseObject, error) {
	r, err := s.outcome()
	resp, _ := r.(PostAuthLogoutResponseObject)
	return resp, err
}

func (s stubOperations) ListKeys(context.Context, ListKeysRequestObject) (ListKeysResponseObject, error) {
	r, err := s.outcome()
	resp, _ := r.(ListKeysResponseObject)
	return resp, err
}

func (s stubOperations) CreateKey(context.Context, CreateKeyRequestObject) (CreateKeyResponseObject, error) {
	r, err := s.outcome()
	resp, _ := r.(CreateKeyResponseObject)
	return resp, err
}

func (s stubOperations) RevokeKey(context.Context, RevokeKeyRequestObject) (RevokeKeyResponseObject, error) {
	r, err := s.outcome()
	resp, _ := r.(RevokeKeyResponseObject)
	return resp, err
}

func (s stubOperations) ListInitiatives(context.Context, ListInitiativesRequestObject) (ListInitiativesResponseObject, error) {
	r, err := s.outcome()
	resp, _ := r.(ListInitiativesResponseObject)
	return resp, err
}

func (s stubOperations) CreateInitiative(context.Context, CreateInitiativeRequestObject) (CreateInitiativeResponseObject, error) {
	r, err := s.outcome()
	resp, _ := r.(CreateInitiativeResponseObject)
	return resp, err
}

func (s stubOperations) GetInitiative(context.Context, GetInitiativeRequestObject) (GetInitiativeResponseObject, error) {
	r, err := s.outcome()
	resp, _ := r.(GetInitiativeResponseObject)
	return resp, err
}

func (s stubOperations) GetMeta(context.Context, GetMetaRequestObject) (GetMetaResponseObject, error) {
	r, err := s.outcome()
	resp, _ := r.(GetMetaResponseObject)
	return resp, err
}

func (s stubOperations) PutProtocolSeen(context.Context, PutProtocolSeenRequestObject) (PutProtocolSeenResponseObject, error) {
	r, err := s.outcome()
	resp, _ := r.(PutProtocolSeenResponseObject)
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
	paths := []string{
		"/healthz", "/readyz", "/api/openapi.yaml", "/api/openapi.json", "/api/docs", "/api/docs/assets/init.js",
		"/auth/login", "/auth/oidc/callback", "/auth/session", "POST /auth/logout",
		"/api/v1/keys", "POST /api/v1/keys", "POST /api/v1/keys/workstation/revoke",
		"/api/v1/initiatives", "POST /api/v1/initiatives", "/api/v1/initiatives/phone-onboard",
		"/api/v1/meta", "PUT /api/v1/actors/me/protocol-seen",
	}
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
			method, target := http.MethodGet, path
			if m, p, ok := strings.Cut(path, " "); ok {
				method, target = m, p
			}
			req := httptest.NewRequest(method, target, strings.NewReader(`{"name":"x"}`))
			req.Header.Set("Content-Type", "application/json")
			mux.ServeHTTP(rec, req)
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

// TestAFailingOperationThroughTheHandlerAnswersAProblemWithNext sends the same
// failing operations through the wrapper NewHandler installs: whatever the
// operation does wrong, the 500 problem document names a next action.
func TestAFailingOperationThroughTheHandlerAnswersAProblemWithNext(t *testing.T) {
	paths := []string{"/healthz", "/api/docs/assets/init.js", "/api/v1/keys", "POST /api/v1/initiatives", "/api/v1/meta"}
	for _, mode := range []string{"fails", "wrong type", "unwritable"} {
		mux := http.NewServeMux()
		HandlerFromMux(strictOperations(stubOperations{mode: mode}, func(next StrictHandlerFunc, _ string) StrictHandlerFunc { return next }), mux)
		h := withProblemNext(mux)
		for _, path := range paths {
			method, target := http.MethodGet, path
			if m, p, ok := strings.Cut(path, " "); ok {
				method, target = m, p
			}
			req := httptest.NewRequest(method, target, strings.NewReader(`{"name":"x"}`))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusInternalServerError {
				t.Errorf("%s: %s = %d, want 500", mode, path, rec.Code)
				continue
			}
			doc := problem(t, rec)
			if next, _ := doc["next"].(string); strings.TrimSpace(next) == "" {
				t.Errorf("%s: %s: problem %v has no next", mode, path, doc)
			}
		}
	}
}

// TestDeclaredProblemResponsesWriteAProblemDocument writes every problem
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
		{"login 401", 401, GetAuthLogin401ApplicationProblemPlusJSONResponse{UnauthorizedApplicationProblemPlusJSONResponse(problem)}.VisitGetAuthLoginResponse},
		{"login 404", 404, GetAuthLogin404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse(problem)}.VisitGetAuthLoginResponse},
		{"login 502", 502, GetAuthLogin502ApplicationProblemPlusJSONResponse{BadGatewayApplicationProblemPlusJSONResponse(problem)}.VisitGetAuthLoginResponse},
		{"login 503", 503, GetAuthLogin503ApplicationProblemPlusJSONResponse{UnavailableApplicationProblemPlusJSONResponse(problem)}.VisitGetAuthLoginResponse},
		{"login default", 418, GetAuthLogindefaultApplicationProblemPlusJSONResponse{Body: problem, StatusCode: 418}.VisitGetAuthLoginResponse},
		{"callback 400", 400, GetAuthOidcCallback400ApplicationProblemPlusJSONResponse(problem).VisitGetAuthOidcCallbackResponse},
		{"callback 401", 401, GetAuthOidcCallback401ApplicationProblemPlusJSONResponse{UnauthorizedApplicationProblemPlusJSONResponse(problem)}.VisitGetAuthOidcCallbackResponse},
		{"callback 403", 403, GetAuthOidcCallback403ApplicationProblemPlusJSONResponse{ForbiddenApplicationProblemPlusJSONResponse(problem)}.VisitGetAuthOidcCallbackResponse},
		{"callback 404", 404, GetAuthOidcCallback404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse(problem)}.VisitGetAuthOidcCallbackResponse},
		{"callback 502", 502, GetAuthOidcCallback502ApplicationProblemPlusJSONResponse{BadGatewayApplicationProblemPlusJSONResponse(problem)}.VisitGetAuthOidcCallbackResponse},
		{"callback default", 418, GetAuthOidcCallbackdefaultApplicationProblemPlusJSONResponse{Body: problem, StatusCode: 418}.VisitGetAuthOidcCallbackResponse},
		{"session 401", 401, GetAuthSession401ApplicationProblemPlusJSONResponse{UnauthorizedApplicationProblemPlusJSONResponse(problem)}.VisitGetAuthSessionResponse},
		{"session 404", 404, GetAuthSession404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse(problem)}.VisitGetAuthSessionResponse},
		{"logout 401", 401, PostAuthLogout401ApplicationProblemPlusJSONResponse{UnauthorizedApplicationProblemPlusJSONResponse(problem)}.VisitPostAuthLogoutResponse},
		{"keys 400", 400, ListKeys400ApplicationProblemPlusJSONResponse{BadRequestApplicationProblemPlusJSONResponse(problem)}.VisitListKeysResponse},
		{"keys 401", 401, ListKeys401ApplicationProblemPlusJSONResponse{UnauthorizedApplicationProblemPlusJSONResponse(problem)}.VisitListKeysResponse},
		{"keys 503", 503, ListKeys503ApplicationProblemPlusJSONResponse{UnavailableApplicationProblemPlusJSONResponse(problem)}.VisitListKeysResponse},
		{"create key 400", 400, CreateKey400ApplicationProblemPlusJSONResponse{BadRequestApplicationProblemPlusJSONResponse(problem)}.VisitCreateKeyResponse},
		{"create key 401", 401, CreateKey401ApplicationProblemPlusJSONResponse{UnauthorizedApplicationProblemPlusJSONResponse(problem)}.VisitCreateKeyResponse},
		{"create key 409", 409, CreateKey409ApplicationProblemPlusJSONResponse{ConflictApplicationProblemPlusJSONResponse(problem)}.VisitCreateKeyResponse},
		{"create key 503", 503, CreateKey503ApplicationProblemPlusJSONResponse{UnavailableApplicationProblemPlusJSONResponse(problem)}.VisitCreateKeyResponse},
		{"revoke key 400", 400, RevokeKey400ApplicationProblemPlusJSONResponse{BadRequestApplicationProblemPlusJSONResponse(problem)}.VisitRevokeKeyResponse},
		{"revoke key 401", 401, RevokeKey401ApplicationProblemPlusJSONResponse{UnauthorizedApplicationProblemPlusJSONResponse(problem)}.VisitRevokeKeyResponse},
		{"revoke key 404", 404, RevokeKey404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse(problem)}.VisitRevokeKeyResponse},
		{"revoke key 503", 503, RevokeKey503ApplicationProblemPlusJSONResponse{UnavailableApplicationProblemPlusJSONResponse(problem)}.VisitRevokeKeyResponse},
		{"list initiatives 400", 400, ListInitiatives400ApplicationProblemPlusJSONResponse{BadRequestApplicationProblemPlusJSONResponse(problem)}.VisitListInitiativesResponse},
		{"list initiatives 401", 401, ListInitiatives401ApplicationProblemPlusJSONResponse{UnauthorizedApplicationProblemPlusJSONResponse(problem)}.VisitListInitiativesResponse},
		{"list initiatives 503", 503, ListInitiatives503ApplicationProblemPlusJSONResponse{UnavailableApplicationProblemPlusJSONResponse(problem)}.VisitListInitiativesResponse},
		{"create initiative 400", 400, CreateInitiative400ApplicationProblemPlusJSONResponse{BadRequestApplicationProblemPlusJSONResponse(problem)}.VisitCreateInitiativeResponse},
		{"create initiative 401", 401, CreateInitiative401ApplicationProblemPlusJSONResponse{UnauthorizedApplicationProblemPlusJSONResponse(problem)}.VisitCreateInitiativeResponse},
		{"create initiative 409", 409, CreateInitiative409ApplicationProblemPlusJSONResponse{ConflictApplicationProblemPlusJSONResponse(problem)}.VisitCreateInitiativeResponse},
		{"create initiative 503", 503, CreateInitiative503ApplicationProblemPlusJSONResponse{UnavailableApplicationProblemPlusJSONResponse(problem)}.VisitCreateInitiativeResponse},
		{"get initiative 400", 400, GetInitiative400ApplicationProblemPlusJSONResponse{BadRequestApplicationProblemPlusJSONResponse(problem)}.VisitGetInitiativeResponse},
		{"get initiative 401", 401, GetInitiative401ApplicationProblemPlusJSONResponse{UnauthorizedApplicationProblemPlusJSONResponse(problem)}.VisitGetInitiativeResponse},
		{"get initiative 404", 404, GetInitiative404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse(problem)}.VisitGetInitiativeResponse},
		{"get initiative 503", 503, GetInitiative503ApplicationProblemPlusJSONResponse{UnavailableApplicationProblemPlusJSONResponse(problem)}.VisitGetInitiativeResponse},
		{"meta 401", 401, GetMeta401ApplicationProblemPlusJSONResponse{UnauthorizedApplicationProblemPlusJSONResponse(problem)}.VisitGetMetaResponse},
		{"protocol seen 400", 400, PutProtocolSeen400ApplicationProblemPlusJSONResponse{BadRequestApplicationProblemPlusJSONResponse(problem)}.VisitPutProtocolSeenResponse},
		{"protocol seen 401", 401, PutProtocolSeen401ApplicationProblemPlusJSONResponse{UnauthorizedApplicationProblemPlusJSONResponse(problem)}.VisitPutProtocolSeenResponse},
		{"protocol seen 503", 503, PutProtocolSeen503ApplicationProblemPlusJSONResponse{UnavailableApplicationProblemPlusJSONResponse(problem)}.VisitPutProtocolSeenResponse},
		{"logout 404", 404, PostAuthLogout404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse(problem)}.VisitPostAuthLogoutResponse},
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
		// The same typed response served through the handler's wrapper gains the
		// next its value lacked and keeps every other field.
		wrapped := httptest.NewRecorder()
		withProblemNext(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _ = tc.write(w) })).ServeHTTP(wrapped, httptest.NewRequest(http.MethodGet, "/", nil))
		var doc map[string]any
		if err := json.Unmarshal(wrapped.Body.Bytes(), &doc); err != nil {
			t.Errorf("%s: wrapped body %q: %v", tc.name, wrapped.Body.String(), err)
			continue
		}
		if next, _ := doc["next"].(string); strings.TrimSpace(next) == "" {
			t.Errorf("%s: wrapped problem %v has no next", tc.name, doc)
		}
		if doc["title"] != problem.Title || doc["type"] != problem.Type || wrapped.Code != tc.want || wrapped.Header().Get("Content-Type") != "application/problem+json" {
			t.Errorf("%s: wrapped response %d %q %v lost fields", tc.name, wrapped.Code, wrapped.Header().Get("Content-Type"), doc)
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
