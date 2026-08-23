package dashboard

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSessionsPageServedWithToken(t *testing.T) {
	server := &Server{token: "secret"}
	req := httptest.NewRequest(http.MethodGet, "/sessions?token=secret", nil)
	rec := httptest.NewRecorder()
	server.handleSessionsPage(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content-type = %q, want text/html", ct)
	}
	if !strings.Contains(rec.Body.String(), "js/sessions.js") {
		t.Fatalf("sessions page must load its script")
	}
}

func TestSessionsPageRequiresToken(t *testing.T) {
	server := &Server{token: "secret"}
	req := httptest.NewRequest(http.MethodGet, "/sessions", nil)
	rec := httptest.NewRecorder()
	server.handleSessionsPage(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", rec.Code)
	}
}

func TestIndexServesTaskFirstWorkspaceNotLegacySessionView(t *testing.T) {
	// The default route must prioritize the active task,
	// not lifetime session telemetry. The old index.html (session tree +
	// aggregate stat cards as the primary object) is gone; / now serves the
	// same task-first content as /tasks.
	server := &Server{token: "secret"}
	req := httptest.NewRequest(http.MethodGet, "/?token=secret", nil)
	rec := httptest.NewRecorder()
	server.handleIndex(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "js/tasks.js") {
		t.Fatal("/ must serve the task-first workspace, not the legacy session view")
	}
	if strings.Contains(rec.Body.String(), `id="core"`) {
		t.Fatal("/ must not serve the old decorative Live Core panel")
	}
}

func TestSessionsAssetsEmbeddedAndWired(t *testing.T) {
	js, err := assets.ReadFile("assets/js/sessions.js")
	if err != nil {
		t.Fatalf("read sessions.js: %v", err)
	}
	if !strings.Contains(string(js), "function escapeHTML(") {
		t.Fatal("sessions.js must HTML-escape untrusted content")
	}
	if _, err := assets.ReadFile("assets/css/sessions.css"); err != nil {
		t.Fatalf("sessions.css must be embedded: %v", err)
	}
}

func TestNavScriptEmbeddedAndLinksEveryPlannedView(t *testing.T) {
	js, err := assets.ReadFile("assets/js/nav.js")
	if err != nil {
		t.Fatalf("read nav.js: %v", err)
	}
	content := string(js)
	for _, href := range []string{"tasks", "project", "memory", "impact", "optimization", "sessions"} {
		if !strings.Contains(content, `href: "`+href+`"`) {
			t.Fatalf("nav.js must link to %q", href)
		}
	}
}
