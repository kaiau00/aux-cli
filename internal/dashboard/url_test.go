package dashboard

import (
	"strings"
	"testing"
)

// Two forms of the address, deliberately: BaseURL says where the dashboard is,
// URL opens it. Everything written down -- the intro message, the startup line
// -- uses the first; only the context pane's `d` hands over the second. They
// must not drift into each other.
func TestBaseURLCarriesNoCredential(t *testing.T) {
	s := &Server{
		baseURL: "http://127.0.0.1:51363",
		url:     "http://127.0.0.1:51363/?token=deadbeef",
	}
	if base := s.BaseURL(); strings.Contains(base, "token") || strings.Contains(base, "?") {
		t.Errorf("BaseURL carries a credential: %q", base)
	}
	if !strings.HasPrefix(s.URL(), s.BaseURL()) {
		t.Errorf("URL %q is not BaseURL %q plus its token", s.URL(), s.BaseURL())
	}
}

// A nil server is the dashboard being disabled. Both accessors have to survive
// it, because the intro and the startup line are built either way.
func TestADisabledDashboardReportsNoAddress(t *testing.T) {
	var off *Server
	if off.BaseURL() != "" {
		t.Error("a disabled dashboard reports an address")
	}
	if off.URL() != "" {
		t.Error("a disabled dashboard reports a URL")
	}
}
