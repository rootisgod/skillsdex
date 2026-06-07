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
	for _, s := range skills {
		if !s.RecommendedDefault {
			t.Fatalf("default skill %s/%s is not marked recommended", s.OwnerSlug, s.Slug)
		}
	}
	if err := createUser(ctx, db, "alice", "", "secret", false); err != nil {
		t.Fatal(err)
	}
	alice, err := getUserByUsername(ctx, db, "alice")
	if err != nil {
		t.Fatal(err)
	}
	_, err = createSkill(ctx, db, alice, SkillInput{
		Owner:          "alice",
		Slug:           "debugging",
		Name:           "debugging",
		Description:    "Use when debugging.",
		PinnedInstall:  true,
		SourceProvider: "github",
		SourceURL:      "https://github.com/acme/skills/tree/main/debugging",
		Body:           "# Debugging",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := getSkill(ctx, db, admin.ID, "alice", "debugging"); !isNotFound(err) {
		t.Fatalf("admin should not read alice skill through owner slug, err=%v", err)
	}
	aliceSkill, err := getSkill(ctx, db, alice.ID, "alice", "debugging")
	if err != nil {
		t.Fatal(err)
	}
	if aliceSkill.RecommendedDefault {
		t.Fatal("new user skill should not be recommended by default")
	}
	if !aliceSkill.PinnedInstall {
		t.Fatal("pinned install metadata was not stored")
	}
	if aliceSkill.SourceProvider != "github" || aliceSkill.SourceURL != "https://github.com/acme/skills/tree/main/debugging" {
		t.Fatalf("source metadata was not stored: %#v", aliceSkill)
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

func TestRecommendedDefaultCanBeUnsetOnSeededSkill(t *testing.T) {
	path := filepath.Join(t.TempDir(), "skillsdex.db")
	db, err := openDB(path)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := getUserByUsername(context.Background(), db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	skill, err := getSkill(context.Background(), db, admin.ID, "admin", "five-advisor-review")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE skills SET recommended_default = 0 WHERE id = ?`, skill.ID); err != nil {
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
	admin, err = getUserByUsername(context.Background(), db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	skill, err = getSkill(context.Background(), db, admin.ID, "admin", "five-advisor-review")
	if err != nil {
		t.Fatal(err)
	}
	if skill.RecommendedDefault {
		t.Fatal("seeded skill was re-marked recommended after user unset it")
	}
}

func TestDeleteSkillRemovesRevisionsAndFiles(t *testing.T) {
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
	skill, err := createSkill(ctx, db, admin, SkillInput{
		Owner:       "admin",
		Slug:        "temporary",
		Name:        "Temporary",
		Description: "Use temporarily.",
		Body:        "# Temporary",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := updateSkill(ctx, db, admin, "admin", "temporary", SkillInput{
		Owner:       "admin",
		Slug:        "temporary",
		Name:        "Temporary",
		Description: "Use temporarily.",
		Body:        "# Temporary\n\nUpdated.",
	}); err != nil {
		t.Fatal(err)
	}
	if err := deleteSkill(ctx, db, admin.ID, "admin", "temporary"); err != nil {
		t.Fatal(err)
	}
	if _, err := getSkill(ctx, db, admin.ID, "admin", "temporary"); !isNotFound(err) {
		t.Fatalf("deleted skill should not be readable, err=%v", err)
	}
	var revisions int
	if err := db.QueryRow(`SELECT COUNT(*) FROM skill_revisions WHERE skill_id = ?`, skill.ID).Scan(&revisions); err != nil {
		t.Fatal(err)
	}
	if revisions != 0 {
		t.Fatalf("revision count after delete = %d, want 0", revisions)
	}
	var files int
	if err := db.QueryRow(`SELECT COUNT(*) FROM skill_revision_files WHERE revision_id NOT IN (SELECT id FROM skill_revisions)`).Scan(&files); err != nil {
		t.Fatal(err)
	}
	if files != 0 {
		t.Fatalf("orphaned revision file count = %d, want 0", files)
	}
}
