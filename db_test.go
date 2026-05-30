package main

import (
	"context"
	"path/filepath"
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
