package features

import (
	"bytes"
	"context"
	"fmt"
	"net/http"

	"github.com/cucumber/godog"
)

// sendWithIdempotencyKey sends a POST with an Idempotency-Key header.
func (h *harness) sendWithIdempotencyKey(ctx context.Context, path, body, actor, key string) error {
	h.nextRequestCarries("Idempotency-Key", key)
	return h.sendTask(ctx, http.MethodPost, path, body, actor)
}

func (h *harness) rememberResponse() {
	h.api.remembered = append([]byte(nil), h.api.body...)
}

func (h *harness) responseIsTheRemembered() error {
	if len(h.api.remembered) == 0 || !bytes.Equal(h.api.body, h.api.remembered) {
		return fmt.Errorf("response body %q, want the remembered %q", h.api.body, h.api.remembered)
	}
	return nil
}

func (h *harness) responseHeaderIs(name, want string) error {
	if got := h.api.respHeader.Get(name); got != want {
		return fmt.Errorf("response header %s is %q, want %q", name, got, want)
	}
	return nil
}

func (h *harness) responseHasNoHeader(name string) error {
	if got, ok := h.api.respHeader[http.CanonicalHeaderKey(name)]; ok {
		return fmt.Errorf("response header %s is %q, want none", name, got)
	}
	return nil
}

func registerIdempotencySteps(sc *godog.ScenarioContext, h *harness) {
	sc.Step(`^I send POST "([^"]*)" with body "([^"]*)" as actor "([^"]*)" with the idempotency key "([^"]*)"$`, h.sendWithIdempotencyKey)
	sc.Step(`^I remember the response$`, h.rememberResponse)
	sc.Step(`^the response body is the remembered response$`, h.responseIsTheRemembered)
	sc.Step(`^the response header "([^"]*)" is "([^"]*)"$`, h.responseHeaderIs)
	sc.Step(`^the response has no header "([^"]*)"$`, h.responseHasNoHeader)
}
