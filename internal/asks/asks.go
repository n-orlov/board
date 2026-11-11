package asks

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/n-orlov/board/internal/domain"
	"github.com/n-orlov/board/internal/store"
)

// Event types an ask write appends.
const (
	EventRaised    = "ask_raised"
	EventAnswered  = "ask_answered"
	EventWithdrawn = "ask_withdrawn"
)

// Errors a caller tells apart.
var (
	ErrInvalid  = errors.New("invalid ask")
	ErrNotFound = errors.New("ask not found")
	ErrConflict = errors.New("ask state conflict")
)

// InvalidError is a field the request got wrong. It satisfies errors.Is(err,
// ErrInvalid), its text is the sentence to show the caller and Next says what
// to send instead.
type InvalidError struct{ Msg, Next string }

// Error is the sentence to show the caller.
func (e InvalidError) Error() string { return e.Msg }

// Is makes errors.Is(err, ErrInvalid) true for every InvalidError.
func (e InvalidError) Is(target error) bool { return target == ErrInvalid }

// ConflictError is an action the ask's, task's or initiative's current state
// does not allow. It satisfies errors.Is(err, ErrConflict).
type ConflictError struct{ Msg, Next string }

// Error is the sentence to show the caller.
func (e ConflictError) Error() string { return e.Msg }

// Is makes errors.Is(err, ErrConflict) true for every ConflictError.
func (e ConflictError) Is(target error) bool { return target == ErrConflict }

func invalid(err error, next string) error {
	if err == nil {
		return nil
	}
	return InvalidError{Msg: err.Error(), Next: next}
}

// Ask is one asks row. Ref is A<n>. Task is the task's ref and Initiative the
// initiative's slug, both empty when the ask has none; the people are actor
// handles, empty when unset. Answer, AnsweredBy, OnBehalfOf and Channel are set
// once the ask is answered, WithdrawnAt and WithdrawReason once it is
// withdrawn. PollSeenAt is when a long poll on the ask last held or returned.
// EndedAt is the clock read just after the transaction that answered or
// withdrew the ask committed (stampEnd): nil while it is open, and for an end
// whose stamp was never written. EndCommitAt is the real clock read after that
// transaction's COMMIT returned (stampEnd), to the nanosecond: the end had
// committed at that instant or earlier, so a mark at or before an instant
// proves the end committed by it. It is nil while the ask is open, for an end
// whose mark was never written and for one recorded before it was kept.
type Ask struct {
	N              int64
	ID             string
	Ref            string
	TaskID         string
	Task           string
	InitiativeID   string
	Initiative     string
	Actor          string
	ActorID        string
	Question       string
	Details        string
	Options        []string
	Mode           string
	Blocking       bool
	State          string
	Answer         string
	AnsweredBy     string
	OnBehalfOf     string
	Channel        string
	CreatedAt      time.Time
	AnsweredAt     *time.Time
	WithdrawnAt    *time.Time
	WithdrawReason string
	PollSeenAt     *time.Time
	EndedAt        *time.Time
	EndCommitAt    *time.Time
}

// Store reads and writes the asks of one database.
type Store struct {
	Store *store.Store
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// MarkClock is the real clock an end's commit mark reads once the end's
	// COMMIT has returned; nil means time.Now. It is never the injectable Now:
	// the deadlines the mark is compared with are real instants.
	MarkClock func() time.Time
}

func (s Store) markNow() time.Time {
	if s.MarkClock != nil {
		return s.MarkClock().UTC()
	}
	return time.Now().UTC()
}

func (s Store) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

const selectAsk = `SELECT k.n, k.id, k.task_id, ti.slug, t.seq, k.initiative_id, i.slug, a.handle, k.actor_id,
	k.question, k.details, k.options, k.mode, k.blocking, k.state, k.answer, ab.handle, ob.handle, k.channel,
	k.created_at, k.answered_at, k.withdrawn_at, k.withdraw_reason, k.poll_seen_at, k.ended_at, k.end_commit_ns
	FROM asks k JOIN actors a ON a.id = k.actor_id
	LEFT JOIN tasks t ON t.id = k.task_id LEFT JOIN initiatives ti ON ti.id = t.initiative_id
	LEFT JOIN initiatives i ON i.id = k.initiative_id
	LEFT JOIN actors ab ON ab.id = k.answered_by LEFT JOIN actors ob ON ob.id = k.on_behalf_of`

