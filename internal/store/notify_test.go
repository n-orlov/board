package store

import (
	"reflect"
	"testing"
)

func TestParseNotifyOn(t *testing.T) {
	all := []string{"stranded", "asking", "done"}
	for in, want := range map[string][]string{
		"":                     all,
		"  ":                   all,
		"done":                 {"done"},
		"asking,stranded":      {"asking", "stranded"},
		" stranded , done ":    {"stranded", "done"},
		"done,done":            {"done", "done"},
		"stranded,asking,done": all,
	} {
		got, err := ParseNotifyOn(in)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("ParseNotifyOn(%q) = %q, %v, want %q", in, got, err, want)
		}
	}
}

func TestParseNotifyOnRefusesAnyOtherWord(t *testing.T) {
	for _, in := range []string{"all", "Done", "done,", ",", "done stranded", "stranded,none", "alerts"} {
		if got, err := ParseNotifyOn(in); err == nil {
			t.Errorf("ParseNotifyOn(%q) = %q, want an error", in, got)
		}
	}
}

func TestParseNotifyOnReturnsACopyOfTheDefaults(t *testing.T) {
	got, _ := ParseNotifyOn("")
	got[0] = "changed"
	if again, _ := ParseNotifyOn(""); again[0] != "stranded" {
		t.Fatalf("the defaults were changed: %q", again)
	}
}

func TestNotifiesIsOffUntilSetNotify(t *testing.T) {
	var s Store
	if s.Notifies(NotifyDone) {
		t.Fatal("a store nobody configured notifies")
	}
	s.SetNotify([]string{NotifyAsking})
	if !s.Notifies(NotifyAsking) || s.Notifies(NotifyDone) || s.Notifies(NotifyStranded) {
		t.Fatal("SetNotify did not turn on exactly asking")
	}
	s.SetNotify(nil)
	if s.Notifies(NotifyAsking) {
		t.Fatal("SetNotify(nil) left asking on")
	}
}
