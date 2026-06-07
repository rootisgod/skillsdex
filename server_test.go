package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
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

func TestSkillExportDumpsOwnedSkills(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	admin, err := getUserByUsername(ctx, db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := createSkill(ctx, db, admin, SkillInput{
		Owner:          "admin",
		Slug:           "exported-skill",
		Name:           "Exported Skill",
		Description:    "Use when testing export.",
		SourceProvider: "github",
		SourceURL:      "https://github.com/acme/skills/tree/main/exported-skill",
		SourceExplicit: true,
		Body:           "# Exported Skill\n\nInstructions.",
	}); err != nil {
		t.Fatal(err)
	}
	if err := archiveSkill(ctx, db, admin.ID, "admin", "exported-skill"); err != nil {
		t.Fatal(err)
	}
	if err := createUser(ctx, db, "alice", "", "secret", false); err != nil {
		t.Fatal(err)
	}
	alice, err := getUserByUsername(ctx, db, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := createSkill(ctx, db, alice, SkillInput{
		Owner:       "alice",
		Slug:        "private",
		Name:        "Private",
		Description: "Use privately.",
		Body:        "# Private",
	}); err != nil {
		t.Fatal(err)
	}
	session, err := createWebSession(ctx, db, admin.ID)
	if err != nil {
		t.Fatal(err)
	}

	a := &app{db: db}
	mux := http.NewServeMux()
	a.routes(mux)
	req := httptest.NewRequest(http.MethodGet, "/skills/export", nil)
	req.AddCookie(&http.Cookie{Name: "skillsdex_session", Value: session})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("export = %d, want 200: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/zip" {
		t.Fatalf("content type = %q, want application/zip", ct)
	}

	zr, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(rc); err != nil {
			rc.Close()
			t.Fatal(err)
		}
		rc.Close()
		entries[f.Name] = buf.String()
	}
	raw, ok := entries["admin--exported-skill/SKILL.md"]
	if !ok || !strings.Contains(raw, "# Exported Skill") {
		t.Fatalf("exported skill missing from zip: %#v", entries)
	}
	if _, ok := entries["alice--private/SKILL.md"]; ok {
		t.Fatal("export included another user's skill")
	}
	var manifest exportManifest
	if err := json.Unmarshal([]byte(entries["skillsdex-export.json"]), &manifest); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, skill := range manifest.Skills {
		if skill.Identifier == "admin/exported-skill" {
			found = true
			if !skill.Archived || skill.SourceProvider != "github" || skill.SourceURL == "" || skill.Path != "admin--exported-skill/SKILL.md" {
				t.Fatalf("unexpected manifest skill: %#v", skill)
			}
		}
		if strings.HasPrefix(skill.Identifier, "alice/") {
			t.Fatalf("manifest included another user's skill: %#v", skill)
		}
	}
	if !found {
		t.Fatalf("manifest missing exported skill: %#v", manifest.Skills)
	}
}

func TestSkillDeleteRouteDeletesOwnedSkill(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	admin, err := getUserByUsername(ctx, db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := createSkill(ctx, db, admin, SkillInput{
		Owner:       "admin",
		Slug:        "delete-me",
		Name:        "Delete Me",
		Description: "Use when testing delete.",
		Body:        "# Delete Me",
	}); err != nil {
		t.Fatal(err)
	}
	session, err := createWebSession(ctx, db, admin.ID)
	if err != nil {
		t.Fatal(err)
	}

	a := &app{db: db}
	mux := http.NewServeMux()
	a.routes(mux)
	req := httptest.NewRequest(http.MethodPost, "/skills/admin/delete-me/delete", nil)
	req.AddCookie(&http.Cookie{Name: "skillsdex_session", Value: session})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/skills" {
		t.Fatalf("delete route = %d location %q, want redirect to /skills", w.Code, w.Header().Get("Location"))
	}
	if _, err := getSkill(ctx, db, admin.ID, "admin", "delete-me"); !isNotFound(err) {
		t.Fatalf("deleted skill should not be readable, err=%v", err)
	}
}

func TestSourceMetaHTMLLinksOnlySafeURLs(t *testing.T) {
	good := sourceMetaHTML(&Skill{SourceProvider: "github", SourceURL: "https://github.com/acme/skills"})
	if !strings.Contains(good, `href="https://github.com/acme/skills"`) || !strings.Contains(good, "GitHub") {
		t.Fatalf("safe source did not render as link: %s", good)
	}

	bad := sourceMetaHTML(&Skill{SourceProvider: "github", SourceURL: "javascript:alert(1)"})
	if strings.Contains(bad, "href=") || !strings.Contains(bad, "GitHub") {
		t.Fatalf("unsafe source rendered incorrectly: %s", bad)
	}
}
