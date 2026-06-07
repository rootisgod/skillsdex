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

func TestRunInstallPinnedInstallsPinnedSkills(t *testing.T) {
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
	pinned, err := createSkill(context.Background(), db, admin, SkillInput{
		Owner:         "admin",
		Slug:          "pinned-skill",
		Name:          "Pinned Skill",
		Description:   "Use on every new machine.",
		PinnedInstall: true,
		Body:          "# Pinned Skill",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := createSkill(context.Background(), db, admin, SkillInput{
		Owner:       "admin",
		Slug:        "unpinned-skill",
		Name:        "Unpinned Skill",
		Description: "Use only sometimes.",
		Body:        "# Unpinned Skill",
	}); err != nil {
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
	if err := runInstallPinned([]string{"--target", "codex"}); err != nil {
		t.Fatal(err)
	}

	manifest, err := loadManifest()
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Installs) != 1 {
		t.Fatalf("install count = %d, want 1", len(manifest.Installs))
	}
	if manifest.Installs[0].Slug != pinned.Slug {
		t.Fatalf("installed slug = %q, want %q", manifest.Installs[0].Slug, pinned.Slug)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "skills", "admin--pinned-skill", "SKILL.md")); err != nil {
		t.Fatalf("expected pinned skill to be installed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "skills", "admin--unpinned-skill", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("unpinned skill should not be installed, err=%v", err)
	}
}
