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
	return ensureDefaultAdmin(db)
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
	q := `SELECT s.id, s.owner_type, s.owner_id, s.owner_slug, s.slug, s.name, s.description, COALESCE(s.short_description,''), COALESCE(s.current_revision_id,''), COALESCE(r.revision_number,0), COALESCE(s.archived_at,''), s.created_at, s.updated_at
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
	row := db.QueryRowContext(ctx, `SELECT s.id, s.owner_type, s.owner_id, s.owner_slug, s.slug, s.name, s.description, COALESCE(s.short_description,''), COALESCE(s.current_revision_id,''), COALESCE(r.revision_number,0), COALESCE(s.archived_at,''), s.created_at, s.updated_at
		FROM skills s LEFT JOIN skill_revisions r ON r.id = s.current_revision_id
		WHERE s.owner_id = ? AND s.owner_slug = ? AND s.slug = ?`, userID, owner, slug)
	return scanSkill(row)
}

func scanSkill(row scanner) (*Skill, error) {
	var s Skill
	var created, updated string
	if err := row.Scan(&s.ID, &s.OwnerType, &s.OwnerID, &s.OwnerSlug, &s.Slug, &s.Name, &s.Description, &s.ShortDescription, &s.CurrentRevisionID, &s.RevisionNumber, &s.ArchivedAt, &created, &updated); err != nil {
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
	if _, err := tx.ExecContext(ctx, `INSERT INTO skills (id, owner_type, owner_id, owner_slug, slug, name, description, short_description, created_at, updated_at) VALUES (?, 'user', ?, ?, ?, ?, ?, ?, ?, ?)`,
		skillID, user.ID, user.Username, in.Slug, strings.TrimSpace(in.Name), strings.TrimSpace(in.Description), strings.TrimSpace(in.ShortDescription), now, now); err != nil {
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
	_, err = tx.ExecContext(ctx, `UPDATE skills SET name = ?, description = ?, short_description = ?, current_revision_id = ?, updated_at = ? WHERE id = ?`,
		strings.TrimSpace(in.Name), strings.TrimSpace(in.Description), strings.TrimSpace(in.ShortDescription), revID, nowUTC().Format(time.RFC3339), s.ID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return getSkill(ctx, db, user.ID, owner, slug)
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
