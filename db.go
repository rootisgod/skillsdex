package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
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
			must_change_password INTEGER NOT NULL DEFAULT 0,
			deactivated_at TEXT,
			email TEXT UNIQUE,
			created_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS api_tokens (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id),
			name TEXT NOT NULL,
			token TEXT NOT NULL UNIQUE,
			token_hash TEXT NOT NULL DEFAULT '',
			token_preview TEXT NOT NULL DEFAULT '',
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
			source_author TEXT,
			source_name TEXT,
			source_path TEXT,
			source_ref TEXT,
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
			csrf_token TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			expires_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS install_sets (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id),
			slug TEXT NOT NULL,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(user_id, slug)
		);`,
		`CREATE TABLE IF NOT EXISTS install_set_skills (
			set_id TEXT NOT NULL REFERENCES install_sets(id) ON DELETE CASCADE,
			skill_id TEXT NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
			position INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY(set_id, skill_id)
		);`,
		`CREATE TABLE IF NOT EXISTS tags (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			slug TEXT NOT NULL,
			name TEXT NOT NULL,
			created_at TEXT NOT NULL,
			UNIQUE(user_id, slug)
		);`,
		`CREATE TABLE IF NOT EXISTS skill_tags (
			skill_id TEXT NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
			tag_id TEXT NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
			PRIMARY KEY(skill_id, tag_id)
		);`,
		`CREATE TABLE IF NOT EXISTS audit_events (
			id TEXT PRIMARY KEY,
			actor_user_id TEXT REFERENCES users(id),
			actor_username TEXT NOT NULL DEFAULT '',
			action TEXT NOT NULL,
			target_type TEXT NOT NULL DEFAULT '',
			target_id TEXT NOT NULL DEFAULT '',
			target_label TEXT NOT NULL DEFAULT '',
			detail_json TEXT NOT NULL DEFAULT '{}',
			remote_addr TEXT NOT NULL DEFAULT '',
			user_agent TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_skills_owner ON skills(owner_id, archived_at, updated_at);`,
		`CREATE INDEX IF NOT EXISTS idx_revisions_skill ON skill_revisions(skill_id, revision_number);`,
		`CREATE INDEX IF NOT EXISTS idx_install_sets_user ON install_sets(user_id, updated_at);`,
		`CREATE INDEX IF NOT EXISTS idx_install_set_skills_set ON install_set_skills(set_id, position);`,
		`CREATE INDEX IF NOT EXISTS idx_tags_user_slug ON tags(user_id, slug);`,
		`CREATE INDEX IF NOT EXISTS idx_skill_tags_tag ON skill_tags(tag_id, skill_id);`,
		`CREATE INDEX IF NOT EXISTS idx_audit_events_created ON audit_events(created_at DESC);`,
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
	if _, err := addColumnIfMissing(db, "users", "must_change_password", "must_change_password INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if _, err := addColumnIfMissing(db, "skills", "source_provider", "source_provider TEXT"); err != nil {
		return err
	}
	if _, err := addColumnIfMissing(db, "skills", "source_url", "source_url TEXT"); err != nil {
		return err
	}
	if _, err := addColumnIfMissing(db, "skills", "source_author", "source_author TEXT"); err != nil {
		return err
	}
	if _, err := addColumnIfMissing(db, "skills", "source_name", "source_name TEXT"); err != nil {
		return err
	}
	if _, err := addColumnIfMissing(db, "skills", "source_path", "source_path TEXT"); err != nil {
		return err
	}
	if _, err := addColumnIfMissing(db, "skills", "source_ref", "source_ref TEXT"); err != nil {
		return err
	}
	if _, err := addColumnIfMissing(db, "skills", "pinned_install", "pinned_install INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if _, err := addColumnIfMissing(db, "api_tokens", "token_hash", "token_hash TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if _, err := addColumnIfMissing(db, "api_tokens", "token_preview", "token_preview TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := migrateAPITokenHashes(db); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_api_tokens_hash ON api_tokens(token_hash)`); err != nil {
		return err
	}
	addedCSRF, err := addColumnIfMissing(db, "web_sessions", "csrf_token", "csrf_token TEXT NOT NULL DEFAULT ''")
	if err != nil {
		return err
	}
	if addedCSRF {
		if _, err := db.Exec(`UPDATE web_sessions SET csrf_token = token WHERE csrf_token = ''`); err != nil {
			return err
		}
	}
	if err := ensureDefaultAdmin(db); err != nil {
		return err
	}
	return ensureDefaultSkills(db, addedRecommendedDefault)
}

