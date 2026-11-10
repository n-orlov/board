package asks

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

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
	out, err := s.endTx(ctx, func(tx *sql.Tx) (Ask, error) {
		return s.answer(ctx, tx, actor, principal, ref, in)
	})
	if err != nil {
		return Ask{}, err
	}
	return s.stampEnd(ctx, out), nil
}

func (s Store) answer(ctx context.Context, tx *sql.Tx, actor audit.Actor, principal, ref string, in Reply) (Ask, error) {
	cur, err := s.findOpen(ctx, tx, ref, "answer")
	if err != nil {
		return Ask{}, err
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
	err = audit.AppendAs(ctx, tx, d, actorID, now, audit.Event{
		Principal: principal, OnBehalfOfID: onBehalf, Type: EventAnswered, InitiativeID: cur.InitiativeID, TaskID: cur.TaskID, RefID: cur.ID,
		Summary: eventSummary(cur, "answered", in.Answer), Data: answeredData(cur, in),
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
	out, err := s.endTx(ctx, func(tx *sql.Tx) (Ask, error) {
		return s.withdraw(ctx, tx, actor, principal, ref, reason)
	})
	if err != nil {
		return Ask{}, err
	}
	return s.stampEnd(ctx, out), nil
}

// commitClock is the real clock an end's commit mark reads: the deadline a
// long poll is judged by is a real instant too, whatever clock Now is.
var commitClock = time.Now

// endTx runs f, which answers or withdraws one ask, in a transaction on the
// writer, and commits when f returns nil. As the last thing before the commit,
// after every statement and clock read of f, it writes the ask's commit mark
// (end_commit_ns): the real clock then, to the nanosecond. The end therefore
// commits after its mark, and a read made at any instant at or before the mark
// cannot have seen it (SPEC 4.5, 7.2).
func (s Store) endTx(ctx context.Context, f func(*sql.Tx) (Ask, error)) (Ask, error) {
	var out Ask
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		k, err := f(tx)
		if err != nil {
			return err
		}
		at := commitClock().UTC()
		q := s.Store.Dialect().Rebind(`UPDATE asks SET end_commit_ns = ? WHERE id = ?`)
		if _, err := tx.ExecContext(ctx, q, at.UnixNano(), k.ID); err != nil {
			return fmt.Errorf("mark the end: %w", err)
		}
		k.EndCommitAt = &at
		out = k
		return nil
	})
	return out, err
}

// stampEnd records the ended_at of k, which the transaction that answered or
// withdrew it has just committed, and returns k with it. The clock is read only
// now, after the commit, and rounded up to the stored whole millisecond, so
// ended_at is never before the end took effect, which answered_at and
// withdrawn_at, read inside the transaction, can be by however long the commit
// took (`board ask --poll` relies on this, SPEC §8.3). The write runs even when
// the caller has gone and changes nothing once a stamp is set; if it fails, the
// end stands without one.
func (s Store) stampEnd(ctx context.Context, k Ask) Ask {
	ms := store.EncodeBound(s.now())
	q := s.Store.Dialect().Rebind(`UPDATE asks SET ended_at = ? WHERE id = ? AND ended_at IS NULL`)
	if _, err := s.Store.Writer().ExecContext(context.WithoutCancel(ctx), q, ms, k.ID); err != nil {
		return k
	}
	at := store.DecodeTime(ms)
	k.EndedAt = &at
	return k
}

func (s Store) withdraw(ctx context.Context, tx *sql.Tx, actor audit.Actor, principal, ref, reason string) (Ask, error) {
	cur, err := s.findOpen(ctx, tx, ref, "withdraw")
	if err != nil {
		return Ask{}, err
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
	err = audit.AppendAs(ctx, tx, d, actorID, now, audit.Event{
		Principal: principal, Type: EventWithdrawn, InitiativeID: cur.InitiativeID, TaskID: cur.TaskID, RefID: cur.ID,
		Summary: eventSummary(cur, "withdrawn", reason), Data: withdrawnData(cur, reason),
	})
	if err != nil {
		return Ask{}, err
	}
	return s.find(ctx, tx, cur.ID)
}

// findOpen reads the ask ref names and refuses one that is not open.
func (s Store) findOpen(ctx context.Context, tx *sql.Tx, ref, action string) (Ask, error) {
	cur, err := s.find(ctx, tx, ref)
	if err != nil {
		return Ask{}, err
	}
	if cur.State != domain.AskOpen {
		return Ask{}, notOpen(cur, action)
	}
	return cur, nil
}

// answeredData is the data of an ask_answered event.
func answeredData(cur Ask, in Reply) map[string]any {
	data := map[string]any{"ref": cur.Ref, "channel": in.Channel}
	if cur.Task != "" {
		data["task"] = cur.Task
	}
	if in.OnBehalfOf != "" {
		data["on_behalf_of"] = in.OnBehalfOf
	}
	return data
}

// withdrawnData is the data of an ask_withdrawn event.
func withdrawnData(cur Ask, reason string) map[string]any {
	data := map[string]any{"ref": cur.Ref}
	if cur.Task != "" {
		data["task"] = cur.Task
	}
	if reason != "" {
		data["reason"] = reason
	}
	return data
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
