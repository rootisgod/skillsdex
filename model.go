package main

import "time"

type User struct {
	ID                 string
	Username           string
	PasswordHash       string
	IsAdmin            bool
	MustChangePassword bool
	DeactivatedAt      *time.Time
	Email              string
	CreatedAt          time.Time
}

type Skill struct {
	ID                 string    `json:"id"`
	OwnerType          string    `json:"owner_type"`
	OwnerID            string    `json:"-"`
	OwnerSlug          string    `json:"owner"`
	Slug               string    `json:"slug"`
	Name               string    `json:"name"`
	Description        string    `json:"description"`
	ShortDescription   string    `json:"short_description"`
	RecommendedDefault bool      `json:"recommended_default"`
	PinnedInstall      bool      `json:"pinned_install"`
	SourceProvider     string    `json:"source_provider,omitempty"`
	SourceURL          string    `json:"source_url,omitempty"`
	SourceAuthor       string    `json:"source_author,omitempty"`
	SourceName         string    `json:"source_name,omitempty"`
	SourcePath         string    `json:"source_path,omitempty"`
	SourceRef          string    `json:"source_ref,omitempty"`
	Tags               []string  `json:"tags,omitempty"`
	CurrentRevisionID  string    `json:"latest_revision"`
	RevisionNumber     int       `json:"revision_number"`
	ArchivedAt         string    `json:"archived_at,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
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

type SkillFile struct {
	Path        string `json:"path"`
	Content     string `json:"content"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
	Executable  bool   `json:"executable"`
}

type SourceRefreshChange struct {
	Path   string `json:"path"`
	Status string `json:"status"`
	Diff   string `json:"diff,omitempty"`
}

type APIToken struct {
	ID           string
	UserID       string
	Name         string
	TokenPreview string
	CreatedAt    time.Time
	LastUsedAt   string
	RevokedAt    string
}

type InstallSet struct {
	ID          string    `json:"id"`
	UserID      string    `json:"-"`
	Slug        string    `json:"slug"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	SkillCount  int       `json:"skill_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type AuditEvent struct {
	ID            string
	ActorUserID   string
	ActorUsername string
	Action        string
	TargetType    string
	TargetID      string
	TargetLabel   string
	DetailJSON    string
	RemoteAddr    string
	UserAgent     string
	CreatedAt     time.Time
}

type RetentionStats struct {
	ExpiredSessions    int64
	RevokedAPITokens   int64
	OldAuditEvents     int64
	PrunedRevisions    int64
	PrunedFiles        int64
	AuditRetentionDays int
	TokenRetentionDays int
	RevisionKeep       int
	DatabaseBytes      int64
	MaxDatabaseBytes   int64
	SkillBytes         int64
	MaxUserStoredBytes int64
	TopUsers           []StorageUserStats
	TopSkills          []StorageSkillStats
}

type StorageUserStats struct {
	UserID        string
	Username      string
	SkillCount    int
	RevisionCount int
	StoredBytes   int64
}

type StorageSkillStats struct {
	ID            string
	OwnerSlug     string
	Slug          string
	RevisionCount int
	StoredBytes   int64
}

type SkillInput struct {
	Owner              string   `json:"owner"`
	Slug               string   `json:"slug"`
	Name               string   `json:"name"`
	Description        string   `json:"description"`
	ShortDescription   string   `json:"short_description"`
	RecommendedDefault bool     `json:"recommended_default"`
	PinnedInstall      bool     `json:"pinned_install"`
	SourceProvider     string   `json:"source_provider,omitempty"`
	SourceURL          string   `json:"source_url,omitempty"`
	SourceAuthor       string   `json:"source_author,omitempty"`
	SourceName         string   `json:"source_name,omitempty"`
	SourcePath         string   `json:"source_path,omitempty"`
	SourceRef          string   `json:"source_ref,omitempty"`
	SourceExplicit     bool     `json:"-"`
	Tags               []string `json:"tags"`
	TagsExplicit       bool     `json:"-"`
	Body               string   `json:"body"`
	Files              []SkillFile
}

type SkillFlagUpdate struct {
	RecommendedDefault *bool `json:"recommended_default,omitempty"`
	PinnedInstall      *bool `json:"pinned_install,omitempty"`
}
