package dialog

import (
	"strings"
	"testing"
)

// Opening the model picker with no configured provider must not crash. It
// sliced the empty provider name to capitalise it and panicked, which a
// first-time user could reach with one keystroke before setting any API key.
//
// Found by TestAnOverlayGrowsTheManagedRegion on CI, where no model catalog is
// cached. It passed locally because this machine had one.
func TestModelPickerWithNoProvidersDoesNotPanic(t *testing.T) {
	view := NewModelDialogCmp().View()
	if !strings.Contains(view, "No providers configured") {
		t.Errorf("expected an explanation rather than a crash, got %q", view)
	}
}
