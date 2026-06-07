package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

func openDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA foreign_keys = ON; PRAGMA busy_timeout = 5000;`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func migrate(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			is_admin INTEGER NOT NULL DEFAULT 0,
			deactivated_at TEXT,
			email TEXT UNIQUE,
			created_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS api_tokens (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id),
			name TEXT NOT NULL,
			token TEXT NOT NULL UNIQUE,
			created_at TEXT NOT NULL,
			last_used_at TEXT,
			revoked_at TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS skills (
			id TEXT PRIMARY KEY,
			owner_type TEXT NOT NULL,
			owner_id TEXT NOT NULL,
			owner_slug TEXT NOT NULL,
			slug TEXT NOT NULL,
			name TEXT NOT NULL,
			description TEXT NOT NULL,
			short_description TEXT,
			recommended_default INTEGER NOT NULL DEFAULT 0,
			pinned_install INTEGER NOT NULL DEFAULT 0,
			source_provider TEXT,
			source_url TEXT,
			current_revision_id TEXT,
			archived_at TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(owner_slug, slug)
		);`,
		`CREATE TABLE IF NOT EXISTS skill_revisions (
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
		`CREATE TABLE IF NOT EXISTS skill_revision_files (
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
		`CREATE TABLE IF NOT EXISTS web_sessions (
			token TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id),
			created_at TEXT NOT NULL,
			expires_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_skills_owner ON skills(owner_id, archived_at, updated_at);`,
		`CREATE INDEX IF NOT EXISTS idx_revisions_skill ON skill_revisions(skill_id, revision_number);`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	addedRecommendedDefault, err := addColumnIfMissing(db, "skills", "recommended_default", "recommended_default INTEGER NOT NULL DEFAULT 0")
	if err != nil {
		return err
	}
	if _, err := addColumnIfMissing(db, "skills", "source_provider", "source_provider TEXT"); err != nil {
		return err
	}
	if _, err := addColumnIfMissing(db, "skills", "source_url", "source_url TEXT"); err != nil {
		return err
	}
	if _, err := addColumnIfMissing(db, "skills", "pinned_install", "pinned_install INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := ensureDefaultAdmin(db); err != nil {
		return err
	}
	return ensureDefaultSkills(db, addedRecommendedDefault)
}

func addColumnIfMissing(db *sql.DB, table, column, definition string) (bool, error) {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var dfltValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dfltValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return false, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	_, err = db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + definition)
	return err == nil, err
}

