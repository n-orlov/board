package features

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cucumber/godog"
)

// apiState is what the API-key scenarios remember between steps.
type apiState struct {
	// created is the key the last create response held, body the last response
	// body.
	created string
	body    []byte
	// ids maps the slug of each initiative a scenario created to its ULID.
	ids map[string]string
	// headers are extra request headers for the next sendKeyed call only.
	headers map[string]string
	// counts is the tasks, events and actors count a scenario noted, and
	// sentAt and answeredAt bracket the last task request.
	counts string
	// startCounts is the tasks, leases and events count a scenario noted.
	startCounts string
	// leaseRows is the leases table a scenario noted.
	leaseRows string
	// askCounts is the asks, events and actors count a scenario noted.
	askCounts string
	// noteCounts is the notes, events and actors count a scenario noted.
	noteCounts string
	// allRows is every row of every table a scenario noted.
	allRows            string
	sentAt, answeredAt time.Time
	// wait is the ask wait a scenario left running in the background.
	wait *askWait
}

// sendKeyed sends one request to the running server with the given key and actor
// and records the status and body.
func (h *harness) sendKeyed(ctx context.Context, method, path, key, actor string, payload any) error {
	var rd io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://"+h.addr+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Board-Actor", actor)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range h.api.headers {
		req.Header.Set(k, v)
	}
	h.api.headers = nil
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	h.status = resp.StatusCode
	h.api.body, err = io.ReadAll(resp.Body)
	return err
}

func (h *harness) createKeyViaAPI(ctx context.Context, name, expiresIn, actor string) error {
	if h.key == "" {
		return errors.New("no key was remembered")
	}
	payload := map[string]string{"name": name}
	if expiresIn != "" {
		payload["expires_in"] = expiresIn
	}
	if err := h.sendKeyed(ctx, http.MethodPost, "/api/v1/keys", h.key, actor, payload); err != nil {
		return err
	}
	var doc struct {
		Key string `json:"key"`
	}
	if h.status == http.StatusCreated {
		if err := json.Unmarshal(h.api.body, &doc); err != nil {
			return fmt.Errorf("response %q: %w", h.api.body, err)
		}
	}
	h.api.created = doc.Key
	return nil
}

func (h *harness) revokeKeyViaAPI(ctx context.Context, name string) error {
	return h.sendKeyed(ctx, http.MethodPost, "/api/v1/keys/"+name+"/revoke", h.key, "godog", nil)
}

func (h *harness) getWithCreatedKey(ctx context.Context, path string) error {
	if h.api.created == "" {
		return errors.New("no key was created through the API")
	}
	return h.sendKeyed(ctx, http.MethodGet, path, h.api.created, "godog", nil)
}

func (h *harness) responseHoldsNewKeyOnce() error {
	if !keyPattern.MatchString(h.api.created) || len(h.api.created) != len("brd_")+52 {
		return fmt.Errorf("the response %q holds no brd_ key", h.api.body)
	}
	if n := bytes.Count(h.api.body, []byte(h.api.created)); n != 1 {
		return fmt.Errorf("the key appears %d times in the response, want 1", n)
	}
	return nil
}

func (h *harness) responseLacksCreatedKey() error {
	if h.api.created == "" {
		return errors.New("no key was created through the API")
	}
	if bytes.Contains(h.api.body, []byte(h.api.created)) {
		return fmt.Errorf("the response %q holds the created key", h.api.body)
	}
	return nil
}

func (h *harness) responseListsKeyWithoutKeyField(name string) error {
	var doc struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(h.api.body, &doc); err != nil {
		return fmt.Errorf("response %q: %w", h.api.body, err)
	}
	for _, item := range doc.Items {
		if item["name"] != name {
			continue
		}
		if _, has := item["key"]; has {
			return fmt.Errorf("the listed key %q has a key field", name)
		}
		if prefix, _ := item["display_prefix"].(string); len(prefix) != 8 || !strings.HasPrefix(h.api.created, "brd_"+prefix) {
			return fmt.Errorf("display_prefix %q does not start key %q", prefix, name)
		}
		return nil
	}
	return fmt.Errorf("the list %s has no key %q", h.api.body, name)
}

