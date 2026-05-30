package main

import "time"

type User struct {
	ID            string
	Username      string
	PasswordHash  string
	IsAdmin       bool
	DeactivatedAt *time.Time
	Email         string
	CreatedAt     time.Time
}

type Skill struct {
	ID                string    `json:"id"`
	OwnerType         string    `json:"owner_type"`
	OwnerID           string    `json:"-"`
	OwnerSlug         string    `json:"owner"`
	Slug              string    `json:"slug"`
	Name              string    `json:"name"`
	Description       string    `json:"description"`
	ShortDescription  string    `json:"short_description"`
	CurrentRevisionID string    `json:"latest_revision"`
	RevisionNumber    int       `json:"revision_number"`
	ArchivedAt        string    `json:"archived_at,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type SkillRevision struct {
	ID              string
	SkillID         string
	RevisionNumber  int
	FrontmatterJSON string
	ManifestJSON    string
	TreeHash        string
	CreatedAt       time.Time
	CreatedBy       string
}

type SkillRevisionFile struct {
	ID          string
	RevisionID  string
	Path        string
	Content     []byte
	ContentType string
	ContentHash string
	SizeBytes   int64
	Executable  bool
	CreatedAt   time.Time
}

type APIToken struct {
	ID         string
	UserID     string
	Name       string
	Token      string
	CreatedAt  time.Time
	LastUsedAt string
	RevokedAt  string
}

type SkillInput struct {
	Owner            string `json:"owner"`
	Slug             string `json:"slug"`
	Name             string `json:"name"`
	Description      string `json:"description"`
	ShortDescription string `json:"short_description"`
	Body             string `json:"body"`
}
