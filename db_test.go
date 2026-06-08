package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestInstallSetsAreScopedAndValidateMembership(t *testing.T) {
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
	if err := createUser(ctx, db, "alice", "", "secret", false); err != nil {
		t.Fatal(err)
	}
	alice, err := getUserByUsername(ctx, db, "alice")
	if err != nil {
		t.Fatal(err)
	}
	adminSkill, err := createSkill(ctx, db, admin, SkillInput{
		Owner:       "admin",
		Slug:        "set-skill",
		Name:        "Set Skill",
		Description: "Use when testing install sets.",
		Body:        "# Set Skill\n\nInstructions.",
	})
	if err != nil {
		t.Fatal(err)
	}
	aliceSkill, err := createSkill(ctx, db, alice, SkillInput{
		Owner:       "alice",
		Slug:        "private-set-skill",
		Name:        "Private Set Skill",
		Description: "Use privately.",
		Body:        "# Private",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := createInstallSet(ctx, db, admin.ID, "daily", "Daily", "Everyday skills"); err != nil {
		t.Fatal(err)
	}
	if _, err := updateInstallSet(ctx, db, admin.ID, "daily", "Daily", "Everyday skills", []string{aliceSkill.ID}); err == nil {
		t.Fatal("expected cross-user skill membership to fail")
	}
	updated, err := updateInstallSet(ctx, db, admin.ID, "daily", "Daily", "Everyday skills", []string{adminSkill.ID})
	if err != nil {
		t.Fatal(err)
	}
	if updated.SkillCount != 1 {
		t.Fatalf("skill count = %d, want 1", updated.SkillCount)
	}
	_, skills, err := listInstallSetSkills(ctx, db, admin.ID, "daily")
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 || skills[0].Slug != adminSkill.Slug {
		t.Fatalf("unexpected set skills: %#v", skills)
	}
	if _, _, err := listInstallSetSkills(ctx, db, alice.ID, "daily"); !isNotFound(err) {
		t.Fatalf("alice should not read admin set, err=%v", err)
	}
}

func TestSkillTagsPersistAndFilterOnlyLists(t *testing.T) {
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
	tagged, err := createSkill(ctx, db, admin, SkillInput{
		Owner:       "admin",
		Slug:        "go-review",
		Name:        "Go Review",
		Description: "Use when reviewing Go code.",
		Tags:        []string{"testing", "go", "go"},
		Body:        "# Go Review\n\nReview Go code.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(tagged.Tags, ","); got != "go,testing" {
		t.Fatalf("created tags = %q, want go,testing", got)
	}
	if _, err := createSkill(ctx, db, admin, SkillInput{
		Owner:       "admin",
		Slug:        "python-review",
		Name:        "Python Review",
		Description: "Use when reviewing Python code.",
		Tags:        []string{"python"},
		Body:        "# Python Review\n\nReview Python code.",
	}); err != nil {
		t.Fatal(err)
	}
	goSkills, err := listSkillsFiltered(ctx, db, admin.ID, false, "go")
	if err != nil {
		t.Fatal(err)
	}
	if len(goSkills) != 1 || goSkills[0].Slug != "go-review" {
		t.Fatalf("go filtered skills = %#v", goSkills)
	}
	if _, err := getSkill(ctx, db, admin.ID, "admin", "python-review"); err != nil {
		t.Fatalf("tag filter should not affect direct skill lookup: %v", err)
	}

	if _, err := updateSkill(ctx, db, admin, "admin", "go-review", SkillInput{
		Owner:       "admin",
		Slug:        "go-review",
		Name:        "Go Review",
		Description: "Use when reviewing Go code.",
		Body:        "# Go Review\n\nUpdated.",
	}); err != nil {
		t.Fatal(err)
	}
	preserved, err := getSkill(ctx, db, admin.ID, "admin", "go-review")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(preserved.Tags, ","); got != "go,testing" {
		t.Fatalf("implicit update tags = %q, want preserved tags", got)
	}

	if _, err := updateSkill(ctx, db, admin, "admin", "go-review", SkillInput{
		Owner:        "admin",
		Slug:         "go-review",
		Name:         "Go Review",
		Description:  "Use when reviewing Go code.",
		Tags:         []string{},
		TagsExplicit: true,
		Body:         "# Go Review\n\nUpdated again.",
	}); err != nil {
		t.Fatal(err)
	}
	cleared, err := getSkill(ctx, db, admin.ID, "admin", "go-review")
	if err != nil {
		t.Fatal(err)
	}
	if len(cleared.Tags) != 0 {
		t.Fatalf("explicit empty tags should clear tags: %#v", cleared.Tags)
	}
}

func TestAPITokensAreStoredHashedAndPreviewed(t *testing.T) {
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
	token, err := createAPIToken(ctx, db, admin.ID, "local")
	if err != nil {
		t.Fatal(err)
	}
	var stored, hash, preview string
	if err := db.QueryRow(`SELECT token, token_hash, token_preview FROM api_tokens WHERE user_id = ?`, admin.ID).Scan(&stored, &hash, &preview); err != nil {
		t.Fatal(err)
	}
	if stored == token {
		t.Fatal("api token was stored in plaintext")
	}
	if hash != apiTokenHash(token) {
		t.Fatalf("token hash = %q, want %q", hash, apiTokenHash(token))
	}
	if preview != tokenPreview(token) || strings.Contains(preview, token[16:40]) {
		t.Fatalf("unexpected token preview %q for %q", preview, token)
	}
	if _, err := userForAPIToken(ctx, db, token); err != nil {
		t.Fatalf("hashed token did not authenticate: %v", err)
	}
	tokens, err := listAPITokens(ctx, db, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 1 || tokens[0].TokenPreview != preview {
		t.Fatalf("unexpected listed token: %#v", tokens)
	}
}

func TestLegacyAPITokensAreMigratedToHashes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "skillsdex.db")
	db, err := openDB(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	admin, err := getUserByUsername(ctx, db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	legacyToken := "sdx_legacyplaintexttoken1234567890"
	legacyID := newID("tok")
	if _, err := db.Exec(`INSERT INTO api_tokens (id, user_id, name, token, token_hash, token_preview, created_at) VALUES (?, ?, 'legacy', ?, '', '', ?)`, legacyID, admin.ID, legacyToken, nowUTC().Format(time.RFC3339)); err != nil {
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
	var stored, hash, preview string
	if err := db.QueryRow(`SELECT token, token_hash, token_preview FROM api_tokens WHERE id = ?`, legacyID).Scan(&stored, &hash, &preview); err != nil {
		t.Fatal(err)
	}
	if stored == legacyToken || stored != "stored:"+legacyID {
		t.Fatalf("legacy token placeholder = %q", stored)
	}
	if hash != apiTokenHash(legacyToken) || preview != tokenPreview(legacyToken) {
		t.Fatalf("legacy token hash/preview = %q %q", hash, preview)
	}
	if _, err := userForAPIToken(ctx, db, legacyToken); err != nil {
		t.Fatalf("migrated legacy token did not authenticate: %v", err)
	}
}

func TestMigrateLegacyDatabaseShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "skillsdex.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	created := nowUTC().AddDate(0, 0, -10).Format(time.RFC3339)
	expires := nowUTC().AddDate(0, 0, 10).Format(time.RFC3339)
	userID := newID("usr")
	skillID := newID("skl")
	revisionID := newID("rev")
	tokenID := newID("tok")
	legacyToken := "sdx_legacyfullschema1234567890"
	legacySession := "legacy-session-token"
	for _, stmt := range []string{
		`CREATE TABLE users (
			id TEXT PRIMARY KEY,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			is_admin INTEGER NOT NULL DEFAULT 0,
			deactivated_at TEXT,
			email TEXT UNIQUE,
			created_at TEXT NOT NULL
		);`,
		`CREATE TABLE api_tokens (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id),
			name TEXT NOT NULL,
			token TEXT NOT NULL UNIQUE,
			created_at TEXT NOT NULL,
			last_used_at TEXT,
			revoked_at TEXT
		);`,
		`CREATE TABLE skills (
			id TEXT PRIMARY KEY,
			owner_type TEXT NOT NULL,
			owner_id TEXT NOT NULL,
			owner_slug TEXT NOT NULL,
			slug TEXT NOT NULL,
			name TEXT NOT NULL,
			description TEXT NOT NULL,
			short_description TEXT,
			current_revision_id TEXT,
			archived_at TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(owner_slug, slug)
		);`,
		`CREATE TABLE skill_revisions (
			id TEXT PRIMARY KEY,
			skill_id TEXT NOT NULL REFERENCES skills(id),
			revision_number INTEGER NOT NULL,
			frontmatter_json TEXT NOT NULL,
			manifest_json TEXT NOT NULL,
			tree_hash TEXT NOT NULL,
			created_at TEXT NOT NULL,
			created_by TEXT NOT NULL REFERENCES users(id),
			UNIQUE(skill_id, revision_number)
		);`,
		`CREATE TABLE skill_revision_files (
			id TEXT PRIMARY KEY,
			revision_id TEXT NOT NULL REFERENCES skill_revisions(id),
			path TEXT NOT NULL,
			content BLOB NOT NULL,
			content_type TEXT NOT NULL,
			content_hash TEXT NOT NULL,
			size_bytes INTEGER NOT NULL,
			executable INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			UNIQUE(revision_id, path)
		);`,
		`CREATE TABLE web_sessions (
			token TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id),
			created_at TEXT NOT NULL,
			expires_at TEXT NOT NULL
		);`,
		`CREATE INDEX idx_skills_owner ON skills(owner_id, archived_at, updated_at);`,
		`CREATE INDEX idx_revisions_skill ON skill_revisions(skill_id, revision_number);`,
	} {
		if _, err := legacy.Exec(stmt); err != nil {
			legacy.Close()
			t.Fatal(err)
		}
	}
	if _, err := legacy.Exec(`INSERT INTO users (id, username, password_hash, is_admin, created_at) VALUES (?, 'admin', 'legacy-hash', 1, ?)`, userID, created); err != nil {
		legacy.Close()
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`INSERT INTO api_tokens (id, user_id, name, token, created_at) VALUES (?, ?, 'legacy-cli', ?, ?)`, tokenID, userID, legacyToken, created); err != nil {
		legacy.Close()
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`INSERT INTO web_sessions (token, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`, legacySession, userID, created, expires); err != nil {
		legacy.Close()
		t.Fatal(err)
	}
	raw := "---\nname: Legacy Skill\ndescription: Use when testing legacy migrations.\n---\n\n# Legacy Skill\n"
	if _, err := legacy.Exec(`INSERT INTO skills (id, owner_type, owner_id, owner_slug, slug, name, description, short_description, current_revision_id, created_at, updated_at) VALUES (?, 'user', ?, 'admin', 'legacy-skill', 'Legacy Skill', 'Use when testing legacy migrations.', 'Legacy migration fixture', ?, ?, ?)`, skillID, userID, revisionID, created, created); err != nil {
		legacy.Close()
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`INSERT INTO skill_revisions (id, skill_id, revision_number, frontmatter_json, manifest_json, tree_hash, created_at, created_by) VALUES (?, ?, 1, '{}', '{}', ?, ?, ?)`, revisionID, skillID, sha256String([]byte(raw)), created, userID); err != nil {
		legacy.Close()
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`INSERT INTO skill_revision_files (id, revision_id, path, content, content_type, content_hash, size_bytes, executable, created_at) VALUES (?, ?, 'SKILL.md', ?, 'text/markdown; charset=utf-8', ?, ?, 0, ?)`, newID("fil"), revisionID, []byte(raw), sha256String([]byte(raw)), len(raw), created); err != nil {
		legacy.Close()
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := openDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	admin, err := getUserByUsername(ctx, db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if admin.MustChangePassword {
		t.Fatal("legacy admin should not be forced to change password after migration")
	}
	if _, err := userForAPIToken(ctx, db, legacyToken); err != nil {
		t.Fatalf("legacy api token should authenticate after hash migration: %v", err)
	}
	var storedToken, migratedHash, migratedPreview string
	if err := db.QueryRow(`SELECT token, token_hash, token_preview FROM api_tokens WHERE id = ?`, tokenID).Scan(&storedToken, &migratedHash, &migratedPreview); err != nil {
		t.Fatal(err)
	}
	if storedToken == legacyToken || migratedHash != apiTokenHash(legacyToken) || migratedPreview != tokenPreview(legacyToken) {
		t.Fatalf("legacy token was not migrated securely: stored=%q hash=%q preview=%q", storedToken, migratedHash, migratedPreview)
	}
	if _, err := userForSession(ctx, db, legacySession); err != nil {
		t.Fatalf("legacy session should remain valid after migration: %v", err)
	}
	csrf, err := csrfTokenForSession(ctx, db, legacySession)
	if err != nil {
		t.Fatal(err)
	}
	if csrf != legacySession {
		t.Fatalf("legacy session csrf token = %q, want session token fallback", csrf)
	}
	skill, err := getSkill(ctx, db, admin.ID, "admin", "legacy-skill")
	if err != nil {
		t.Fatal(err)
	}
	if skill.RecommendedDefault || skill.PinnedInstall || skill.SourceProvider != "" || skill.SourceURL != "" {
		t.Fatalf("new skill metadata columns should default empty/false after migration: %#v", skill)
	}
	files, _, err := currentSkillFiles(ctx, db, admin.ID, "admin", "legacy-skill")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "SKILL.md" || !strings.Contains(files[0].Content, "# Legacy Skill") {
		t.Fatalf("legacy skill file not readable after migration: %#v", files)
	}
	if _, err := createInstallSet(ctx, db, admin.ID, "migration", "Migration", "Migration smoke set"); err != nil {
		t.Fatalf("new install set table should exist after migration: %v", err)
	}
	if err := createAuditEvent(ctx, db, AuditEvent{ActorUserID: admin.ID, ActorUsername: admin.Username, Action: "migration_smoke", TargetType: "test"}); err != nil {
		t.Fatalf("new audit table should exist after migration: %v", err)
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

func TestSkillRevisionStoresSupportingFiles(t *testing.T) {
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
		Slug:        "folder-skill",
		Name:        "Folder Skill",
		Description: "Use when testing full skill folders.",
		Body:        "# Folder Skill",
		Files: []SkillFile{
			{Path: "references/guide.md", Content: "# Guide\n"},
			{Path: "scripts/run.sh", Content: "#!/bin/sh\n", Executable: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	files, got, err := currentSkillFiles(ctx, db, admin.ID, "admin", "folder-skill")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != skill.ID {
		t.Fatalf("skill id = %q, want %q", got.ID, skill.ID)
	}
	if len(files) != 3 {
		t.Fatalf("file count = %d, want 3: %#v", len(files), files)
	}
	if files[1].Path != "references/guide.md" || files[1].Content != "# Guide\n" {
		t.Fatalf("unexpected reference file: %#v", files[1])
	}
	if files[2].Path != "scripts/run.sh" || !files[2].Executable {
		t.Fatalf("unexpected script file: %#v", files[2])
	}
	var manifestRaw string
	if err := db.QueryRow(`SELECT manifest_json FROM skill_revisions WHERE id = ?`, skill.CurrentRevisionID).Scan(&manifestRaw); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(manifestRaw, "references/guide.md") || !strings.Contains(manifestRaw, "scripts/run.sh") {
		t.Fatalf("manifest did not include supporting files: %s", manifestRaw)
	}
}
