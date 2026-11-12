package features

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/cucumber/godog"
)

// notificationID is the ULID the scenarios give the nth outbox row.
func notificationID(n int) string { return fmt.Sprintf("01JC%022d", n) }

func (h *harness) listNotifications(ctx context.Context, query, actor string) error {
	if err := h.requireKey(); err != nil {
		return err
	}
	path := "/api/v1/notifications"
	if query != "" {
		path += "?" + query
	}
	return h.sendKeyed(ctx, http.MethodGet, path, h.key, actor, nil)
}

func (h *harness) nextNotificationsPage(ctx context.Context, query, actor string) error {
	after, err := h.nextAfter()
	if err != nil {
		return err
	}
	return h.listNotifications(ctx, query+"&after="+url.QueryEscape(after), actor)
}

// seedNotifications inserts one outbox row per table row, on the event written nth,
// created that many seconds after a fixed instant. A pending row is due a minute
// after it was created and a sent row was sent half a second after.
func (h *harness) seedNotifications(ctx context.Context, table *godog.Table) error {
	base := time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)
	head := table.Rows[0].Cells
	for _, row := range table.Rows[1:] {
		col := map[string]string{}
		for i, c := range row.Cells {
			col[head[i].Value] = c.Value
		}
		n, _ := strconv.Atoi(col["n"])
		seconds, _ := strconv.Atoi(col["seconds"])
		attempts, _ := strconv.Atoi(col["attempts"])
		eventN, err := h.eventOrdinal(ctx, n+1)
		if err != nil {
			return err
		}
		eventID, err := h.scalar(ctx, `SELECT id FROM events WHERE n = `+eventN)
		if err != nil {
			return err
		}
		at := base.Add(time.Duration(seconds) * time.Second).UnixMilli()
		var sent, lastError any
		if col["state"] == "sent" {
			sent = at + 500
		}
		if col["last_error"] != "" {
			lastError = col["last_error"]
		}
		_, err = h.exec(ctx, `INSERT INTO notifications (id, kind, event_id, payload, state, attempts, next_attempt_at, last_error, created_at, sent_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, notificationID(n), col["kind"], eventID,
			fmt.Sprintf(`{"kind":%q,"text":"alert %d"}`, col["kind"], n), col["state"], attempts, at+60000, lastError, at, sent)
		if err != nil {
			return fmt.Errorf("seed notification %d: %w", n, err)
		}
	}
	return nil
}

func (h *harness) itemsAreNotifications(positions string) error {
	var want []string
	for _, p := range splitList(positions) {
		n, err := strconv.Atoi(p)
		if err != nil {
			return err
		}
		want = append(want, notificationID(n))
	}
	return h.itemsHave("id", strings.Join(want, ","))
}

func (h *harness) nextAfterIsNotification(n int) error {
	got, err := h.nextAfter()
	if err != nil {
		return err
	}
	if want := notificationID(n); got != want {
		return fmt.Errorf("next_after is %q, want the id of notification %d (%q)", got, n, want)
	}
	return nil
}

func (h *harness) itemIsACompleteNotification(ordinal int) error {
	doc, err := h.bodyMap()
	if err != nil {
		return err
	}
	list, _ := doc["items"].([]any)
	if len(list) < ordinal {
		return fmt.Errorf("the response %s has no item %d", h.api.body, ordinal)
	}
	item, _ := list[ordinal-1].(map[string]any)
	for _, f := range []string{"id", "kind", "state", "event", "created_at"} {
		if s, ok := item[f].(string); !ok || s == "" {
			return fmt.Errorf("item %d has no text %q: %v", ordinal, f, item)
		}
	}
	if _, ok := item["attempts"].(float64); !ok {
		return fmt.Errorf("item %d has no attempts number: %v", ordinal, item)
	}
	if _, ok := item["payload"].(map[string]any); !ok {
		return fmt.Errorf("item %d has no payload object: %v", ordinal, item)
	}
	if _, err := time.Parse(time.RFC3339Nano, fmt.Sprint(item["created_at"])); err != nil {
		return fmt.Errorf("item %d created_at %v: %w", ordinal, item["created_at"], err)
	}
	return nil
}

func (h *harness) pathIsEventRefOfNotification(ctx context.Context, path string, n int) error {
	eventN, err := h.eventOrdinal(ctx, n+1)
	if err != nil {
		return err
	}
	return h.pathIs(path, "E"+eventN)
}

func registerNotificationSteps(sc *godog.ScenarioContext, h *harness) {
	sc.Step(`^I request the notifications list "([^"]*)" as actor "([^"]*)"$`, h.listNotifications)
	sc.Step(`^I request the next notifications page with "([^"]*)" as actor "([^"]*)"$`, h.nextNotificationsPage)
	sc.Step(`^the outbox holds these notifications:$`, h.seedNotifications)
	sc.Step(`^the response items are the notifications "([^"]*)"$`, h.itemsAreNotifications)
	sc.Step(`^the response next_after is the id of the notification (\d+)$`, h.nextAfterIsNotification)
	sc.Step(`^item (\d+) of the response is a complete notification$`, h.itemIsACompleteNotification)
	sc.Step(`^the response path "([^"]*)" is the ref of the event of notification (\d+)$`, h.pathIsEventRefOfNotification)
}
