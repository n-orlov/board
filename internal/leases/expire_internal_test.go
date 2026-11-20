package leases

import (
	"errors"
	"testing"
)

type affected struct {
	n   int64
	err error
}

func (a affected) LastInsertId() (int64, error) { return 0, nil }
func (a affected) RowsAffected() (int64, error) { return a.n, a.err }

func TestAnExpiryUpdateMustTouchExactlyTheLeasesItListed(t *testing.T) {
	if err := wonAll(affected{n: 3}, 3); err != nil {
		t.Errorf("3 of 3 listed: %v", err)
	}
	for name, res := range map[string]affected{
		"another writer expired one first": {n: 2},
		"another writer made one due":      {n: 4},
		"the driver cannot say":            {err: errors.New("no count")},
	} {
		if err := wonAll(res, 3); err == nil {
			t.Errorf("%s: the update was accepted", name)
		}
	}
}
