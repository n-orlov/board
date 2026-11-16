package features

import (
	"context"
	"fmt"
	"strings"

	"github.com/cucumber/godog"

	"github.com/n-orlov/village-hall/internal/audit"
	"github.com/n-orlov/village-hall/internal/initiatives"
	"github.com/n-orlov/village-hall/internal/store"
)

// seedInbox migrates the scenario's database and creates the initiative inbox
// as the agent claude@laptop under key:seed, closed when closed is set (by an
// UPDATE: only the row's status matters to the scenario).
func (h *harness) seedInbox(ctx context.Context, title, tags string, closed bool) error {
	if err := h.freshDatabase(); err != nil {
		return err
	}
	st, err := store.Open(h.dbURL)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		return err
	}
	var list []string
	for _, tag := range strings.Split(tags, ",") {
		if tag = strings.TrimSpace(tag); tag != "" {
			list = append(list, tag)
		}
	}
	ini, err := (initiatives.Store{Store: st}).Create(ctx, audit.Actor{Handle: "claude@laptop"}, "key:seed",
		initiatives.New{Slug: "inbox", Title: title, Tags: list})
	if err != nil || !closed {
		return err
	}
	_, err = st.Writer().ExecContext(ctx, st.Dialect().Rebind(
		`UPDATE initiatives SET status = 'closed', closed_at = created_at, closed_by = created_by WHERE id = ?`), ini.ID)
	return err
}

func (h *harness) initiativeIs(ctx context.Context, slug, status, title, description, tags string) error {
	st, err := store.Open(h.dbURL)
	if err != nil {
		return err
	}
	defer st.Close()
	got, err := (initiatives.Store{Store: st}).Get(ctx, slug)
	if err != nil {
		return err
	}
	want := ""
	if tags != "no tags" {
		want = strings.Trim(strings.TrimPrefix(tags, "the tags "), `"`)
	}
	if got.Status != status || got.Title != title || got.Description != description || strings.Join(got.Tags, ", ") != want {
		return fmt.Errorf("initiative %s = %s, %q, %q, tags %v; want %s, %q, %q, tags %q", slug, got.Status, got.Title, got.Description, got.Tags, status, title, description, want)
	}
	return nil
}

func (h *harness) creationEventsAre(ctx context.Context, slug string, n int, kind, handle, principal string) error {
	st, err := store.Open(h.dbURL)
	if err != nil {
		return err
	}
	defer st.Close()
	return checkCreationEvents(ctx, st, slug, n, kind, handle, principal)
}

// checkCreationEvents finds exactly n initiative_created events for the initiative slug, each made by
// an actor of the given kind and handle under principal.
func checkCreationEvents(ctx context.Context, st *store.Store, slug string, n int, kind, handle, principal string) error {
	rows, err := st.Reader().QueryContext(ctx, st.Dialect().Rebind(`SELECT a.handle, a.kind, e.principal FROM events e
		JOIN actors a ON a.id = e.actor_id JOIN initiatives i ON i.id = e.initiative_id
		WHERE e.type = 'initiative_created' AND i.slug = ? ORDER BY e.id`), slug)
	if err != nil {
		return err
	}
	defer rows.Close()
	var got [][3]string
	for rows.Next() {
		var e [3]string
		if err := rows.Scan(&e[0], &e[1], &e[2]); err != nil {
			return err
		}
		got = append(got, e)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(got) != n {
		return fmt.Errorf("initiative %s has %d initiative_created events, want %d: %v", slug, len(got), n, got)
	}
	for _, e := range got {
		if e != [3]string{handle, kind, principal} {
			return fmt.Errorf("initiative_created event of %s = %v, want actor %s (%s) under principal %s", slug, e, handle, kind, principal)
		}
	}
	return nil
}

func registerInitiativeInboxSteps(sc *godog.ScenarioContext, h *harness) {
	sc.Step(`^a migrated database holding the initiative "inbox" titled "([^"]*)" with the tags "([^"]*)"$`,
		func(ctx context.Context, title, tags string) error { return h.seedInbox(ctx, title, tags, false) })
	sc.Step(`^a migrated database holding the closed initiative "inbox" titled "([^"]*)"$`,
		func(ctx context.Context, title string) error { return h.seedInbox(ctx, title, "", true) })
	sc.Step(`^the initiative "([^"]*)" is (open|closed), titled "([^"]*)", described "([^"]*)" and has (no tags|the tags "[^"]*")$`, h.initiativeIs)
	sc.Step(`^the initiative "([^"]*)" has exactly (\d+) initiative_created events?, made by (operator|agent) "([^"]*)" under principal "([^"]*)"$`, h.creationEventsAre)
}