func ensureDefaultAdmin(db *sql.DB) error {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO users (id, username, password_hash, is_admin, created_at) VALUES (?, ?, ?, 1, ?)`,
		newID("usr"), "admin", string(hash), nowUTC().Format(time.RFC3339))
	return err
}

func ensureDefaultSkills(db *sql.DB, markExistingRecommended bool) error {
	ctx := context.Background()
	admin, err := getUserByUsername(ctx, db, "admin")
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return err
	}
	for _, skill := range defaultSkills() {
		existing, err := getSkill(ctx, db, admin.ID, "admin", skill.Slug)
		if err == nil {
			if markExistingRecommended && existing.RecommendedDefault != skill.RecommendedDefault {
				if _, err := db.ExecContext(ctx, `UPDATE skills SET recommended_default = ? WHERE id = ?`, boolInt(skill.RecommendedDefault), existing.ID); err != nil {
					return err
				}
			}
			continue
		}
		if !isNotFound(err) {
			return err
		}
		if _, err = createSkill(ctx, db, admin, skill); err != nil {
			return err
		}
	}
	return nil
}

func defaultSkills() []SkillInput {
	skills := []SkillInput{
		{
			Owner:            "admin",
			Slug:             "five-advisor-review",
			Name:             "Five Advisor Review",
			Description:      "Use when you need a decision, plan, proposal, or idea stress-tested from multiple viewpoints before choosing a concrete next step.",
			ShortDescription: "Stress-test decisions with five shuffled advisors",
			Body: `# Five Advisor Review

Use five advisors:

1. Contrarian - what could fail?
2. First Principles - what problem are we really solving?
3. Expansionist - what upside are we missing?
4. Outsider - does this make sense to someone new?
5. Executor - what would you actually do Monday morning?

Have each advisor give a short answer.

Then anonymize and shuffle their responses, labeling them Response A through Response E. Make the advisors peer-review the anonymous responses:

- Which response is strongest?
- Which has the biggest blind spot?
- What did all five miss?

Finish with:

- Best recommendation
- Biggest blind spot
- What everyone missed
- One concrete next step`,
		},
		{
			Owner:            "admin",
			Slug:             "systematic-debugging",
			Name:             "Systematic Debugging",
			Description:      "Use when tests fail, runtime behavior is wrong, or a regression needs to be isolated without guessing.",
			ShortDescription: "Debug failures with evidence and minimal changes",
			Body: `# Systematic Debugging

Start by stating the exact failure, expected behavior, observed behavior, and the smallest reproduction available.

Work in this order:

1. Reproduce the issue with the narrowest command, test, or input.
2. Identify the boundary where correct behavior becomes incorrect.
3. List two or three plausible causes, then rank them by evidence.
4. Inspect code, logs, state, and recent changes before editing.
5. Make the smallest targeted fix.
6. Re-run the failing check and at least one nearby regression check.

Avoid broad rewrites, speculative fixes, and changing unrelated behavior.

Finish with:

- Root cause
- Fix made or recommended
- Verification run
- Remaining risk`,
		},
		{
			Owner:            "admin",
			Slug:             "code-review",
			Name:             "Code Review",
			Description:      "Use when reviewing a code change for bugs, regressions, missing tests, security issues, and maintainability risks.",
			ShortDescription: "Review code with findings first",
			Body: `# Code Review

Review as a senior engineer looking for concrete defects, not style preferences.

Prioritize:

1. Correctness bugs and behavioral regressions.
2. Security, privacy, and data integrity risks.
3. Missing tests for changed behavior.
4. Edge cases, concurrency, error handling, and migration safety.
5. Maintainability issues only when they create real risk.

For every finding, include:

- Severity
- File and line reference when available
- What can fail
- Why it can fail
- A specific fix direction

Keep summaries short. If there are no findings, say that clearly and mention residual test gaps.`,
		},
		{
			Owner:            "admin",
			Slug:             "test-strategy",
			Name:             "Test Strategy",
			Description:      "Use when deciding what tests to add for a feature, bug fix, refactor, migration, or API change.",
			ShortDescription: "Choose focused tests for the real risk",
			Body: `# Test Strategy

Start from the behavior that could break, not from implementation details.

Create a compact test plan with:

1. Core happy path.
2. Important failure path.
3. Boundary or edge case.
4. Regression case for the reported bug or changed contract.
5. Integration or end-to-end check only if unit tests cannot cover the risk.

For each test, state:

- What it proves
- Why it matters
- Setup data
- Expected assertion

Prefer a small number of high-signal tests over exhaustive low-value coverage. Call out tests that are intentionally deferred and why.`,
		},
		{
			Owner:            "admin",
			Slug:             "implementation-plan",
			Name:             "Implementation Plan",
			Description:      "Use before building a non-trivial change that needs sequencing, scope control, and verification.",
			ShortDescription: "Turn a request into a scoped build plan",
			Body: `# Implementation Plan

Read the relevant code and existing patterns before proposing changes.

Produce a short plan with:

1. Goal and non-goals.
2. Files or modules likely to change.
3. Data model, API, or UI contracts affected.
4. Step-by-step implementation sequence.
5. Verification commands or manual checks.
6. Rollback or mitigation notes for risky changes.

Keep the plan practical. Prefer the smallest design that satisfies the request and fits the current codebase.

If the request is ambiguous, state the assumption you will use and why it is reasonable.`,
		},
		{
			Owner:            "admin",
			Slug:             "research-brief",
			Name:             "Research Brief",
			Description:      "Use when gathering current information, comparing options, or summarizing sources for a decision.",
			ShortDescription: "Research with sources, dates, and decision relevance",
			Body: `# Research Brief

Gather current information from primary or high-quality sources when facts may have changed.

Structure the brief as:

1. Question being answered.
2. Short answer.
3. Key findings with dates where relevant.
4. Source notes and links.
5. Conflicts, uncertainty, or stale information.
6. Recommendation or next research step.

Do not overquote. Paraphrase source material and cite links. Separate confirmed facts from inference.`,
		},
		{
			Owner:            "admin",
			Slug:             "documentation-update",
			Name:             "Documentation Update",
			Description:      "Use when writing or revising README, API docs, runbooks, changelogs, setup guides, or user-facing instructions.",
			ShortDescription: "Write docs that match real behavior",
			Body: `# Documentation Update

Write documentation from the user's task, not from the implementation order.

Before editing, verify:

- Current command names, flags, paths, and defaults.
- Required prerequisites.
- Expected outputs or side effects.
- Known limitations.

Use concise sections, runnable examples, and concrete paths. Avoid marketing language and unsupported claims.

Finish by checking that every documented command still matches the code or tested behavior.`,
		},
		{
			Owner:            "admin",
			Slug:             "security-review",
			Name:             "Security Review",
			Description:      "Use when evaluating auth, authorization, tokens, file writes, secrets, web handlers, or data isolation.",
			ShortDescription: "Check security boundaries and abuse cases",
			Body: `# Security Review

Review the change from an abuse-case perspective.

Check:

1. Authentication: who proves identity and how?
2. Authorization: what tenant, owner, or role boundary is enforced?
3. Secrets: are tokens, keys, passwords, and logs handled safely?
4. Inputs: are paths, URLs, JSON, forms, and filenames validated?
5. Writes: can the change overwrite, delete, or expose unintended data?
6. Defaults: are first-run and local-development shortcuts clearly scoped?

Output:

- Highest-risk issue
- Concrete exploit or failure scenario
- Minimal fix
- Residual risk after the fix`,
		},
		{
			Owner:            "admin",
			Slug:             "frontend-polish",
			Name:             "Frontend Polish",
			Description:      "Use when improving a web UI for clarity, responsive behavior, accessibility, and practical workflow quality.",
			ShortDescription: "Improve UI usability without decorative churn",
			Body: `# Frontend Polish

Evaluate the interface as a working tool.

Check:

1. Primary task is visible and efficient.
2. Controls use familiar patterns and clear labels.
3. Text fits at mobile and desktop widths.
4. Empty, loading, error, and success states are handled.
5. Color, spacing, and typography support scanning.
6. Keyboard and screen-reader basics are respected.

Make focused changes that improve the workflow. Avoid decorative redesigns unless the request is explicitly visual.`,
		},
		{
			Owner:            "admin",
			Slug:             "product-interface-redesign",
			Name:             "Product Interface Redesign",
			Description:      "Use when a web app interface needs a substantial quality jump across layout, visual hierarchy, workflow clarity, accessibility, and responsive behavior.",
			ShortDescription: "Redesign app UI with product context and concrete implementation",
			Body: `# Product Interface Redesign

Use this when the current interface feels too rough, generic, cramped, confusing, or unfinished.

Start by identifying:

- Primary users and their repeated jobs.
- The most important screen and the most important action on that screen.
- Existing design constraints, components, CSS, framework, and data density.
- What must stay simple, fast, and server-rendered.

Audit the current UI across:

1. Information architecture: navigation, page titles, hierarchy, and wayfinding.
2. Workflow clarity: create, edit, preview, install, token setup, errors, and empty states.
3. Visual system: spacing, typography, contrast, buttons, forms, tables, panels, and status treatments.
4. Interaction quality: keyboard use, focus states, confirmation flows, and destructive actions.
5. Responsive behavior: mobile, tablet, and desktop layouts without text overlap.
6. Accessibility: semantic structure, labels, color contrast, focus visibility, and readable sizes.

Design direction:

- For operational tools, prefer calm, dense, scannable layouts over marketing-style pages.
- Make the first-run path obvious without adding explanatory clutter everywhere.
- Use restrained color with clear accents for actions and status.
- Keep repeated content easy to compare in tables or compact lists.
- Do not nest cards inside cards.
- Do not use decorative gradients, blobs, or oversized hero sections for app workflows.

Before editing, describe the target interface in one paragraph and list the concrete screens/components to change.

When implementing:

1. Improve the layout and component system first.
2. Add explicit empty, error, and success states where they matter.
3. Tighten form ergonomics and button placement.
4. Verify mobile and desktop behavior.
5. Keep scope focused on the requested UI improvement.

Finish with:

- What changed visually
- What workflow became easier
- Accessibility or responsive checks performed
- Remaining UI debt
- One next polish step`,
		},
		{
			Owner:            "admin",
			Slug:             "pull-request-summary",
			Name:             "Pull Request Summary",
			Description:      "Use when summarizing a completed change for a pull request, commit message, release note, or handoff.",
			ShortDescription: "Summarize changes, tests, and risk",
			Body: `# Pull Request Summary

Summarize the change for a reviewer who needs to understand impact quickly.

Include:

- What changed
- Why it changed
- User-visible behavior
- Data model, API, or migration impact
- Tests and checks run
- Known gaps or follow-up work

Keep it factual. Do not claim tests were run unless they were run. Mention any manual verification separately from automated tests.`,
		},
	}
	for i := range skills {
		skills[i].RecommendedDefault = true
	}
	return skills
}

func getUserByUsername(ctx context.Context, db *sql.DB, username string) (*User, error) {
	row := db.QueryRowContext(ctx, `SELECT id, username, password_hash, is_admin, COALESCE(deactivated_at,''), COALESCE(email,''), created_at FROM users WHERE username = ?`, username)
	return scanUser(row)
}

func getUserByID(ctx context.Context, db *sql.DB, id string) (*User, error) {
	row := db.QueryRowContext(ctx, `SELECT id, username, password_hash, is_admin, COALESCE(deactivated_at,''), COALESCE(email,''), created_at FROM users WHERE id = ?`, id)
	return scanUser(row)
}

type scanner interface {
	Scan(dest ...any) error
}

func scanUser(row scanner) (*User, error) {
	var u User
	var deactivated, created string
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.IsAdmin, &deactivated, &u.Email, &created); err != nil {
		return nil, err
	}
	u.DeactivatedAt = nullableTime(deactivated)
	u.CreatedAt = parseTime(created)
	return &u, nil
}

func listUsers(ctx context.Context, db *sql.DB) ([]User, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, username, password_hash, is_admin, COALESCE(deactivated_at,''), COALESCE(email,''), created_at FROM users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, *u)
	}
	return users, rows.Err()
}

func createUser(ctx context.Context, db *sql.DB, username, email, password string, isAdmin bool) error {
	if err := validateSlug("username", username); err != nil {
		return err
	}
	if strings.TrimSpace(password) == "" {
		return errors.New("password is required")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `INSERT INTO users (id, username, password_hash, is_admin, email, created_at) VALUES (?, ?, ?, ?, NULLIF(?, ''), ?)`,
		newID("usr"), username, string(hash), boolInt(isAdmin), strings.TrimSpace(email), nowUTC().Format(time.RFC3339))
	return err
}

func updateUser(ctx context.Context, db *sql.DB, id, email, password string, isAdmin, deactivated bool) error {
	if strings.TrimSpace(password) != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		_, err = db.ExecContext(ctx, `UPDATE users SET password_hash = ?, is_admin = ?, email = NULLIF(?, ''), deactivated_at = ? WHERE id = ?`,
			string(hash), boolInt(isAdmin), strings.TrimSpace(email), deactivatedAt(deactivated), id)
		return err
	}
	_, err := db.ExecContext(ctx, `UPDATE users SET is_admin = ?, email = NULLIF(?, ''), deactivated_at = ? WHERE id = ?`,
		boolInt(isAdmin), strings.TrimSpace(email), deactivatedAt(deactivated), id)
	return err
}

func deactivatedAt(deactivated bool) any {
	if !deactivated {
		return nil
	}
	return nowUTC().Format(time.RFC3339)
}

func createWebSession(ctx context.Context, db *sql.DB, userID string) (string, error) {
	token := newToken()
	now := nowUTC()
	_, err := db.ExecContext(ctx, `INSERT INTO web_sessions (token, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		token, userID, now.Format(time.RFC3339), now.Add(30*24*time.Hour).Format(time.RFC3339))
	return token, err
}