// rowScanner is the part of *sql.Row and *sql.Rows that scans.
type rowScanner interface{ Scan(dest ...any) error }

// queryer is the part of *sql.DB and *sql.Tx that reads one row.
type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func scan(r rowScanner) (Ask, error) {
	var (
		k                                          Ask
		taskID, taskSlug, iniID, iniSlug           sql.NullString
		seq                                        sql.NullInt64
		options                                    string
		answer, answeredBy, onBehalf, channel, why sql.NullString
		created                                    int64
		answered, withdrawn, polled, ended, commit sql.NullInt64
	)
	err := r.Scan(&k.N, &k.ID, &taskID, &taskSlug, &seq, &iniID, &iniSlug, &k.Actor, &k.ActorID,
		&k.Question, &k.Details, &options, &k.Mode, &k.Blocking, &k.State, &answer, &answeredBy, &onBehalf, &channel,
		&created, &answered, &withdrawn, &why, &polled, &ended, &commit)
	if err != nil {
		return Ask{}, err
	}
	k.Ref = domain.FormatAskRef(k.N)
	k.TaskID, k.InitiativeID, k.Initiative = taskID.String, iniID.String, iniSlug.String
	if taskID.Valid {
		k.Task = domain.FormatTaskRef(taskSlug.String, int(seq.Int64))
	}
	k.Options = []string{}
	if err := json.Unmarshal([]byte(options), &k.Options); err != nil {
		return Ask{}, fmt.Errorf("read options: %w", err)
	}
	k.Answer, k.AnsweredBy, k.OnBehalfOf, k.Channel, k.WithdrawReason = answer.String, answeredBy.String, onBehalf.String, channel.String, why.String
	k.CreatedAt = store.DecodeTime(created)
	k.AnsweredAt, k.WithdrawnAt, k.PollSeenAt, k.EndedAt = timePtr(answered), timePtr(withdrawn), timePtr(polled), timePtr(ended)
	if commit.Valid {
		at := time.Unix(0, commit.Int64).UTC()
		k.EndCommitAt = &at
	}
	return k, nil
}

func timePtr(n sql.NullInt64) *time.Time {
	if !n.Valid {
		return nil
	}
	t := store.DecodeTime(n.Int64)
	return &t
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// find reads the ask a ref (A<n>, either case, or its ULID in either case)
// names through q.
func (s Store) find(ctx context.Context, q queryer, ref string) (Ask, error) {
	where, arg := ` WHERE k.n = ?`, any(nil)
	if n, ok := domain.ParseAskRef(ref); ok {
		arg = n
	} else if up := strings.ToUpper(ref); domain.ValidateULID(up) == nil {
		where, arg = ` WHERE k.id = ?`, up
	} else {
		return Ask{}, fmt.Errorf("%w: ask %s", ErrNotFound, ref)
	}
	k, err := scan(q.QueryRowContext(ctx, s.Store.Dialect().Rebind(selectAsk+where), arg))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Ask{}, fmt.Errorf("%w: ask %s", ErrNotFound, ref)
	case err != nil:
		return Ask{}, fmt.Errorf("read ask: %w", err)
	}
	return k, nil
}

// Get returns the ask whose ref (A<n>) or ULID is ref, in either letter case,
// or ErrNotFound.
func (s Store) Get(ctx context.Context, ref string) (Ask, error) {
	return s.find(ctx, s.Store.Reader(), ref)
}

// inTx runs f in a transaction on the writer and commits when f returns nil.
func (s Store) inTx(ctx context.Context, f func(*sql.Tx) error) error {
	tx, err := s.Store.Writer().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	if err := f(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// waitingWindow is how recent a long poll must be for the asker to count as waiting.
const waitingWindow = 60 * time.Second

// Waiting is the derived asker_waiting of k: the ask is open, its mode is poll
// and a long poll on it held or returned within the last 60 seconds.
func (s Store) Waiting(k Ask) bool { return s.waitingAt(k, s.now()) }

// waitingAt is Waiting as of the instant at.
func (s Store) waitingAt(k Ask, at time.Time) bool {
	if k.State != domain.AskOpen || k.Mode != domain.AskModePoll || k.PollSeenAt == nil {
		return false
	}
	return at.Sub(*k.PollSeenAt) <= waitingWindow
}
