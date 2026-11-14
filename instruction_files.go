package skillsdex

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var errLocalInstructionConflict = errors.New("local instruction file differs from the last applied version")

func instructionPath(home, agent, codexHome string) (string, error) {
	switch agent {
	case "codex":
		if codexHome != "" {
			if !filepath.IsAbs(codexHome) {
				return "", errors.New("CODEX_HOME must be absolute")
			}
			return filepath.Join(codexHome, "AGENTS.md"), nil
		}
		return filepath.Join(home, ".codex", "AGENTS.md"), nil
	case "claude":
		return filepath.Join(home, ".claude", "CLAUDE.md"), nil
	case "gemini":
		return filepath.Join(home, ".gemini", "GEMINI.md"), nil
	default:
		return "", errors.New("unsupported instruction adapter")
	}
}

// Reject links in every existing component, including Windows junctions exposed
// as symlinks by Go. This is a conservative boundary, not a hostile-writer lock.
func rejectInstructionLinks(path string) error {
	for current := filepath.Clean(path); ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("linked instruction path is not managed: %s", current)
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	return nil
}

func applyInstructionFile(path, baseline string, content []byte) error {
	if len(content) > 65536 {
		return errors.New("instructions exceed 64 KiB")
	}
	if !filepath.IsAbs(path) {
		return errors.New("instruction destination must be absolute")
	}
	if err := rejectInstructionLinks(path); err != nil {
		return err
	}
	previous, err := os.ReadFile(path)
	exists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if exists && bytes.Equal(previous, content) {
		return nil
	}
	if exists && (baseline == "" || sha256String(previous) != baseline) {
		return errLocalInstructionConflict
	}
	if !exists && baseline != "" {
		return errLocalInstructionConflict
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if exists {
		backup, err := os.CreateTemp(filepath.Dir(path), ".skillsdex-backup-*")
		if err != nil {
			return err
		}
		if _, err = backup.Write(previous); err != nil {
			backup.Close()
			return err
		}
		if err = backup.Sync(); err != nil {
			backup.Close()
			return err
		}
		if err = backup.Close(); err != nil {
			return err
		}
	}
	staged, err := os.CreateTemp(filepath.Dir(path), ".skillsdex-staged-*")
	if err != nil {
		return err
	}
	defer os.Remove(staged.Name())
	if _, err = staged.Write(content); err != nil {
		staged.Close()
		return err
	}
	if err = staged.Sync(); err != nil {
		staged.Close()
		return err
	}
	if err = staged.Close(); err != nil {
		return err
	}
	if err = rejectInstructionLinks(path); err != nil {
		return err
	}
	latest, readErr := os.ReadFile(path)
	if exists {
		if readErr != nil || !bytes.Equal(latest, previous) {
			return errLocalInstructionConflict
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return errLocalInstructionConflict
	}
	return os.Rename(staged.Name(), path)
}