func migrateAPITokenHashes(db *sql.DB) error {
	rows, err := db.Query(`SELECT id, token FROM api_tokens WHERE token_hash = ''`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type pendingToken struct {
		id    string
		token string
	}
	var pending []pendingToken
	for rows.Next() {
		var p pendingToken
		if err := rows.Scan(&p.id, &p.token); err != nil {
			return err
		}
		pending = append(pending, p)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, p := range pending {
		hash := apiTokenHash(p.token)
		preview := tokenPreview(p.token)
		if _, err := db.Exec(`UPDATE api_tokens SET token = ?, token_hash = ?, token_preview = ? WHERE id = ?`, "stored:"+p.id, hash, preview, p.id); err != nil {
			return err
		}
	}
	return nil
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
	row := db.QueryRowContext(ctx, `SELECT id, username, password_hash, is_admin, must_change_password, COALESCE(deactivated_at,''), COALESCE(email,''), created_at FROM users WHERE username = ?`, username)
	return scanUser(row)
}

func getUserByID(ctx context.Context, db *sql.DB, id string) (*User, error) {
	row := db.QueryRowContext(ctx, `SELECT id, username, password_hash, is_admin, must_change_password, COALESCE(deactivated_at,''), COALESCE(email,''), created_at FROM users WHERE id = ?`, id)
	return scanUser(row)
}

type scanner interface {
	Scan(dest ...any) error
}

func scanUser(row scanner) (*User, error) {
	var u User
	var deactivated, created string
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.IsAdmin, &u.MustChangePassword, &deactivated, &u.Email, &created); err != nil {
		return nil, err
	}
	u.DeactivatedAt = nullableTime(deactivated)
	u.CreatedAt = parseTime(created)
	return &u, nil
}

func listUsers(ctx context.Context, db *sql.DB) ([]User, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, username, password_hash, is_admin, must_change_password, COALESCE(deactivated_at,''), COALESCE(email,''), created_at FROM users ORDER BY username`)
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

func createAuditEvent(ctx context.Context, db *sql.DB, event AuditEvent) error {
	if strings.TrimSpace(event.Action) == "" {
		return errors.New("audit action is required")
	}
	detail := strings.TrimSpace(event.DetailJSON)
	if detail == "" {
		detail = "{}"
	}
	_, err := db.ExecContext(ctx, `INSERT INTO audit_events (id, actor_user_id, actor_username, action, target_type, target_id, target_label, detail_json, remote_addr, user_agent, created_at) VALUES (?, NULLIF(?, ''), ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		newID("aud"), strings.TrimSpace(event.ActorUserID), strings.TrimSpace(event.ActorUsername), strings.TrimSpace(event.Action), strings.TrimSpace(event.TargetType), strings.TrimSpace(event.TargetID), strings.TrimSpace(event.TargetLabel), detail, strings.TrimSpace(event.RemoteAddr), strings.TrimSpace(event.UserAgent), nowUTC().Format(time.RFC3339))
	return err
}

func listAuditEvents(ctx context.Context, db *sql.DB, limit int) ([]AuditEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := db.QueryContext(ctx, `SELECT id, COALESCE(actor_user_id,''), actor_username, action, target_type, target_id, target_label, detail_json, remote_addr, user_agent, created_at FROM audit_events ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []AuditEvent
	for rows.Next() {
		var event AuditEvent
		var created string
		if err := rows.Scan(&event.ID, &event.ActorUserID, &event.ActorUsername, &event.Action, &event.TargetType, &event.TargetID, &event.TargetLabel, &event.DetailJSON, &event.RemoteAddr, &event.UserAgent, &created); err != nil {
			return nil, err
		}
		event.CreatedAt = parseTime(created)
		events = append(events, event)
	}
	return events, rows.Err()
}

func retentionStats(ctx context.Context, db *sql.DB, auditDays, tokenDays int) (RetentionStats, error) {
	stats := RetentionStats{
		AuditRetentionDays: auditDays,
		TokenRetentionDays: tokenDays,
	}
	now := nowUTC().Format(time.RFC3339)
	auditCutoff := nowUTC().AddDate(0, 0, -auditDays).Format(time.RFC3339)
	tokenCutoff := nowUTC().AddDate(0, 0, -tokenDays).Format(time.RFC3339)
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM web_sessions WHERE expires_at <= ?`, now).Scan(&stats.ExpiredSessions); err != nil {
		return RetentionStats{}, err
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM api_tokens WHERE revoked_at IS NOT NULL AND revoked_at <> '' AND revoked_at <= ?`, tokenCutoff).Scan(&stats.RevokedAPITokens); err != nil {
		return RetentionStats{}, err
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_events WHERE created_at <= ?`, auditCutoff).Scan(&stats.OldAuditEvents); err != nil {
		return RetentionStats{}, err
	}
	return stats, nil
}

func pruneRetention(ctx context.Context, db *sql.DB, auditDays, tokenDays int) (RetentionStats, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return RetentionStats{}, err
	}
	defer tx.Rollback()
	now := nowUTC().Format(time.RFC3339)
	auditCutoff := nowUTC().AddDate(0, 0, -auditDays).Format(time.RFC3339)
	tokenCutoff := nowUTC().AddDate(0, 0, -tokenDays).Format(time.RFC3339)
	stats := RetentionStats{
		AuditRetentionDays: auditDays,
		TokenRetentionDays: tokenDays,
	}
	if stats.ExpiredSessions, err = deleteCount(ctx, tx, `DELETE FROM web_sessions WHERE expires_at <= ?`, now); err != nil {
		return RetentionStats{}, err
	}
	if stats.RevokedAPITokens, err = deleteCount(ctx, tx, `DELETE FROM api_tokens WHERE revoked_at IS NOT NULL AND revoked_at <> '' AND revoked_at <= ?`, tokenCutoff); err != nil {
		return RetentionStats{}, err
	}
	if stats.OldAuditEvents, err = deleteCount(ctx, tx, `DELETE FROM audit_events WHERE created_at <= ?`, auditCutoff); err != nil {
		return RetentionStats{}, err
	}
	if err := tx.Commit(); err != nil {
		return RetentionStats{}, err
	}
	return stats, nil
}