func userForSession(ctx context.Context, db *sql.DB, token string) (*User, error) {
	row := db.QueryRowContext(ctx, `SELECT u.id, u.username, u.password_hash, u.is_admin, COALESCE(u.deactivated_at,''), COALESCE(u.email,''), u.created_at
		FROM web_sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token = ? AND s.expires_at > ? AND u.deactivated_at IS NULL`, token, nowUTC().Format(time.RFC3339))
	return scanUser(row)
}

func deleteWebSession(ctx context.Context, db *sql.DB, token string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM web_sessions WHERE token = ?`, token)
	return err
}

func createAPIToken(ctx context.Context, db *sql.DB, userID, name string) (string, error) {
	token := newToken()
	_, err := db.ExecContext(ctx, `INSERT INTO api_tokens (id, user_id, name, token, created_at) VALUES (?, ?, ?, ?, ?)`,
		newID("tok"), userID, strings.TrimSpace(name), token, nowUTC().Format(time.RFC3339))
	return token, err
}

func listAPITokens(ctx context.Context, db *sql.DB, userID string) ([]APIToken, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, user_id, name, token, created_at, COALESCE(last_used_at,''), COALESCE(revoked_at,'') FROM api_tokens WHERE user_id = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tokens []APIToken
	for rows.Next() {
		var t APIToken
		var created string
		if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &t.Token, &created, &t.LastUsedAt, &t.RevokedAt); err != nil {
			return nil, err
		}
		t.CreatedAt = parseTime(created)
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}

func revokeAPIToken(ctx context.Context, db *sql.DB, userID, tokenID string) error {
	_, err := db.ExecContext(ctx, `UPDATE api_tokens SET revoked_at = ? WHERE id = ? AND user_id = ?`, nowUTC().Format(time.RFC3339), tokenID, userID)
	return err
}

func userForAPIToken(ctx context.Context, db *sql.DB, token string) (*User, error) {
	row := db.QueryRowContext(ctx, `SELECT u.id, u.username, u.password_hash, u.is_admin, COALESCE(u.deactivated_at,''), COALESCE(u.email,''), u.created_at
		FROM api_tokens t JOIN users u ON u.id = t.user_id
		WHERE t.token = ? AND t.revoked_at IS NULL AND u.deactivated_at IS NULL`, token)
	u, err := scanUser(row)
	if err != nil {
		return nil, err
	}
	_, _ = db.ExecContext(ctx, `UPDATE api_tokens SET last_used_at = ? WHERE token = ?`, nowUTC().Format(time.RFC3339), token)
	return u, nil
}

func listSkills(ctx context.Context, db *sql.DB, userID string, includeArchived bool) ([]Skill, error) {
	q := `SELECT s.id, s.owner_type, s.owner_id, s.owner_slug, s.slug, s.name, s.description, COALESCE(s.short_description,''), s.recommended_default, s.pinned_install, COALESCE(s.source_provider,''), COALESCE(s.source_url,''), COALESCE(s.current_revision_id,''), COALESCE(r.revision_number,0), COALESCE(s.archived_at,''), s.created_at, s.updated_at
		FROM skills s LEFT JOIN skill_revisions r ON r.id = s.current_revision_id
		WHERE s.owner_id = ?`
	args := []any{userID}
	if !includeArchived {
		q += ` AND s.archived_at IS NULL`
	}
	q += ` ORDER BY s.updated_at DESC`
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var skills []Skill
	for rows.Next() {
		s, err := scanSkill(rows)
		if err != nil {
			return nil, err
		}
		skills = append(skills, *s)
	}
	return skills, rows.Err()
}

func getSkill(ctx context.Context, db *sql.DB, userID, owner, slug string) (*Skill, error) {
	row := db.QueryRowContext(ctx, `SELECT s.id, s.owner_type, s.owner_id, s.owner_slug, s.slug, s.name, s.description, COALESCE(s.short_description,''), s.recommended_default, s.pinned_install, COALESCE(s.source_provider,''), COALESCE(s.source_url,''), COALESCE(s.current_revision_id,''), COALESCE(r.revision_number,0), COALESCE(s.archived_at,''), s.created_at, s.updated_at
		FROM skills s LEFT JOIN skill_revisions r ON r.id = s.current_revision_id
		WHERE s.owner_id = ? AND s.owner_slug = ? AND s.slug = ?`, userID, owner, slug)
	return scanSkill(row)
}

func scanSkill(row scanner) (*Skill, error) {
	var s Skill
	var created, updated string
	if err := row.Scan(&s.ID, &s.OwnerType, &s.OwnerID, &s.OwnerSlug, &s.Slug, &s.Name, &s.Description, &s.ShortDescription, &s.RecommendedDefault, &s.PinnedInstall, &s.SourceProvider, &s.SourceURL, &s.CurrentRevisionID, &s.RevisionNumber, &s.ArchivedAt, &created, &updated); err != nil {
		return nil, err
	}
	s.CreatedAt = parseTime(created)
	s.UpdatedAt = parseTime(updated)
	return &s, nil
}

func createSkill(ctx context.Context, db *sql.DB, user *User, in SkillInput) (*Skill, error) {
	if in.Owner != user.Username {
		return nil, errors.New("owner must match your username")
	}
	if err := validateSlug("slug", in.Slug); err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.Description) == "" {
		return nil, errors.New("name and description are required")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := nowUTC().Format(time.RFC3339)
	skillID := newID("skl")
	sourceProvider, sourceURL := normalizedSource(in)
	if _, err := tx.ExecContext(ctx, `INSERT INTO skills (id, owner_type, owner_id, owner_slug, slug, name, description, short_description, recommended_default, pinned_install, source_provider, source_url, created_at, updated_at) VALUES (?, 'user', ?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?)`,
		skillID, user.ID, user.Username, in.Slug, strings.TrimSpace(in.Name), strings.TrimSpace(in.Description), strings.TrimSpace(in.ShortDescription), boolInt(in.RecommendedDefault), boolInt(in.PinnedInstall), sourceProvider, sourceURL, now, now); err != nil {
		return nil, err
	}
	revID, err := insertRevision(ctx, tx, skillID, 1, user.ID, in)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE skills SET current_revision_id = ? WHERE id = ?`, revID, skillID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return getSkill(ctx, db, user.ID, user.Username, in.Slug)
}