func (h *harness) problemDetailIs(want string) error {
	var doc struct {
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(h.api.body, &doc); err != nil {
		return fmt.Errorf("response %q is not a problem document: %w", h.api.body, err)
	}
	if doc.Detail != want {
		return fmt.Errorf("problem detail %q, want %q", doc.Detail, want)
	}
	return nil
}

// query rewrites ? markers to the leg's placeholder form (one marker at most).
func (h *harness) query(q string) string {
	if h.leg == "postgres" {
		return strings.Replace(q, "?", "$1", 1)
	}
	return q
}

func (h *harness) keyExpiredAnHourAgo(ctx context.Context, name string) error {
	db, err := h.openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	past := time.Now().UnixMilli() - 3600*1000
	q := "UPDATE api_keys SET expires_at = ? WHERE name = ?"
	if h.leg == "postgres" {
		q = "UPDATE api_keys SET expires_at = $1 WHERE name = $2"
	}
	res, err := db.ExecContext(ctx, q, past, name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return fmt.Errorf("%d rows updated for key %q, want 1", n, name)
	}
	return nil
}

func (h *harness) stdoutLacksCreatedKey() error {
	if h.api.created == "" {
		return errors.New("no key was created through the API")
	}
	if strings.Contains(h.stdout, h.api.created) {
		return fmt.Errorf("stdout %q contains the created key", h.stdout)
	}
	return nil
}

func (h *harness) rowHoldsOnlyTheHash(ctx context.Context, name string) error {
	db, err := h.openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, h.query("SELECT * FROM api_keys WHERE name = ?"), name)
	if err != nil {
		return err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return err
	}
	if !rows.Next() {
		return fmt.Errorf("no api_keys row named %q", name)
	}
	vals := make([]any, len(cols))
	dest := make([]any, len(cols))
	for i := range vals {
		dest[i] = &vals[i]
	}
	if err := rows.Scan(dest...); err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(h.api.created))
	hashed := false
	for i, c := range cols {
		text := fmt.Sprint(vals[i])
		hashed = hashed || (c == "key_hash" && text == hex.EncodeToString(sum[:]))
		if strings.Contains(text, h.api.created) || strings.Contains(text, h.api.created[len("brd_")+8:]) {
			return fmt.Errorf("column %s holds the key", c)
		}
	}
	if !hashed {
		return errors.New("key_hash is not the SHA-256 of the created key")
	}
	return nil
}

// eventColumn reads one column of the key_created event of the key named name.
func (h *harness) eventValue(ctx context.Context, name, query string) (string, error) {
	db, err := h.openDB()
	if err != nil {
		return "", err
	}
	defer db.Close()
	var got string
	err = db.QueryRowContext(ctx, h.query(query), name).Scan(&got)
	return got, err
}

func (h *harness) eventHasPrincipal(ctx context.Context, name, want string) error {
	got, err := h.eventValue(ctx, name, `SELECT e.principal FROM events e JOIN api_keys k ON k.id = e.ref_id WHERE e.type = 'key_created' AND k.name = ?`)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("principal column %q, want %q", got, want)
	}
	return nil
}

func (h *harness) eventHasActor(ctx context.Context, name, want string) error {
	got, err := h.eventValue(ctx, name, `SELECT a.handle FROM events e JOIN actors a ON a.id = e.actor_id JOIN api_keys k ON k.id = e.ref_id WHERE e.type = 'key_created' AND k.name = ?`)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("actor handle %q, want %q", got, want)
	}
	return nil
}

func registerAPIKeySteps(sc *godog.ScenarioContext, h *harness) {
	sc.Step(`^I create the API key "([^"]*)"(?: expiring in "([^"]*)")? as actor "([^"]*)" through the API with the remembered key$`, h.createKeyViaAPI)
	sc.Step(`^I revoke the API key "([^"]*)" through the API with the remembered key$`, h.revokeKeyViaAPI)
	sc.Step(`^I request GET "([^"]*)" with the created key$`, h.getWithCreatedKey)
	sc.Step(`^the response holds the new key exactly once$`, h.responseHoldsNewKeyOnce)
	sc.Step(`^the response does not contain the created key$`, h.responseLacksCreatedKey)
	sc.Step(`^the response lists the key "([^"]*)" with a display prefix and no key field$`, h.responseListsKeyWithoutKeyField)
	sc.Step(`^the problem detail is "([^"]*)"$`, h.problemDetailIs)
	sc.Step(`^the key "([^"]*)" expired an hour ago$`, h.keyExpiredAnHourAgo)
	sc.Step(`^stdout does not contain the created key$`, h.stdoutLacksCreatedKey)
	sc.Step(`^the api_keys row named "([^"]*)" holds the SHA-256 hash of the created key and nowhere the key itself$`, h.rowHoldsOnlyTheHash)
	sc.Step(`^the key_created event for "([^"]*)" has principal "([^"]*)" in the principal column$`, h.eventHasPrincipal)
	sc.Step(`^the key_created event for "([^"]*)" has actor "([^"]*)" in the actor column$`, h.eventHasActor)
}
