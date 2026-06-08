package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type AuthConfig struct {
	ServerURL string `json:"server_url"`
	Token     string `json:"token"`
}

type LocalConfig struct {
	DefaultTarget string `json:"default_target"`
}

type Manifest struct {
	Version  int       `json:"version"`
	Installs []Install `json:"installs"`
}

type Install struct {
	Owner          string          `json:"owner"`
	Slug           string          `json:"slug"`
	Target         string          `json:"target"`
	Path           string          `json:"path"`
	RemoteRevision string          `json:"remote_revision"`
	InstalledHash  string          `json:"installed_hash"`
	Files          []InstalledFile `json:"files,omitempty"`
	InstalledAt    string          `json:"installed_at"`
	Managed        bool            `json:"managed"`
}

type InstalledFile struct {
	Path       string `json:"path"`
	Hash       string `json:"hash"`
	Executable bool   `json:"executable,omitempty"`
}

func skillsdexDir() (string, error) {
	if v := os.Getenv("SKILLSDEX_HOME"); v != "" {
		return v, nil
	}
	if runtime.GOOS == "windows" {
		base, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(base, "skillsdex"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".skillsdex"), nil
}

func authPath() (string, error) {
	dir, err := skillsdexDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "auth.json"), nil
}

func manifestPath() (string, error) {
	dir, err := skillsdexDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "manifest.json"), nil
}

func configPath() (string, error) {
	dir, err := skillsdexDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

func loadAuth() (AuthConfig, error) {
	path, err := authPath()
	if err != nil {
		return AuthConfig{}, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return AuthConfig{}, err
	}
	var cfg AuthConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		return AuthConfig{}, err
	}
	if cfg.ServerURL == "" || cfg.Token == "" {
		return AuthConfig{}, errors.New("auth is incomplete; run skillsdex login")
	}
	cfg.ServerURL = strings.TrimRight(cfg.ServerURL, "/")
	return cfg, nil
}

func saveAuth(cfg AuthConfig) error {
	dir, err := skillsdexDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	path, err := authPath()
	if err != nil {
		return err
	}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile(path, b, 0600)
}

func loadLocalConfig() (LocalConfig, error) {
	path, err := configPath()
	if err != nil {
		return LocalConfig{}, err
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return LocalConfig{DefaultTarget: "codex"}, nil
	}
	if err != nil {
		return LocalConfig{}, err
	}
	var cfg LocalConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		return LocalConfig{}, err
	}
	cfg.DefaultTarget = strings.TrimSpace(cfg.DefaultTarget)
	if cfg.DefaultTarget == "" {
		cfg.DefaultTarget = "codex"
	}
	return cfg, nil
}

func saveLocalConfig(cfg LocalConfig) error {
	dir, err := skillsdexDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	path, err := configPath()
	if err != nil {
		return err
	}
	cfg.DefaultTarget = strings.TrimSpace(cfg.DefaultTarget)
	if cfg.DefaultTarget == "" {
		cfg.DefaultTarget = "codex"
	}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile(path, b, 0600)
}

func loadManifest() (Manifest, error) {
	path, err := manifestPath()
	if err != nil {
		return Manifest{}, err
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Manifest{Version: 1}, nil
	}
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return Manifest{}, err
	}
	if m.Version == 0 {
		m.Version = 1
	}
	return m, nil
}

func saveManifest(m Manifest) error {
	dir, err := skillsdexDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	path, err := manifestPath()
	if err != nil {
		return err
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	return os.WriteFile(path, b, 0600)
}

func (m Manifest) find(owner, slug, target string) (int, *Install) {
	for i := range m.Installs {
		in := &m.Installs[i]
		if in.Owner == owner && in.Slug == slug && in.Target == target {
			return i, in
		}
	}
	return -1, nil
}

func (m *Manifest) upsert(in Install) {
	idx, _ := m.find(in.Owner, in.Slug, in.Target)
	if idx >= 0 {
		m.Installs[idx] = in
		return
	}
	m.Installs = append(m.Installs, in)
}

func (m *Manifest) remove(owner, slug, target string) {
	idx, _ := m.find(owner, slug, target)
	if idx < 0 {
		return
	}
	m.Installs = append(m.Installs[:idx], m.Installs[idx+1:]...)
}

func codexSkillsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex", "skills"), nil
}

func codexSkillPath(owner, slug string) (string, error) {
	dir, err := codexSkillsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, owner+"--"+slug, "SKILL.md"), nil
}

func targetPath(target, owner, slug string) (string, error) {
	if err := validateTarget(target); err != nil {
		return "", err
	}
	return codexSkillPath(owner, slug)
}

func validateTarget(target string) error {
	if target != "codex" {
		return fmt.Errorf("unknown target %q", target)
	}
	return nil
}

func fileHash(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return sha256String(b), nil
}

func installStatus(in Install, remote *Skill, remoteErr error) string {
	modified, missing := installHasLocalChanges(in)
	if missing {
		return "missing"
	}
	if modified {
		return "modified"
	}
	if remoteErr != nil {
		return "remote-deleted"
	}
	if remote != nil && remote.ArchivedAt != "" {
		return "remote-archived"
	}
	if remote != nil && remote.CurrentRevisionID != in.RemoteRevision {
		return "update-available"
	}
	return "clean"
}

func newInstall(owner, slug, target, path, rev string, content []byte) Install {
	return newInstallPackage(owner, slug, target, path, rev, []SkillFile{{Path: "SKILL.md", Content: string(content)}})
}

func newInstallPackage(owner, slug, target, path, rev string, files []SkillFile) Install {
	installedFiles := make([]InstalledFile, 0, len(files))
	mainHash := ""
	for _, file := range files {
		hash := sha256String([]byte(file.Content))
		if file.Path == "SKILL.md" {
			mainHash = hash
		}
		installedFiles = append(installedFiles, InstalledFile{Path: file.Path, Hash: hash, Executable: file.Executable})
	}
	return Install{
		Owner:          owner,
		Slug:           slug,
		Target:         target,
		Path:           path,
		RemoteRevision: rev,
		InstalledHash:  mainHash,
		Files:          installedFiles,
		InstalledAt:    nowUTC().Format(time.RFC3339),
		Managed:        true,
	}
}

func installHasLocalChanges(in Install) (modified bool, missing bool) {
	files := in.Files
	if len(files) == 0 {
		files = []InstalledFile{{Path: "SKILL.md", Hash: in.InstalledHash}}
	}
	baseDir := filepath.Dir(in.Path)
	for _, file := range files {
		dst := in.Path
		if file.Path != "SKILL.md" {
			if err := validateSkillFilePath(file.Path); err != nil {
				return true, false
			}
			dst = filepath.Join(baseDir, filepath.FromSlash(file.Path))
		}
		hash, err := fileHash(dst)
		if errors.Is(err, os.ErrNotExist) {
			return false, true
		}
		if err != nil {
			return false, true
		}
		if hash != file.Hash {
			return true, false
		}
	}
	return false, false
}
