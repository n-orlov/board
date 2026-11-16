package events

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/n-orlov/village-hall/internal/domain"
	"github.com/n-orlov/village-hall/internal/store"
)

func scan(r interface{ Scan(dest ...any) error }) (Event, error) {
	var (
		e                                       Event
		at                                      int64
		onBehalf, ini, iniID, slug, taskID, ref sql.NullString
		seq                                     sql.NullInt64
		data                                    string
	)
	err := r.Scan(&e.N, &e.ID, &at, &e.Type, &e.Actor, &e.Principal, &onBehalf, &ini, &iniID, &slug, &seq, &taskID, &ref, &e.Summary, &data)
	if err != nil {
		return Event{}, err
	}
	e.Ref, e.At = domain.FormatEventRef(e.N), store.DecodeTime(at)
	e.OnBehalfOf, e.Initiative, e.InitiativeID, e.TaskID, e.RefID = onBehalf.String, ini.String, iniID.String, taskID.String, ref.String
	e.Task = taskRef(slug, seq, taskID)
	e.Data = map[string]any{}
	if err := json.Unmarshal([]byte(data), &e.Data); err != nil {
		return Event{}, fmt.Errorf("read event data: %w", err)
	}
	return e, nil
}

// taskRef is the ref of the event's task: slug-seq while the task row exists,
// else the task id the event kept.
func taskRef(slug sql.NullString, seq sql.NullInt64, id sql.NullString) string {
	if slug.Valid && seq.Valid {
		return domain.FormatTaskRef(slug.String, int(seq.Int64))
	}
	return id.String
}