func updateSkill(ctx context.Context, db *sql.DB, user *User, owner, slug string, in SkillInput) (*Skill, error) {
	s, err := getSkill(ctx, db, user.ID, owner, slug)
	if err != nil {
		return nil, err
	}
	if s.ArchivedAt != "" {
		return nil, errors.New("archived skills cannot be edited")
	}
	if strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.Description) == "" {
		return nil, errors.New("name and description are required")
	}
	sourceProvider, sourceURL := normalizedSource(in)
	if !in.SourceExplicit && sourceProvider == "" && sourceURL == "" {
		sourceProvider = s.SourceProvider
		sourceURL = s.SourceURL
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var next int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(revision_number), 0) + 1 FROM skill_revisions WHERE skill_id = ?`, s.ID).Scan(&next); err != nil {
		return nil, err
	}
	revID, err := insertRevision(ctx, tx, s.ID, next, user.ID, in)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE skills SET name = ?, description = ?, short_description = ?, recommended_default = ?, pinned_install = ?, source_provider = NULLIF(?, ''), source_url = NULLIF(?, ''), current_revision_id = ?, updated_at = ? WHERE id = ?`,
		strings.TrimSpace(in.Name), strings.TrimSpace(in.Description), strings.TrimSpace(in.ShortDescription), boolInt(in.RecommendedDefault), boolInt(in.PinnedInstall), sourceProvider, sourceURL, revID, nowUTC().Format(time.RFC3339), s.ID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return getSkill(ctx, db, user.ID, owner, slug)
}

