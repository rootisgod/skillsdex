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

type Manifest struct {
	Version  int       `json:"version"`
	Installs []Install `json:"installs"`
}

type Install struct {
	Owner          string `json:"owner"`
	Slug           string `json:"slug"`
	Target         string `json:"target"`
	Path           string `json:"path"`
	RemoteRevision string `json:"remote_revision"`
	InstalledHash  string `json:"installed_hash"`
	InstalledAt    string `json:"installed_at"`
	Managed        bool   `json:"managed"`
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
	if target != "codex" {
		return "", fmt.Errorf("unknown target %q", target)
	}
	return codexSkillPath(owner, slug)
}

func fileHash(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return sha256String(b), nil
}

func installStatus(in Install, remote *Skill, remoteErr error) string {
	hash, err := fileHash(in.Path)
	if errors.Is(err, os.ErrNotExist) {
		return "missing"
	}
	if err != nil {
		return "missing"
	}
	if hash != in.InstalledHash {
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
	return Install{
		Owner:          owner,
		Slug:           slug,
		Target:         target,
		Path:           path,
		RemoteRevision: rev,
		InstalledHash:  sha256String(content),
		InstalledAt:    nowUTC().Format(time.RFC3339),
		Managed:        true,
	}
}