func deleteCount(ctx context.Context, tx *sql.Tx, query string, args ...any) (int64, error) {
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return count, nil
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
		_, err = db.ExecContext(ctx, `UPDATE users SET password_hash = ?, must_change_password = 0, is_admin = ?, email = NULLIF(?, ''), deactivated_at = ? WHERE id = ?`,
			string(hash), boolInt(isAdmin), strings.TrimSpace(email), deactivatedAt(deactivated), id)
		return err
	}
	_, err := db.ExecContext(ctx, `UPDATE users SET is_admin = ?, email = NULLIF(?, ''), deactivated_at = ? WHERE id = ?`,
		boolInt(isAdmin), strings.TrimSpace(email), deactivatedAt(deactivated), id)
	return err
}

func setUserMustChangePassword(ctx context.Context, db *sql.DB, userID string, required bool) error {
	_, err := db.ExecContext(ctx, `UPDATE users SET must_change_password = ? WHERE id = ?`, boolInt(required), userID)
	return err
}

func setUserPassword(ctx context.Context, db *sql.DB, userID, password string) error {
	if len(strings.TrimSpace(password)) < 12 {
		return errors.New("password must be at least 12 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `UPDATE users SET password_hash = ?, must_change_password = 0 WHERE id = ?`, string(hash), userID)
	return err
}

func revokeUserAccess(ctx context.Context, db *sql.DB, userID string) error {
	now := nowUTC().Format(time.RFC3339)
	if _, err := db.ExecContext(ctx, `DELETE FROM web_sessions WHERE user_id = ?`, userID); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `UPDATE api_tokens SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL`, now, userID)
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
	csrfToken := newToken()
	now := nowUTC()
	_, err := db.ExecContext(ctx, `INSERT INTO web_sessions (token, user_id, csrf_token, created_at, expires_at) VALUES (?, ?, ?, ?, ?)`,
		token, userID, csrfToken, now.Format(time.RFC3339), now.Add(30*24*time.Hour).Format(time.RFC3339))
	return token, err
}

func userForSession(ctx context.Context, db *sql.DB, token string) (*User, error) {
	row := db.QueryRowContext(ctx, `SELECT u.id, u.username, u.password_hash, u.is_admin, u.must_change_password, COALESCE(u.deactivated_at,''), COALESCE(u.email,''), u.created_at
		FROM web_sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token = ? AND s.expires_at > ? AND u.deactivated_at IS NULL`, token, nowUTC().Format(time.RFC3339))
	return scanUser(row)
}

func deleteWebSession(ctx context.Context, db *sql.DB, token string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM web_sessions WHERE token = ?`, token)
	return err
}

func csrfTokenForSession(ctx context.Context, db *sql.DB, token string) (string, error) {
	var csrfToken string
	err := db.QueryRowContext(ctx, `SELECT csrf_token FROM web_sessions WHERE token = ? AND expires_at > ?`, token, nowUTC().Format(time.RFC3339)).Scan(&csrfToken)
	return csrfToken, err
}

func createAPIToken(ctx context.Context, db *sql.DB, userID, name string) (string, error) {
	token := newToken()
	id := newID("tok")
	_, err := db.ExecContext(ctx, `INSERT INTO api_tokens (id, user_id, name, token, token_hash, token_preview, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, userID, strings.TrimSpace(name), "stored:"+id, apiTokenHash(token), tokenPreview(token), nowUTC().Format(time.RFC3339))
	return token, err
}

func listAPITokens(ctx context.Context, db *sql.DB, userID string) ([]APIToken, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, user_id, name, token_preview, created_at, COALESCE(last_used_at,''), COALESCE(revoked_at,'') FROM api_tokens WHERE user_id = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tokens []APIToken
	for rows.Next() {
		var t APIToken
		var created string
		if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &t.TokenPreview, &created, &t.LastUsedAt, &t.RevokedAt); err != nil {
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
	tokenHash := apiTokenHash(token)
	row := db.QueryRowContext(ctx, `SELECT u.id, u.username, u.password_hash, u.is_admin, u.must_change_password, COALESCE(u.deactivated_at,''), COALESCE(u.email,''), u.created_at
		FROM api_tokens t JOIN users u ON u.id = t.user_id
		WHERE t.token_hash = ? AND t.revoked_at IS NULL AND u.deactivated_at IS NULL`, tokenHash)
	u, err := scanUser(row)
	if err != nil {
		return nil, err
	}
	_, _ = db.ExecContext(ctx, `UPDATE api_tokens SET last_used_at = ? WHERE token_hash = ?`, nowUTC().Format(time.RFC3339), tokenHash)
	return u, nil
}

func createInstallSet(ctx context.Context, db *sql.DB, userID, slug, name, description string) (*InstallSet, error) {
	if err := validateSlug("set slug", slug); err != nil {
		return nil, err
	}
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("name is required")
	}
	now := nowUTC().Format(time.RFC3339)
	id := newID("set")
	_, err := db.ExecContext(ctx, `INSERT INTO install_sets (id, user_id, slug, name, description, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, userID, slug, strings.TrimSpace(name), strings.TrimSpace(description), now, now)
	if err != nil {
		return nil, err
	}
	return getInstallSet(ctx, db, userID, slug)
}

func listInstallSets(ctx context.Context, db *sql.DB, userID string) ([]InstallSet, error) {
	rows, err := db.QueryContext(ctx, `SELECT st.id, st.user_id, st.slug, st.name, st.description, COUNT(s.id), st.created_at, st.updated_at
		FROM install_sets st
		LEFT JOIN install_set_skills ss ON ss.set_id = st.id
		LEFT JOIN skills s ON s.id = ss.skill_id AND s.archived_at IS NULL
		WHERE st.user_id = ?
		GROUP BY st.id
		ORDER BY st.updated_at DESC, st.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sets []InstallSet
	for rows.Next() {
		set, err := scanInstallSet(rows)
		if err != nil {
			return nil, err
		}
		sets = append(sets, *set)
	}
	return sets, rows.Err()
}

func getInstallSet(ctx context.Context, db *sql.DB, userID, slug string) (*InstallSet, error) {
	row := db.QueryRowContext(ctx, `SELECT st.id, st.user_id, st.slug, st.name, st.description, COUNT(s.id), st.created_at, st.updated_at
		FROM install_sets st
		LEFT JOIN install_set_skills ss ON ss.set_id = st.id
		LEFT JOIN skills s ON s.id = ss.skill_id AND s.archived_at IS NULL
		WHERE st.user_id = ? AND st.slug = ?
		GROUP BY st.id`, userID, slug)
	return scanInstallSet(row)
}

func scanInstallSet(row scanner) (*InstallSet, error) {
	var set InstallSet
	var created, updated string
	if err := row.Scan(&set.ID, &set.UserID, &set.Slug, &set.Name, &set.Description, &set.SkillCount, &created, &updated); err != nil {
		return nil, err
	}
	set.CreatedAt = parseTime(created)
	set.UpdatedAt = parseTime(updated)
	return &set, nil
}

func updateInstallSet(ctx context.Context, db *sql.DB, userID, slug, name, description string, skillIDs []string) (*InstallSet, error) {
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("name is required")
	}
	set, err := getInstallSet(ctx, db, userID, slug)
	if err != nil {
		return nil, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE install_sets SET name = ?, description = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		strings.TrimSpace(name), strings.TrimSpace(description), nowUTC().Format(time.RFC3339), set.ID, userID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM install_set_skills WHERE set_id = ?`, set.ID); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	position := 0
	for _, skillID := range skillIDs {
		skillID = strings.TrimSpace(skillID)
		if skillID == "" || seen[skillID] {
			continue
		}
		seen[skillID] = true
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM skills WHERE id = ? AND owner_id = ? AND archived_at IS NULL`, skillID, userID).Scan(&exists); err != nil {
			return nil, err
		}
		if exists == 0 {
			return nil, errors.New("install set includes an unknown or archived skill")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO install_set_skills (set_id, skill_id, position) VALUES (?, ?, ?)`, set.ID, skillID, position); err != nil {
			return nil, err
		}
		position++
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return getInstallSet(ctx, db, userID, slug)
}

func deleteInstallSet(ctx context.Context, db *sql.DB, userID, slug string) error {
	set, err := getInstallSet(ctx, db, userID, slug)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `DELETE FROM install_sets WHERE id = ? AND user_id = ?`, set.ID, userID)
	return err
}

func listInstallSetSkills(ctx context.Context, db *sql.DB, userID, slug string) (*InstallSet, []Skill, error) {
	set, err := getInstallSet(ctx, db, userID, slug)
	if err != nil {
		return nil, nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT s.id, s.owner_type, s.owner_id, s.owner_slug, s.slug, s.name, s.description, COALESCE(s.short_description,''), s.recommended_default, s.pinned_install, COALESCE(s.source_provider,''), COALESCE(s.source_url,''), COALESCE(s.source_author,''), COALESCE(s.source_name,''), COALESCE(s.source_path,''), COALESCE(s.source_ref,''), COALESCE(s.current_revision_id,''), COALESCE(r.revision_number,0), COALESCE(s.archived_at,''), s.created_at, s.updated_at
		FROM install_set_skills ss
		JOIN skills s ON s.id = ss.skill_id
		LEFT JOIN skill_revisions r ON r.id = s.current_revision_id
		WHERE ss.set_id = ? AND s.owner_id = ? AND s.archived_at IS NULL
		ORDER BY ss.position, s.name`, set.ID, userID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var skills []Skill
	for rows.Next() {
		s, err := scanSkill(rows)
		if err != nil {
			return nil, nil, err
		}
		skills = append(skills, *s)
	}
	return set, skills, rows.Err()
}

func listSkills(ctx context.Context, db *sql.DB, userID string, includeArchived bool) ([]Skill, error) {
	return listSkillsFiltered(ctx, db, userID, includeArchived, "")
}

func listSkillsFiltered(ctx context.Context, db *sql.DB, userID string, includeArchived bool, tag string) ([]Skill, error) {
	tag = strings.TrimSpace(tag)
	q := `SELECT s.id, s.owner_type, s.owner_id, s.owner_slug, s.slug, s.name, s.description, COALESCE(s.short_description,''), s.recommended_default, s.pinned_install, COALESCE(s.source_provider,''), COALESCE(s.source_url,''), COALESCE(s.source_author,''), COALESCE(s.source_name,''), COALESCE(s.source_path,''), COALESCE(s.source_ref,''), COALESCE(s.current_revision_id,''), COALESCE(r.revision_number,0), COALESCE(s.archived_at,''), s.created_at, s.updated_at
		FROM skills s LEFT JOIN skill_revisions r ON r.id = s.current_revision_id
		WHERE s.owner_id = ?`
	args := []any{userID}
	if !includeArchived {
		q += ` AND s.archived_at IS NULL`
	}
	if tag != "" {
		if err := validateTagSlug(tag); err != nil {
			return nil, err
		}
		q += ` AND EXISTS (
			SELECT 1 FROM skill_tags st
			JOIN tags t ON t.id = st.tag_id
			WHERE st.skill_id = s.id AND t.user_id = ? AND t.slug = ?
		)`
		args = append(args, userID, tag)
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := hydrateSkillTags(ctx, db, userID, skills); err != nil {
		return nil, err
	}
	return skills, nil
}

func getSkill(ctx context.Context, db *sql.DB, userID, owner, slug string) (*Skill, error) {
	row := db.QueryRowContext(ctx, `SELECT s.id, s.owner_type, s.owner_id, s.owner_slug, s.slug, s.name, s.description, COALESCE(s.short_description,''), s.recommended_default, s.pinned_install, COALESCE(s.source_provider,''), COALESCE(s.source_url,''), COALESCE(s.source_author,''), COALESCE(s.source_name,''), COALESCE(s.source_path,''), COALESCE(s.source_ref,''), COALESCE(s.current_revision_id,''), COALESCE(r.revision_number,0), COALESCE(s.archived_at,''), s.created_at, s.updated_at
		FROM skills s LEFT JOIN skill_revisions r ON r.id = s.current_revision_id
		WHERE s.owner_id = ? AND s.owner_slug = ? AND s.slug = ?`, userID, owner, slug)
	s, err := scanSkill(row)
	if err != nil {
		return nil, err
	}
	s.Tags, err = skillTags(ctx, db, userID, s.ID)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func scanSkill(row scanner) (*Skill, error) {
	var s Skill
	var created, updated string
	if err := row.Scan(&s.ID, &s.OwnerType, &s.OwnerID, &s.OwnerSlug, &s.Slug, &s.Name, &s.Description, &s.ShortDescription, &s.RecommendedDefault, &s.PinnedInstall, &s.SourceProvider, &s.SourceURL, &s.SourceAuthor, &s.SourceName, &s.SourcePath, &s.SourceRef, &s.CurrentRevisionID, &s.RevisionNumber, &s.ArchivedAt, &created, &updated); err != nil {
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
	source := normalizedSource(in)
	if _, err := tx.ExecContext(ctx, `INSERT INTO skills (id, owner_type, owner_id, owner_slug, slug, name, description, short_description, recommended_default, pinned_install, source_provider, source_url, source_author, source_name, source_path, source_ref, created_at, updated_at) VALUES (?, 'user', ?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), ?, ?)`,
		skillID, user.ID, user.Username, in.Slug, strings.TrimSpace(in.Name), strings.TrimSpace(in.Description), strings.TrimSpace(in.ShortDescription), boolInt(in.RecommendedDefault), boolInt(in.PinnedInstall), source.Provider, source.URL, source.Author, source.Name, source.Path, source.Ref, now, now); err != nil {
		return nil, err
	}
	if err := setSkillTagsTx(ctx, tx, user.ID, skillID, in.Tags); err != nil {
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
	source := normalizedSource(in)
	if !in.SourceExplicit && source.empty() {
		source = sourceFromSkill(s)
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
	_, err = tx.ExecContext(ctx, `UPDATE skills SET name = ?, description = ?, short_description = ?, recommended_default = ?, pinned_install = ?, source_provider = NULLIF(?, ''), source_url = NULLIF(?, ''), source_author = NULLIF(?, ''), source_name = NULLIF(?, ''), source_path = NULLIF(?, ''), source_ref = NULLIF(?, ''), current_revision_id = ?, updated_at = ? WHERE id = ?`,
		strings.TrimSpace(in.Name), strings.TrimSpace(in.Description), strings.TrimSpace(in.ShortDescription), boolInt(in.RecommendedDefault), boolInt(in.PinnedInstall), source.Provider, source.URL, source.Author, source.Name, source.Path, source.Ref, revID, nowUTC().Format(time.RFC3339), s.ID)
	if err != nil {
		return nil, err
	}
	if in.TagsExplicit || in.Tags != nil {
		if err := setSkillTagsTx(ctx, tx, user.ID, s.ID, in.Tags); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return getSkill(ctx, db, user.ID, owner, slug)
}

func updateSkillFlags(ctx context.Context, db *sql.DB, user *User, owner, slug string, recommendedDefault, pinnedInstall bool) (*Skill, error) {
	s, err := getSkill(ctx, db, user.ID, owner, slug)
	if err != nil {
		return nil, err
	}
	if s.ArchivedAt != "" {
		return nil, errors.New("archived skills cannot be edited")
	}
	if _, err := db.ExecContext(ctx, `UPDATE skills SET recommended_default = ?, pinned_install = ?, updated_at = ? WHERE id = ?`,
		boolInt(recommendedDefault), boolInt(pinnedInstall), nowUTC().Format(time.RFC3339), s.ID); err != nil {
		return nil, err
	}
	return getSkill(ctx, db, user.ID, owner, slug)
}

func updateSkillSource(ctx context.Context, db *sql.DB, userID, owner, slug string, in SkillInput) (*Skill, error) {
	s, err := getSkill(ctx, db, userID, owner, slug)
	if err != nil {
		return nil, err
	}
	source := normalizedSource(in)
	if source.equal(sourceFromSkill(s)) {
		return s, nil
	}
	_, err = db.ExecContext(ctx, `UPDATE skills SET source_provider = NULLIF(?, ''), source_url = NULLIF(?, ''), source_author = NULLIF(?, ''), source_name = NULLIF(?, ''), source_path = NULLIF(?, ''), source_ref = NULLIF(?, ''), updated_at = ? WHERE id = ?`,
		source.Provider, source.URL, source.Author, source.Name, source.Path, source.Ref, nowUTC().Format(time.RFC3339), s.ID)
	if err != nil {
		return nil, err
	}
	return getSkill(ctx, db, userID, owner, slug)
}

type sourceMetadata struct {
	Provider string
	URL      string
	Author   string
	Name     string
	Path     string
	Ref      string
}

func normalizedSource(in SkillInput) sourceMetadata {
	return sourceMetadata{
		Provider: strings.TrimSpace(in.SourceProvider),
		URL:      strings.TrimSpace(in.SourceURL),
		Author:   strings.TrimSpace(in.SourceAuthor),
		Name:     strings.TrimSpace(in.SourceName),
		Path:     strings.TrimSpace(in.SourcePath),
		Ref:      strings.TrimSpace(in.SourceRef),
	}
}

func sourceFromSkill(s *Skill) sourceMetadata {
	return sourceMetadata{
		Provider: strings.TrimSpace(s.SourceProvider),
		URL:      strings.TrimSpace(s.SourceURL),
		Author:   strings.TrimSpace(s.SourceAuthor),
		Name:     strings.TrimSpace(s.SourceName),
		Path:     strings.TrimSpace(s.SourcePath),
		Ref:      strings.TrimSpace(s.SourceRef),
	}
}

func (m sourceMetadata) empty() bool {
	return m.Provider == "" && m.URL == "" && m.Author == "" && m.Name == "" && m.Path == "" && m.Ref == ""
}

func (m sourceMetadata) equal(other sourceMetadata) bool {
	return m.Provider == other.Provider && m.URL == other.URL && m.Author == other.Author && m.Name == other.Name && m.Path == other.Path && m.Ref == other.Ref
}

func normalizeTags(tags []string) ([]string, error) {
	seen := map[string]bool{}
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag == "" {
			continue
		}
		if err := validateTagSlug(tag); err != nil {
			return nil, err
		}
		if seen[tag] {
			continue
		}
		seen[tag] = true
		out = append(out, tag)
	}
	sort.Strings(out)
	if len(out) > 12 {
		return nil, errors.New("a skill can have at most 12 tags")
	}
	return out, nil
}

func validateTagSlug(tag string) error {
	if err := validateSlug("tag", tag); err != nil {
		return err
	}
	if len(tag) > 32 {
		return errors.New("tag must be 32 characters or fewer")
	}
	return nil
}

func setSkillTagsTx(ctx context.Context, tx *sql.Tx, userID, skillID string, tags []string) error {
	normalized, err := normalizeTags(tags)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM skill_tags WHERE skill_id = ?`, skillID); err != nil {
		return err
	}
	now := nowUTC().Format(time.RFC3339)
	for _, tag := range normalized {
		tagID := ""
		err := tx.QueryRowContext(ctx, `SELECT id FROM tags WHERE user_id = ? AND slug = ?`, userID, tag).Scan(&tagID)
		if err != nil {
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			tagID = newID("tag")
			if _, err := tx.ExecContext(ctx, `INSERT INTO tags (id, user_id, slug, name, created_at) VALUES (?, ?, ?, ?, ?)`, tagID, userID, tag, tag, now); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO skill_tags (skill_id, tag_id) VALUES (?, ?)`, skillID, tagID); err != nil {
			return err
		}
	}
	return nil
}

func updateSkillTags(ctx context.Context, db *sql.DB, userID, skillID string, tags []string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := setSkillTagsTx(ctx, tx, userID, skillID, tags); err != nil {
		return err
	}
	return tx.Commit()
}

func skillTags(ctx context.Context, db *sql.DB, userID, skillID string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT t.slug
		FROM tags t
		JOIN skill_tags st ON st.tag_id = t.id
		WHERE t.user_id = ? AND st.skill_id = ?
		ORDER BY t.slug`, userID, skillID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tags []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}

func hydrateSkillTags(ctx context.Context, db *sql.DB, userID string, skills []Skill) error {
	if len(skills) == 0 {
		return nil
	}
	ids := make([]string, 0, len(skills))
	byID := map[string]int{}
	for i, skill := range skills {
		ids = append(ids, skill.ID)
		byID[skill.ID] = i
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids)+1)
	args = append(args, userID)
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := db.QueryContext(ctx, `SELECT st.skill_id, t.slug
		FROM skill_tags st
		JOIN tags t ON t.id = st.tag_id
		WHERE t.user_id = ? AND st.skill_id IN (`+placeholders+`)
		ORDER BY t.slug`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var skillID, tag string
		if err := rows.Scan(&skillID, &tag); err != nil {
			return err
		}
		if idx, ok := byID[skillID]; ok {
			skills[idx].Tags = append(skills[idx].Tags, tag)
		}
	}
	return rows.Err()
}

func insertRevision(ctx context.Context, tx *sql.Tx, skillID string, number int, userID string, in SkillInput) (string, error) {
	files, totalSize, err := normalizedSkillFiles(in)
	if err != nil {
		return "", err
	}
	frontmatter, _ := json.Marshal(map[string]any{
		"name":              strings.TrimSpace(in.Name),
		"description":       strings.TrimSpace(in.Description),
		"short_description": strings.TrimSpace(in.ShortDescription),
	})
	filePaths := make([]string, 0, len(files))
	for _, file := range files {
		filePaths = append(filePaths, file.Path)
	}
	manifest, _ := json.Marshal(map[string]any{
		"files":             filePaths,
		"file_count":        len(files),
		"total_size":        totalSize,
		"supported_targets": []string{"codex"},
	})
	revID := newID("rev")
	now := nowUTC().Format(time.RFC3339)
	if _, err := tx.ExecContext(ctx, `INSERT INTO skill_revisions (id, skill_id, revision_number, frontmatter_json, manifest_json, tree_hash, created_at, created_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		revID, skillID, number, string(frontmatter), string(manifest), skillTreeHash(files), now, userID); err != nil {
		return "", err
	}
	for _, file := range files {
		if _, err := tx.ExecContext(ctx, `INSERT INTO skill_revision_files (id, revision_id, path, content, content_type, content_hash, size_bytes, executable, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			newID("file"), revID, file.Path, []byte(file.Content), file.ContentType, sha256String([]byte(file.Content)), len([]byte(file.Content)), boolInt(file.Executable), now); err != nil {
			return "", err
		}
	}
	return revID, nil
}

func normalizedSkillFiles(in SkillInput) ([]SkillFile, int, error) {
	main := SkillFile{Path: "SKILL.md", Content: renderSkillMarkdown(in), ContentType: "text/markdown; charset=utf-8"}
	files := []SkillFile{main}
	seen := map[string]bool{"SKILL.md": true}
	total := len([]byte(main.Content))
	for _, file := range in.Files {
		file.Path = strings.TrimSpace(file.Path)
		if file.Path == "" {
			continue
		}
		if err := validateSkillFilePath(file.Path); err != nil {
			return nil, 0, err
		}
		if file.Path == "SKILL.md" {
			return nil, 0, errors.New("supporting files cannot replace SKILL.md")
		}
		if seen[file.Path] {
			return nil, 0, fmt.Errorf("duplicate file path %q", file.Path)
		}
		seen[file.Path] = true
		if file.ContentType == "" {
			file.ContentType = contentTypeForPath(file.Path)
		}
		size := len([]byte(file.Content))
		if size > maxImportedSkillBytes {
			return nil, 0, fmt.Errorf("%s is too large", file.Path)
		}
		total += size
		if total > maxSkillsDexExportBytes {
			return nil, 0, errors.New("skill package is too large")
		}
		files = append(files, file)
	}
	supporting := files[1:]
	sort.Slice(supporting, func(i, j int) bool {
		return supporting[i].Path < supporting[j].Path
	})
	return files, total, nil
}

func contentTypeForPath(filePath string) string {
	switch strings.ToLower(path.Ext(filePath)) {
	case ".md", ".markdown":
		return "text/markdown; charset=utf-8"
	case ".txt", ".json", ".yaml", ".yml", ".toml", ".sh", ".py", ".js", ".ts":
		return "text/plain; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}

func skillTreeHash(files []SkillFile) string {
	var b strings.Builder
	for _, file := range files {
		b.WriteString(file.Path)
		b.WriteByte('\n')
		b.WriteString(sha256String([]byte(file.Content)))
		b.WriteByte('\n')
	}
	return sha256String([]byte(b.String()))
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

func currentSkillFiles(ctx context.Context, db *sql.DB, userID, owner, slug string) ([]SkillFile, *Skill, error) {
	s, err := getSkill(ctx, db, userID, owner, slug)
	if err != nil {
		return nil, nil, err
	}
	files, err := filesForRevision(ctx, db, s.CurrentRevisionID)
	if err != nil {
		return nil, nil, err
	}
	return files, s, nil
}

func filesForRevision(ctx context.Context, db *sql.DB, revisionID string) ([]SkillFile, error) {
	rows, err := db.QueryContext(ctx, `SELECT path, content, content_type, size_bytes, executable FROM skill_revision_files WHERE revision_id = ? ORDER BY CASE WHEN path = 'SKILL.md' THEN 0 ELSE 1 END, path`, revisionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var files []SkillFile
	for rows.Next() {
		var file SkillFile
		var content []byte
		if err := rows.Scan(&file.Path, &content, &file.ContentType, &file.SizeBytes, &file.Executable); err != nil {
			return nil, err
		}
		file.Content = string(content)
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return files, nil
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

func getRevisionFiles(ctx context.Context, db *sql.DB, userID, owner, slug, revID string) ([]SkillFile, SkillRevision, *Skill, error) {
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
	files, err := filesForRevision(ctx, db, r.ID)
	return files, r, s, err
}

func previousRevisionFiles(ctx context.Context, db *sql.DB, rev SkillRevision) ([]SkillFile, error) {
	var prevID string
	err := db.QueryRowContext(ctx, `SELECT id FROM skill_revisions WHERE skill_id = ? AND revision_number < ? ORDER BY revision_number DESC LIMIT 1`, rev.SkillID, rev.RevisionNumber).Scan(&prevID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return filesForRevision(ctx, db, prevID)
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