func updateSkillSource(ctx context.Context, db *sql.DB, userID, owner, slug string, in SkillInput) (*Skill, error) {
	s, err := getSkill(ctx, db, userID, owner, slug)
	if err != nil {
		return nil, err
	}
	sourceProvider, sourceURL := normalizedSource(in)
	if sourceProvider == s.SourceProvider && sourceURL == s.SourceURL {
		return s, nil
	}
	_, err = db.ExecContext(ctx, `UPDATE skills SET source_provider = NULLIF(?, ''), source_url = NULLIF(?, ''), updated_at = ? WHERE id = ?`,
		sourceProvider, sourceURL, nowUTC().Format(time.RFC3339), s.ID)
	if err != nil {
		return nil, err
	}
	return getSkill(ctx, db, userID, owner, slug)
}

func normalizedSource(in SkillInput) (string, string) {
	return strings.TrimSpace(in.SourceProvider), strings.TrimSpace(in.SourceURL)
}

func insertRevision(ctx context.Context, tx *sql.Tx, skillID string, number int, userID string, in SkillInput) (string, error) {
	raw := []byte(renderSkillMarkdown(in))
	frontmatter, _ := json.Marshal(map[string]any{
		"name":              strings.TrimSpace(in.Name),
		"description":       strings.TrimSpace(in.Description),
		"short_description": strings.TrimSpace(in.ShortDescription),
	})
	manifest, _ := json.Marshal(map[string]any{
		"files":             []string{"SKILL.md"},
		"file_count":        1,
		"total_size":        len(raw),
		"supported_targets": []string{"codex"},
	})
	revID := newID("rev")
	now := nowUTC().Format(time.RFC3339)
	if _, err := tx.ExecContext(ctx, `INSERT INTO skill_revisions (id, skill_id, revision_number, frontmatter_json, manifest_json, tree_hash, created_at, created_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		revID, skillID, number, string(frontmatter), string(manifest), sha256String(raw), now, userID); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO skill_revision_files (id, revision_id, path, content, content_type, content_hash, size_bytes, executable, created_at) VALUES (?, ?, 'SKILL.md', ?, 'text/markdown; charset=utf-8', ?, ?, 0, ?)`,
		newID("file"), revID, raw, sha256String(raw), len(raw), now); err != nil {
		return "", err
	}
	return revID, nil
}

