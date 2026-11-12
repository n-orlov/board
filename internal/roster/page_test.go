package roster

import "testing"

func TestThePageSizeDefaultsAndIsKeptWhenSet(t *testing.T) {
	if got := (Store{}).page(); got != DefaultPage {
		t.Errorf("page = %d, want the default %d", got, DefaultPage)
	}
	if got := (Store{Page: -1}).page(); got != DefaultPage {
		t.Errorf("page of -1 = %d, want the default %d", got, DefaultPage)
	}
	if got := (Store{Page: 3}).page(); got != 3 {
		t.Errorf("page = %d, want 3", got)
	}
}
