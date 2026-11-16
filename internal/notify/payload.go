package notify

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/n-orlov/board/internal/domain"
	"github.com/n-orlov/board/internal/store"
)

// Payload is the JSON body of an alert (SPEC 5.4). It carries the task, initiative,
// ask and lease an alert is about, never a key, a session or a header value.
type Payload struct {
	Text       string      `json:"text"`
	Kind       string      `json:"kind"`
	Task       *TaskInfo   `json:"task,omitempty"`
	Initiative *Initiative `json:"initiative,omitempty"`
	Actor      Actor       `json:"actor"`
	Ask        *AskInfo    `json:"ask,omitempty"`
	Lease      *LeaseInfo  `json:"lease,omitempty"`
	BoardURL   string      `json:"board_url,omitempty"`
}

// TaskInfo names a task; URL is absent when VH_PUBLIC_URL is unset.
type TaskInfo struct {
	Ref   string `json:"ref"`
	Title string `json:"title"`
	URL   string `json:"url,omitempty"`
}

// Initiative names the initiative an ask on an initiative is about.
type Initiative struct {
	Slug string `json:"slug"`
}

// Actor is who the alert is about: the lease holder, the asker or whoever marked the task done.
type Actor struct {
	Handle  string `json:"handle"`
	WayBack string `json:"way_back,omitempty"`
}

// AskInfo is the ask of an asking alert.
type AskInfo struct {
	Ref      string `json:"ref"`
	Question string `json:"question"`
	Blocking bool   `json:"blocking"`
}

// LeaseInfo is the lease of a stranded alert.
type LeaseInfo struct {
	Ref       string `json:"ref"`
	Intent    string `json:"intent"`
	ExpiredAt string `json:"expired_at"`
}

// textMax caps a title, a question or an intent in the readable text, in runes.
const textMax = 120

// BuildPayload reads what the event eventID of the given kind is about, in tx,
// and returns the alert's JSON. publicURL is VH_PUBLIC_URL (empty when unset).
func BuildPayload(ctx context.Context, tx *sql.Tx, st *store.Store, eventID, kind, publicURL string) ([]byte, error) {
	r := reader{ctx: ctx, tx: tx, st: st}
	p := Payload{Kind: kind}
	var taskID, initiativeID, refID sql.NullString
	err := tx.QueryRowContext(ctx, st.Dialect().Rebind(`SELECT a.handle, a.way_back, e.task_id, e.initiative_id, e.ref_id
		FROM events e JOIN actors a ON a.id = e.actor_id WHERE e.id = ?`), eventID).
		Scan(&p.Actor.Handle, (*nullable)(&p.Actor.WayBack), &taskID, &initiativeID, &refID)
	if err != nil {
		return nil, fmt.Errorf("read the event of a %s notification: %w", kind, err)
	}
	base := strings.TrimRight(publicURL, "/")
	if base != "" {
		p.BoardURL = base + "/"
	}
	if err := r.subject(&p, taskID.String, initiativeID.String, base); err != nil {
		return nil, err
	}
	if err := r.about(&p, refID.String); err != nil {
		return nil, err
	}
	p.Text = p.text()
	return json.Marshal(p)
}

// nullable scans a possibly-NULL text column into a string.
type nullable string

func (n *nullable) Scan(v any) error {
	var s sql.NullString
	if err := s.Scan(v); err != nil {
		return err
	}
	*n = nullable(s.String)
	return nil
}

type reader struct {
	ctx context.Context
	tx  *sql.Tx
	st  *store.Store
}

// subject fills the task of a task alert, or the initiative of an ask on an
// initiative; a board ask has neither.
func (r reader) subject(p *Payload, taskID, initiativeID, base string) error {
	switch {
	case taskID != "":
		var slug string
		var seq int
		var title string
		err := r.tx.QueryRowContext(r.ctx, r.st.Dialect().Rebind(`SELECT i.slug, t.seq, t.title FROM tasks t
			JOIN initiatives i ON i.id = t.initiative_id WHERE t.id = ?`), taskID).Scan(&slug, &seq, &title)
		if err != nil {
			return fmt.Errorf("read the task of a notification: %w", err)
		}
		p.Task = &TaskInfo{Ref: domain.FormatTaskRef(slug, seq), Title: title}
		if base != "" {
			p.Task.URL = base + "/#/t/" + p.Task.Ref
		}
	case initiativeID != "":
		var slug string
		err := r.tx.QueryRowContext(r.ctx, r.st.Dialect().Rebind(`SELECT slug FROM initiatives WHERE id = ?`), initiativeID).Scan(&slug)
		if err != nil {
			return fmt.Errorf("read the initiative of a notification: %w", err)
		}
		p.Initiative = &Initiative{Slug: slug}
	}
	return nil
}

// about fills the ask of an asking alert or the lease of a stranded one.
func (r reader) about(p *Payload, refID string) error {
	switch p.Kind {
	case store.NotifyAsking:
		var n int64
		a := AskInfo{}
		err := r.tx.QueryRowContext(r.ctx, r.st.Dialect().Rebind(`SELECT n, question, blocking FROM asks WHERE id = ?`), refID).
			Scan(&n, &a.Question, &a.Blocking)
		if err != nil {
			return fmt.Errorf("read the ask of a notification: %w", err)
		}
		a.Ref = domain.FormatAskRef(n)
		p.Ask = &a
	case store.NotifyStranded:
		var n, expires int64
		l := LeaseInfo{}
		err := r.tx.QueryRowContext(r.ctx, r.st.Dialect().Rebind(`SELECT n, intent, expires_at FROM leases WHERE id = ?`), refID).
			Scan(&n, &l.Intent, &expires)
		if err != nil {
			return fmt.Errorf("read the lease of a notification: %w", err)
		}
		l.Ref = domain.FormatLeaseRef(n)
		l.ExpiredAt = store.DecodeTime(expires).UTC().Format(time.RFC3339Nano)
		p.Lease = &l
	}
	return nil
}

// text is the plain text a person reads on a phone: one line, or two for a
// stranded task (what the holder meant to do and how to reach them).
func (p Payload) text() string {
	who := p.Actor.Handle
	switch p.Kind {
	case store.NotifyStranded:
		return fmt.Sprintf("stranded: %s lease %s expired (%s)\nintent: %s; way back: %s",
			p.subjectText(), p.Lease.Ref, who, oneLine(p.Lease.Intent), wayBack(p.Actor.WayBack))
	case store.NotifyAsking:
		return fmt.Sprintf("asking%s: %s (%s%s)", p.askScope(), oneLine(p.Ask.Question), who, blockingText(p.Ask.Blocking))
	}
	return fmt.Sprintf("done: %s (%s)", p.subjectText(), who)
}

func (p Payload) subjectText() string {
	if p.Task == nil {
		return "board"
	}
	return p.Task.Ref + " " + oneLine(p.Task.Title)
}

// askScope says where an ask was raised: on a task, an initiative or the board.
func (p Payload) askScope() string {
	switch {
	case p.Task != nil:
		return " on " + p.subjectText()
	case p.Initiative != nil:
		return " on initiative " + p.Initiative.Slug
	}
	return " on board"
}

func blockingText(blocking bool) string {
	if blocking {
		return ", blocking"
	}
	return ""
}

func wayBack(w string) string {
	if w == "" {
		return "none recorded"
	}
	return oneLine(w)
}

// oneLine collapses s onto one line and cuts it to textMax runes.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > textMax {
		return string(r[:textMax-1]) + "…"
	}
	return s
}
