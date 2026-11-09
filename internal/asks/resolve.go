package asks

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/n-orlov/board/internal/audit"
	"github.com/n-orlov/board/internal/domain"
	"github.com/n-orlov/board/internal/store"
)

// Reply is the request to answer an ask. Channel "" is ui for a session (an
// operator actor) and api for a key. OnBehalfOf is the handle of the person a
// relayed answer came from, allowed with the relay channel only.
type Reply struct {
	Answer     string
	Channel    string
	OnBehalfOf string
}

// Normalize checks the reply against the actor answering and returns it with the
// answer trimmed and the channel defaulted.
func (r Reply) Normalize(actor audit.Actor) (Reply, error) {
	if r.Channel == "" {
		r.Channel = domain.ChannelAPI
		if actor.Operator {
			r.Channel = domain.ChannelUI
		}
	}
	r.OnBehalfOf = strings.TrimSpace(r.OnBehalfOf)
	switch {
	case domain.ValidateAnswer(r.Answer) != nil:
		return Reply{}, invalid(domain.ValidateAnswer(r.Answer), "Send an answer of 1 to 20000 characters.")
	case domain.ValidateChannel(r.Channel) != nil:
		return Reply{}, invalid(domain.ValidateChannel(r.Channel), "Send channel ui, api or relay, or leave it out.")
	case r.OnBehalfOf != "" && r.Channel != domain.ChannelRelay:
		return Reply{}, InvalidError{Msg: "on_behalf_of is allowed with channel relay only",
			Next: "Send channel relay with on_behalf_of, or leave on_behalf_of out."}
	}
	r.Answer = strings.TrimSpace(r.Answer)
	return r, nil
}

// Answer moves the open ask ref names to answered, recording the answer, the
// channel and who answered (and for whom, on a relay), and appends an
// ask_answered event, in one transaction. It returns ErrInvalid for a reply
// that is wrong, ErrNotFound for an unknown ask and ErrConflict, naming its
// state, for an ask that is not open, and writes no row, event or actor then.
func (s Store) Answer(ctx context.Context, actor audit.Actor, principal, ref string, in Reply) (Ask, error) {
	in, err := in.Normalize(actor)
	if err != nil {
		return Ask{}, err
	}
	var out Ask
	err = s.inTx(ctx, func(tx *sql.Tx) error {
		out, err = s.answer(ctx, tx, actor, principal, ref, in)
		return err
	})
	return out, err
}

func (s Store) answer(ctx context.Context, tx *sql.Tx, actor audit.Actor, principal, ref string, in Reply) (Ask, error) {
	cur, err := s.find(ctx, tx, ref)
	if err != nil {
		return Ask{}, err
	}
	if cur.State != domain.AskOpen {
		return Ask{}, notOpen(cur, "answer")
	}
	now := s.now()
	d := s.Store.Dialect()
	actorID, err := audit.UpsertActor(ctx, tx, d, actor, now)
	if err != nil {
		return Ask{}, err
	}
	onBehalf, err := audit.OnBehalf(ctx, tx, d, audit.Actor{OnBehalfOf: in.OnBehalfOf}, now)
	if err != nil {
		return Ask{}, err
	}
	res, err := tx.ExecContext(ctx, d.Rebind(`UPDATE asks SET state = 'answered', answer = ?, answered_by = ?, on_behalf_of = ?,
		channel = ?, answered_at = ? WHERE id = ? AND state = 'open'`),
		in.Answer, actorID, nullable(onBehalf), in.Channel, store.EncodeTime(now), cur.ID)
	if err != nil {
		return Ask{}, fmt.Errorf("answer ask: %w", err)
	}
	if err := s.wonRace(ctx, tx, res, cur, "answer"); err != nil {
		return Ask{}, err
	}
	data := map[string]any{"ref": cur.Ref, "channel": in.Channel}
	if cur.Task != "" {
		data["task"] = cur.Task
	}
	if in.OnBehalfOf != "" {
		data["on_behalf_of"] = in.OnBehalfOf
	}
	err = audit.AppendAs(ctx, tx, d, actorID, now, audit.Event{
		Principal: principal, OnBehalfOfID: onBehalf, Type: EventAnswered, InitiativeID: cur.InitiativeID, TaskID: cur.TaskID, RefID: cur.ID,
		Summary: eventSummary(cur, "answered", in.Answer), Data: data,
	})
	if err != nil {
		return Ask{}, err
	}
	return s.find(ctx, tx, cur.ID)
}

