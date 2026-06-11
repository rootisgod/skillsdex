package skillsdex

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func packageFileDestination(skillPath, filePath string) string {
	if filePath == "SKILL.md" {
		return skillPath
	}
	return filepath.Join(filepath.Dir(skillPath), filepath.FromSlash(filePath))
}

func installedFileDestination(in Install, filePath string) (string, error) {
	if filePath == "SKILL.md" {
		return in.Path, nil
	}
	if err := validateSkillFilePath(filePath); err != nil {
		return "", err
	}
	return packageFileDestination(in.Path, filePath), nil
}

func installedFiles(in *Install) []InstalledFile {
	if in == nil {
		return nil
	}
	if len(in.Files) > 0 {
		return in.Files
	}
	return []InstalledFile{{Path: "SKILL.md", Hash: in.InstalledHash}}
}

func installedHashForFile(in *Install, filePath string) string {
	for _, file := range installedFiles(in) {
		if file.Path == filePath {
			return file.Hash
		}
	}
	return ""
}

func installHasModifiedManagedFiles(in Install) (bool, error) {
	for _, file := range installedFiles(&in) {
		dst, err := installedFileDestination(in, file.Path)
		if err != nil {
			return false, err
		}
		current, err := os.ReadFile(dst)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return false, err
		}
		if sha256String(current) != file.Hash {
			return true, nil
		}
	}
	return false, nil
}

func confirmPackageFileWrite(path, installedHash string, next []byte, confirm confirmOptions) error {
	current, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if bytes.Equal(current, next) {
		return nil
	}
	currentHash := sha256String(current)
	if installedHash != "" && currentHash == installedHash {
		return nil
	}
	showOverwriteDiff(path, current, next)
	if confirm.DryRun {
		return nil
	}
	if installedHash == "" {
		return requireConfirmation(fmt.Sprintf("%s exists but is not managed by SkillsDex. Overwrite?", path), confirm)
	}
	return requireConfirmation(fmt.Sprintf("%s was edited locally. Overwrite?", path), confirm)
}

func confirmExecutableFile(file SkillFile, installedHash string, confirm confirmOptions) error {
	if confirm.DryRun || !file.Executable || file.Path == "SKILL.md" || installedHash != "" {
		return nil
	}
	return requireConfirmation(fmt.Sprintf("%s is an executable supporting file. Install it?", file.Path), confirm)
}

func confirmManagedFileRemoval(path, installedHash string, confirm confirmOptions) error {
	current, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if installedHash != "" && sha256String(current) == installedHash {
		return nil
	}
	showOverwriteDiff(path, current, nil)
	if confirm.DryRun {
		return nil
	}
	return requireConfirmation(fmt.Sprintf("%s was edited locally. Remove stale managed file?", path), confirm)
}

func showPackageDiffs(in Install, files []SkillFile) {
	baseDir := filepath.Dir(in.Path)
	next := map[string]SkillFile{}
	for _, file := range files {
		next[file.Path] = file
	}
	for _, file := range installedFiles(&in) {
		dst := in.Path
		if file.Path != "SKILL.md" {
			if err := validateSkillFilePath(file.Path); err != nil {
				continue
			}
			dst = filepath.Join(baseDir, filepath.FromSlash(file.Path))
		}
		current, err := os.ReadFile(dst)
		if err != nil || sha256String(current) == file.Hash {
			continue
		}
		if replacement, ok := next[file.Path]; ok {
			showOverwriteDiff(dst, current, []byte(replacement.Content))
		} else {
			showOverwriteDiff(dst, current, nil)
		}
	}
}

func removeEmptyParents(dir, stop string) {
	stopAbs, err := filepath.Abs(stop)
	if err != nil {
		return
	}
	for {
		dirAbs, err := filepath.Abs(dir)
		if err != nil || dirAbs == stopAbs {
			return
		}
		rel, err := filepath.Rel(stopAbs, dirAbs)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			return
		}
		if err := os.Remove(dirAbs); err != nil {
			return
		}
		dir = filepath.Dir(dirAbs)
	}
}

func showOverwriteDiff(path string, current, next []byte) {
	diff := strings.TrimSpace(unifiedDiff(string(current), string(next)))
	if diff == "" {
		return
	}
	fmt.Fprintf(os.Stderr, "\nDiff for %s:\n%s\n\n", path, diff)
}
