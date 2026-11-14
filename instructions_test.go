package skillsdex

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestInstructionsHTTPAuthenticationCSRFAndConflicts(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	if _, err := a.DB.Exec(`UPDATE users SET must_change_password = 0 WHERE id = ?`, a.Admin.ID); err != nil {
		t.Fatal(err)
	}
	session, err := createWebSession(ctx, a.DB, a.Admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	csrf, err := csrfTokenForSession(ctx, a.DB, session)
	if err != nil {
		t.Fatal(err)
	}
	request := func(method string, form url.Values, authenticated bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/instructions", strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if authenticated {
			r.AddCookie(&http.Cookie{Name: "skillsdex_session", Value: session})
		}
		w := httptest.NewRecorder()
		a.Mux.ServeHTTP(w, r)
		return w
	}
	if w := request(http.MethodGet, nil, false); w.Code != http.StatusFound {
		t.Fatalf("anonymous GET: %d", w.Code)
	}
	if w := request(http.MethodGet, nil, true); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "csrf_token") {
		t.Fatalf("editor GET: %d", w.Code)
	}
	form := url.Values{"revision": {"0"}, "body": {"Targeted reads first."}}
	if w := request(http.MethodPost, form, true); w.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF: %d", w.Code)
	}
	form.Set("csrf_token", csrf)
	if w := request(http.MethodPost, form, true); w.Code != http.StatusSeeOther {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}
	form.Set("body", "Unsaved draft to preserve")
	if w := request(http.MethodPost, form, true); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "Unsaved draft to preserve") || !strings.Contains(w.Body.String(), "Targeted reads first.") {
		t.Fatalf("stale save did not preserve draft and current history: %d", w.Code)
	}
	if w := request(http.MethodDelete, nil, true); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE: %d", w.Code)
	}
	form.Set("revision", "1")
	form.Set("body", strings.Repeat("x", 65537))
	if w := request(http.MethodPost, form, true); w.Code != http.StatusBadRequest {
		t.Fatalf("oversized content: %d", w.Code)
	}
	history, err := instructionHistory(ctx, a.DB, a.Admin.ID)
	if err != nil || len(history) != 1 {
		t.Fatalf("rejected requests mutated history: %d, %v", len(history), err)
	}
}

func TestInstructionHistoryConflictAndRestore(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	if err := saveInstructions(ctx, a.DB, a.Admin.ID, 0, "First", 0); err != nil {
		t.Fatal(err)
	}
	if err := saveInstructions(ctx, a.DB, a.Admin.ID, 0, "Stale", 0); !errors.Is(err, errInstructionConflict) {
		t.Fatalf("stale edit: %v", err)
	}
	if err := saveInstructions(ctx, a.DB, a.Admin.ID, 1, "Second", 0); err != nil {
		t.Fatal(err)
	}
	if err := saveInstructions(ctx, a.DB, a.Admin.ID, 2, "", 1); err != nil {
		t.Fatal(err)
	}
	history, err := instructionHistory(ctx, a.DB, a.Admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 3 || history[0].Body != "First" || history[1].Body != "Second" || history[2].Body != "First" {
		t.Fatalf("restore altered history: %+v", history)
	}
	other, err := instructionHistory(ctx, a.DB, "other-user")
	if err != nil || len(other) != 0 {
		t.Fatalf("history leaked: %+v, %v", other, err)
	}
	if err := saveInstructions(ctx, a.DB, "other-user", 0, "", 1); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("cross-user restore: %v", err)
	}
}

func TestInstructionRenderingEscapesContent(t *testing.T) {
	page := string(instructionsPage([]instructionRevision{{Revision: 1, Body: `</textarea><script>alert(1)</script>`}}))
	if strings.Contains(page, "<script>") || !strings.Contains(page, "&lt;script&gt;") {
		t.Fatal("instruction content was not escaped")
	}
}
