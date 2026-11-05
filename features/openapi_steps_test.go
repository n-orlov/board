package features

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/cucumber/godog"
	"go.yaml.in/yaml/v3"
)

// openapiPath is api/openapi.yaml, relative to the features/ test directory.
var openapiPath = filepath.Join("..", "api", "openapi.yaml")

// operation is one method + path the OpenAPI document declares.
type operation struct {
	method string
	path   string
}

func (o operation) String() string { return o.method + " " + o.path }

var httpMethods = map[string]bool{
	"get": true, "put": true, "post": true, "delete": true,
	"options": true, "head": true, "patch": true, "trace": true,
}

var pathParam = regexp.MustCompile(`\{[^}]*\}`)

// declaredOperations reads every operation from the OpenAPI document at path,
// in a stable order.
func declaredOperations(path string) ([]operation, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	var ops []operation
	for p, item := range doc.Paths {
		for m := range item {
			if httpMethods[m] {
				ops = append(ops, operation{method: strings.ToUpper(m), path: p})
			}
		}
	}
	sort.Slice(ops, func(i, j int) bool { return ops[i].String() < ops[j].String() })
	if len(ops) == 0 {
		return nil, fmt.Errorf("%s declares no operation", path)
	}
	return ops, nil
}

// sendWithoutCredentials sends op with no Authorization header and no cookie
// and returns the status. A path parameter is filled with a placeholder.
func (h *harness) sendWithoutCredentials(ctx context.Context, op operation) (int, error) {
	target := "http://" + h.addr + pathParam.ReplaceAllString(op.path, "x")
	req, err := http.NewRequestWithContext(ctx, op.method, target, nil)
	if err != nil {
		return 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

func (h *harness) sendEveryDeclaredOperation(ctx context.Context) error {
	ops, err := declaredOperations(openapiPath)
	if err != nil {
		return err
	}
	h.operations, h.answers = ops, map[string]int{}
	for _, op := range ops {
		status, err := h.sendWithoutCredentials(ctx, op)
		if err != nil {
			return fmt.Errorf("%s: %w", op, err)
		}
		h.answers[op.String()] = status
	}
	return nil
}

// everyOperationAnswered requires every declared operation except exception to
// have answered want, and requires exception to be declared.
func (h *harness) everyOperationAnswered(want int, exception string) error {
	if len(h.operations) == 0 {
		return errors.New("no operation was sent")
	}
	if _, ok := h.answers[exception]; !ok {
		return fmt.Errorf("%s is not declared in api/openapi.yaml", exception)
	}
	var wrong []string
	for _, op := range h.operations {
		if op.String() == exception {
			continue
		}
		if got := h.answers[op.String()]; got != want {
			wrong = append(wrong, fmt.Sprintf("%s answered %d", op, got))
		}
	}
	if len(wrong) > 0 {
		return fmt.Errorf("want %d from every operation except %s, but %s", want, exception, strings.Join(wrong, ", "))
	}
	return nil
}

func (h *harness) operationAnswered(op string, want int) error {
	got, ok := h.answers[op]
	if !ok {
		return fmt.Errorf("%s was not sent", op)
	}
	if got != want {
		return fmt.Errorf("%s answered %d, want %d", op, got, want)
	}
	return nil
}

func (h *harness) operationIsDeclared(op string) error {
	ops, err := declaredOperations(openapiPath)
	if err != nil {
		return err
	}
	for _, o := range ops {
		if o.String() == op {
			return nil
		}
	}
	return fmt.Errorf("api/openapi.yaml does not declare %s", op)
}

func registerOpenAPISteps(sc *godog.ScenarioContext, h *harness) {
	sc.Step(`^I send every operation declared in api/openapi.yaml with no Authorization header and no cookie$`, h.sendEveryDeclaredOperation)
	sc.Step(`^every operation except "([^"]*)" answered (\d+)$`, func(exception string, want int) error {
		return h.everyOperationAnswered(want, exception)
	})
	sc.Step(`^"([^"]*)" answered (\d+)$`, h.operationAnswered)
	sc.Step(`^api/openapi.yaml declares "([^"]*)"$`, h.operationIsDeclared)
}
