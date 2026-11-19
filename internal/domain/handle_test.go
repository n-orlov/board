package domain

import (
	"strings"
	"testing"
)

func TestValidateHandle(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, handle, wantErr string
	}{
		{"plain", "alice", ""},
		{"agent form", "ralph:run-1", ""},
		{"punctuation and at sign", "claude@laptop/x.y_z", ""},
		{"exactly the longest", strings.Repeat("a", MaxHandleLen), ""},
		{"multibyte counts characters", strings.Repeat("é", MaxHandleLen), ""},
		{"empty", "", "not empty"},
		{"one too long", strings.Repeat("a", MaxHandleLen+1), "at most 100"},
		{"space", "al ice", "no whitespace"},
		{"tab", "al\tice", "no whitespace"},
		{"newline", "alice\n", "no whitespace"},
		{"non-breaking space", "al ice", "no whitespace"},
		{"control character", "al\x07ice", "control"},
	} {
		err := ValidateHandle(tc.handle)
		switch {
		case tc.wantErr == "" && err != nil:
			t.Errorf("%s: ValidateHandle(%q) = %v, want nil", tc.name, tc.handle, err)
		case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
			t.Errorf("%s: ValidateHandle(%q) = %v, want an error saying %q", tc.name, tc.handle, err, tc.wantErr)
		}
	}
}
