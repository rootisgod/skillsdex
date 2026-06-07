package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestRunInstallDefaultsInstallsRecommendedSkills(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	admin, err := getUserByUsername(context.Background(), db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	token, err := createAPIToken(context.Background(), db, admin.ID, "test")
	if err != nil {
		t.Fatal(err)
	}

	a := &app{db: db}
	mux := http.NewServeMux()
	a.routes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	if err := saveAuth(AuthConfig{ServerURL: server.URL, Token: token}); err != nil {
		t.Fatal(err)
	}
	if err := runInstallDefaults([]string{"--target", "codex"}); err != nil {
		t.Fatal(err)
	}

	manifest, err := loadManifest()
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Installs) != len(defaultSkills()) {
		t.Fatalf("install count = %d, want %d", len(manifest.Installs), len(defaultSkills()))
	}
	wantPath := filepath.Join(home, ".codex", "skills", "admin--five-advisor-review", "SKILL.md")
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("expected installed skill at %s: %v", wantPath, err)
	}
}
