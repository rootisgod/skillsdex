package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultAdminAndTenantScopedSkills(t *testing.T) {
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
	if !admin.IsAdmin {
		t.Fatal("default admin is not admin")
	}
	raw, skill, err := rawSkill(ctx, db, admin.ID, "admin", "five-advisor-review")
	if err != nil {
		t.Fatal(err)
	}
	if skill.Name != "Five Advisor Review" {
		t.Fatalf("default skill name = %q", skill.Name)
	}
	if !strings.Contains(string(raw), "Use five advisors") {
		t.Fatalf("default skill body missing advisor instructions:\n%s", raw)
	}
	skills, err := listSkills(ctx, db, admin.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != len(defaultSkills()) {
		t.Fatalf("default skill count = %d, want %d", len(skills), len(defaultSkills()))
	}
	if err := createUser(ctx, db, "alice", "", "secret", false); err != nil {
		t.Fatal(err)
	}
	alice, err := getUserByUsername(ctx, db, "alice")
	if err != nil {
		t.Fatal(err)
	}
	_, err = createSkill(ctx, db, alice, SkillInput{
		Owner:       "alice",
		Slug:        "debugging",
		Name:        "debugging",
		Description: "Use when debugging.",
		Body:        "# Debugging",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := getSkill(ctx, db, admin.ID, "alice", "debugging"); !isNotFound(err) {
		t.Fatalf("admin should not read alice skill through owner slug, err=%v", err)
	}
}

func TestDefaultSkillSeedIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "skillsdex.db")
	db, err := openDB(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = openDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM skills WHERE owner_slug = 'admin'`).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != len(defaultSkills()) {
		t.Fatalf("default skill count = %d, want %d", count, len(defaultSkills()))
	}
}
