package welcome

import (
	"strings"
	"testing"
)

// The intro is the only place a new user is told what Aux does with their
// code before it starts doing it, so the claims it makes are load-bearing.
func TestIntroExplainsWhatHappensToTheUsersCode(t *testing.T) {
	body := buildIntroBody("")

	for _, want := range []struct{ substr, why string }{
		{"sent to your configured model provider", "a user must know their files leave the machine"},
		{"ask first", "the permission model is the reason it is safe to say yes"},
		{"outside this project directory", "read confinement is a selling point and undocumented otherwise"},
		{"stored locally", "where state lives is the second question anyone asks"},
	} {
		if !strings.Contains(body, want.substr) {
			t.Errorf("intro is missing %q — %s", want.substr, want.why)
		}
	}
}

func TestIntroRendersWithoutADashboard(t *testing.T) {
	body := buildIntroBody("")
	if body == "" {
		t.Fatal("the intro must render when the dashboard is disabled")
	}
	if !strings.Contains(body, "dashboard.enabled") {
		t.Error("with no dashboard, the intro should say how to turn one on")
	}
}

// ShouldShow decides whether a user is interrupted on startup, so it must not
// misfire when configuration is missing.
func TestShouldShowIsSafeWithoutConfig(t *testing.T) {
	// config.Get() is nil in a bare test binary; this must not panic and must
	// not claim a first boot it cannot verify.
	if ShouldShow() {
		t.Error("without a data directory there is no flag file to check, so the welcome must not fire")
	}
}