// Withdraw moves the open ask ref names to withdrawn, with an optional reason of
// at most 200 characters, and appends an ask_withdrawn event, in one
// transaction. It returns ErrInvalid for a reason that is too long, ErrNotFound
// for an unknown ask and ErrConflict, naming its state, for an ask that is not
// open, and writes no row, event or actor then.
func (s Store) Withdraw(ctx context.Context, actor audit.Actor, principal, ref, reason string) (Ask, error) {
	reason = strings.TrimSpace(reason)
	if n := len([]rune(reason)); n > domain.MaxWithdrawNote {
		return Ask{}, InvalidError{Msg: fmt.Sprintf("reason must be at most %d characters, got %d", domain.MaxWithdrawNote, n),
			Next: "Send a shorter reason, or leave it out."}
	}
	var out Ask
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var err error
		out, err = s.withdraw(ctx, tx, actor, principal, ref, reason)
		return err
	})
	return out, err
}

func (s Store) withdraw(ctx context.Context, tx *sql.Tx, actor audit.Actor, principal, ref, reason string) (Ask, error) {
	cur, err := s.find(ctx, tx, ref)
	if err != nil {
		return Ask{}, err
	}
	if cur.State != domain.AskOpen {
		return Ask{}, notOpen(cur, "withdraw")
	}
	now := s.now()
	d := s.Store.Dialect()
	actorID, err := audit.UpsertActor(ctx, tx, d, actor, now)
	if err != nil {
		return Ask{}, err
	}
	res, err := tx.ExecContext(ctx, d.Rebind(`UPDATE asks SET state = 'withdrawn', withdrawn_at = ?, withdraw_reason = ?
		WHERE id = ? AND state = 'open'`), store.EncodeTime(now), nullable(reason), cur.ID)
	if err != nil {
		return Ask{}, fmt.Errorf("withdraw ask: %w", err)
	}
	if err := s.wonRace(ctx, tx, res, cur, "withdraw"); err != nil {
		return Ask{}, err
	}
	data := map[string]any{"ref": cur.Ref}
	if cur.Task != "" {
		data["task"] = cur.Task
	}
	if reason != "" {
		data["reason"] = reason
	}
	err = audit.AppendAs(ctx, tx, d, actorID, now, audit.Event{
		Principal: principal, Type: EventWithdrawn, InitiativeID: cur.InitiativeID, TaskID: cur.TaskID, RefID: cur.ID,
		Summary: eventSummary(cur, "withdrawn", reason), Data: data,
	})
	if err != nil {
		return Ask{}, err
	}
	return s.find(ctx, tx, cur.ID)
}

// notOpen is the refusal of an action on an ask that is answered or withdrawn.
func notOpen(k Ask, action string) ConflictError {
	return ConflictError{Msg: "cannot " + action + " ask " + k.Ref + ": it is " + k.State,
		Next: "Only an open ask can be answered or withdrawn."}
}

// wonRace returns nil when the conditional update changed the ask, and else the
// ConflictError naming the state another writer moved it to.
func (s Store) wonRace(ctx context.Context, tx *sql.Tx, res sql.Result, cur Ask, action string) error {
	if n, err := res.RowsAffected(); err == nil && n > 0 {
		return nil
	}
	now, err := s.find(ctx, tx, cur.ID)
	if err != nil {
		return err
	}
	return notOpen(now, action)
}
