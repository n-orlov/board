package domain

import (
	"strings"
	"testing"
)

func TestAskRefs(t *testing.T) {
	if FormatAskRef(7) != "A7" {
		t.Error("FormatAskRef")
	}
	for in, want := range map[string]int64{"A1": 1, "a12": 12} {
		if n, ok := ParseAskRef(in); !ok || n != want {
			t.Errorf("ParseAskRef(%q) = %d, %v", in, n, ok)
		}
	}
	for _, in := range []string{"", "A", "A0", "A-1", "A+1", "L1", "1", "Ax", "A1a"} {
		if _, ok := ParseAskRef(in); ok {
			t.Errorf("ParseAskRef(%q) accepted", in)
		}
	}
}

func TestAskValidators(t *testing.T) {
	long := strings.Repeat("x", 201)
	for name, c := range map[string]struct {
		err  error
		fail bool
	}{
		"question ok":        {ValidateQuestion(strings.Repeat("x", 200)), false},
		"question empty":     {ValidateQuestion(" "), true},
		"question long":      {ValidateQuestion(long), true},
		"question two line":  {ValidateQuestion("a\nb"), true},
		"question cr":        {ValidateQuestion("a\rb"), true},
		"answer ok":          {ValidateAnswer(strings.Repeat("x", MaxAnswer)), false},
		"answer empty":       {ValidateAnswer(""), true},
		"answer long":        {ValidateAnswer(strings.Repeat("x", MaxAnswer+1)), true},
		"options none":       {ValidateOptions(nil), false},
		"options ten":        {ValidateOptions(strings.Split(strings.Repeat("o,", 10), ",")[:10]), false},
		"options eleven":     {ValidateOptions(strings.Split(strings.Repeat("o,", 11), ",")[:11]), true},
		"option 80":          {ValidateOptions([]string{strings.Repeat("x", 80)}), false},
		"option 81":          {ValidateOptions([]string{strings.Repeat("x", 81)}), true},
		"option empty":       {ValidateOptions([]string{""}), true},
		"mode stop":          {ValidateAskMode("stop"), false},
		"mode poll":          {ValidateAskMode("poll"), false},
		"mode other":         {ValidateAskMode("Stop"), true},
		"lease action none":  {ValidateLeaseAction("none"), false},
		"lease action keep":  {ValidateLeaseAction("keep"), false},
		"lease action stop":  {ValidateLeaseAction("stop"), false},
		"lease action other": {ValidateLeaseAction("Stop"), true},
		"lease action empty": {ValidateLeaseAction(""), true},
		"channel ui":         {ValidateChannel("ui"), false},
		"channel api":        {ValidateChannel("api"), false},
		"channel relay":      {ValidateChannel("relay"), false},
		"channel other":      {ValidateChannel("sms"), true},
	} {
		if (c.err != nil) != c.fail {
			t.Errorf("%s: err = %v", name, c.err)
		}
	}
}
