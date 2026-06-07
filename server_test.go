package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestHelpPageRequiresAuthAndRendersReference(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	a := &app{db: db}
	mux := http.NewServeMux()
	a.routes(mux)

	unauth := httptest.NewRecorder()
	mux.ServeHTTP(unauth, httptest.NewRequest(http.MethodGet, "/help", nil))
	if unauth.Code != http.StatusFound || unauth.Header().Get("Location") != "/login" {
		t.Fatalf("unauthenticated /help = %d location %q, want redirect to /login", unauth.Code, unauth.Header().Get("Location"))
	}

	admin, err := getUserByUsername(context.Background(), db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	session, err := createWebSession(context.Background(), db, admin.ID)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/help", nil)
	req.AddCookie(&http.Cookie{Name: "skillsdex_session", Value: session})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("authenticated /help = %d, want 200", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		"Assistant Formats",
		"Portability checklist",
		".agents/skills/name/SKILL.md",
		"~/.codex/skills/owner--slug/SKILL.md",
		"Cursor project rule",
		"https://developers.openai.com/codex/skills",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("/help body missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "<table") {
		t.Fatalf("/help should render as responsive cards, found table markup")
	}
}

func TestHelpPageRejectsNonGet(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	admin, err := getUserByUsername(context.Background(), db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	session, err := createWebSession(context.Background(), db, admin.ID)
	if err != nil {
		t.Fatal(err)
	}

	a := &app{db: db}
	mux := http.NewServeMux()
	a.routes(mux)
	req := httptest.NewRequest(http.MethodPost, "/help", nil)
	req.AddCookie(&http.Cookie{Name: "skillsdex_session", Value: session})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /help = %d, want 405", w.Code)
	}
}