func archiveSkill(ctx context.Context, db *sql.DB, userID, owner, slug string) error {
	s, err := getSkill(ctx, db, userID, owner, slug)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `UPDATE skills SET archived_at = ?, updated_at = ? WHERE id = ?`, nowUTC().Format(time.RFC3339), nowUTC().Format(time.RFC3339), s.ID)
	return err
}

func deleteSkill(ctx context.Context, db *sql.DB, userID, owner, slug string) error {
	s, err := getSkill(ctx, db, userID, owner, slug)
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM skill_revision_files WHERE revision_id IN (SELECT id FROM skill_revisions WHERE skill_id = ?)`, s.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM skill_revisions WHERE skill_id = ?`, s.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM skills WHERE id = ? AND owner_id = ?`, s.ID, userID); err != nil {
		return err
	}
	return tx.Commit()
}

func rawSkill(ctx context.Context, db *sql.DB, userID, owner, slug string) ([]byte, *Skill, error) {
	s, err := getSkill(ctx, db, userID, owner, slug)
	if err != nil {
		return nil, nil, err
	}
	var content []byte
	err = db.QueryRowContext(ctx, `SELECT content FROM skill_revision_files WHERE revision_id = ? AND path = 'SKILL.md'`, s.CurrentRevisionID).Scan(&content)
	return content, s, err
}

func listRevisions(ctx context.Context, db *sql.DB, userID, owner, slug string) ([]SkillRevision, error) {
	s, err := getSkill(ctx, db, userID, owner, slug)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT id, skill_id, revision_number, frontmatter_json, manifest_json, tree_hash, created_at, created_by FROM skill_revisions WHERE skill_id = ? ORDER BY revision_number DESC`, s.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var revs []SkillRevision
	for rows.Next() {
		var r SkillRevision
		var created string
		if err := rows.Scan(&r.ID, &r.SkillID, &r.RevisionNumber, &r.FrontmatterJSON, &r.ManifestJSON, &r.TreeHash, &created, &r.CreatedBy); err != nil {
			return nil, err
		}
		r.CreatedAt = parseTime(created)
		revs = append(revs, r)
	}
	return revs, rows.Err()
}

