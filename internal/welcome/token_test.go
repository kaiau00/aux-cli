package welcome

import (
	"strings"
	"testing"
)

// The dashboard's access token must never reach the intro message.
//
// The intro is a real message in the session: written to the database, printed
// into the terminal's scrollback, and carried into anything exported or pasted
// from the transcript. The token is the only thing between a local HTTP port
// and the whole session -- prompts, file contents, tool output -- so a token
// written down there outlives the session that made it and travels wherever
// the transcript goes. D10, and why M2.7 lists this.
//
// The guarantee is structural rather than a check on the output: the function
// that builds the intro is given the dashboard's address and nothing else, so
// there is no token in scope for it to print. This test holds that shape in
// place -- if the signature goes back to taking the server, it stops
// compiling.
func TestTheIntroIsNeverGivenTheDashboardToken(t *testing.T) {
	const addr = "http://127.0.0.1:51363"
	intro := buildIntroBody(addr)

	if !strings.Contains(intro, addr) {
		t.Errorf("the intro does not name the dashboard's address:\n%s", intro)
	}
	if strings.Contains(intro, "token=") {
		t.Error("the intro contains a token query parameter")
	}
	// It has to say how to actually open it, or naming the address is a
	// dead end: the dashboard refuses a request without the token.
	if !strings.Contains(intro, "`d`") {
		t.Error("the intro does not say how to get the link with the token in it")
	}
}

// With the dashboard off, the intro says so rather than naming an address
// nothing is listening on.
func TestTheIntroSaysWhenTheDashboardIsOff(t *testing.T) {
	intro := buildIntroBody("")
	if strings.Contains(intro, "127.0.0.1") || strings.Contains(intro, "running at") {
		t.Errorf("the intro advertises a dashboard that is not running:\n%s", intro)
	}
	if !strings.Contains(intro, "disabled") {
		t.Error("the intro does not say the dashboard is disabled")
	}
}
