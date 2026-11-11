package store

import (
	"context"
	"testing"
	"time"
)

func TestParseCheckpointInterval(t *testing.T) {
	for _, tc := range []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"", 10 * time.Minute, false},
		{" 2s ", 2 * time.Second, false},
		{"0", 0, false},
		{"off", 0, false},
		{"OFF", 0, false},
		{"0s", 0, false},
		{"-5m", 0, true},
		{"soon", 0, true},
	} {
		got, err := ParseCheckpointInterval(tc.in)
		if got != tc.want || (err != nil) != tc.wantErr {
			t.Errorf("%q: got %v, %v; want %v, error %v", tc.in, got, err, tc.want, tc.wantErr)
		}
	}
}

func TestCheckpointOnAClosedSQLiteStoreFails(t *testing.T) {
	st, err := Open("sqlite://" + t.TempDir() + "/c.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Checkpoint(context.Background()); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	if err := st.Checkpoint(context.Background()); err == nil {
		t.Fatal("a checkpoint on a closed store succeeded")
	}
}

func TestCheckpointOnPostgresDoesNothingAndNeverConnects(t *testing.T) {
	s, err := Open("postgres://u:p@127.0.0.1:1/db?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if err := s.Checkpoint(context.Background()); err != nil {
		t.Fatalf("a Postgres checkpoint is a no-op, got %v", err)
	}
}