func getRevisionContent(ctx context.Context, db *sql.DB, userID, owner, slug, revID string) ([]byte, SkillRevision, *Skill, error) {
	s, err := getSkill(ctx, db, userID, owner, slug)
	if err != nil {
		return nil, SkillRevision{}, nil, err
	}
	var r SkillRevision
	var created string
	err = db.QueryRowContext(ctx, `SELECT id, skill_id, revision_number, frontmatter_json, manifest_json, tree_hash, created_at, created_by FROM skill_revisions WHERE id = ? AND skill_id = ?`, revID, s.ID).
		Scan(&r.ID, &r.SkillID, &r.RevisionNumber, &r.FrontmatterJSON, &r.ManifestJSON, &r.TreeHash, &created, &r.CreatedBy)
	if err != nil {
		return nil, SkillRevision{}, nil, err
	}
	r.CreatedAt = parseTime(created)
	var content []byte
	err = db.QueryRowContext(ctx, `SELECT content FROM skill_revision_files WHERE revision_id = ? AND path = 'SKILL.md'`, r.ID).Scan(&content)
	return content, r, s, err
}

func previousRevisionContent(ctx context.Context, db *sql.DB, rev SkillRevision) ([]byte, error) {
	var prevID string
	err := db.QueryRowContext(ctx, `SELECT id FROM skill_revisions WHERE skill_id = ? AND revision_number < ? ORDER BY revision_number DESC LIMIT 1`, rev.SkillID, rev.RevisionNumber).Scan(&prevID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	var content []byte
	err = db.QueryRowContext(ctx, `SELECT content FROM skill_revision_files WHERE revision_id = ? AND path = 'SKILL.md'`, prevID).Scan(&content)
	return content, err
}

func isNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

func dbPathError(path string, err error) error {
	if strings.Contains(err.Error(), "unable to open database file") {
		return fmt.Errorf("open %s: %w", path, err)
	}
	return err
}
