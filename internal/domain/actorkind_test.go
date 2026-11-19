package domain

import "testing"

func TestActorKind(t *testing.T) {
	cases := []struct{ handle, want string }{
		{"ralph:x", "ralph"},
		{"claude:session:2", "claude"},
		{"pi:a", "pi"},
		{"codex:a:b", "codex"},
		{"deck:z", "deck"},
		{"ralph:", "ralph"},
		{"foo:x", ""},
		{"Ralph:x", ""},
		{"ralphs:x", ""},
		{"ralph", ""},
		{"", ""},
		{":ralph", ""},
	}
	for _, c := range cases {
		if got := ActorKind(c.handle); got != c.want {
			t.Errorf("ActorKind(%q) = %q, want %q", c.handle, got, c.want)
		}
	}
}
