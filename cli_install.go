package skillsdex

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func runInstall(args []string) error {
	identifier, target, confirm, err := parseTargetArgs(args, true)
	if err != nil {
		return errors.New("usage: skillsdex install <owner>/<slug> [--target codex] [--dry-run] [--yes|--no]")
	}
	target, err = resolveTarget(target)
	if err != nil {
		return err
	}
	owner, slug, err := parseIdentifier(identifier)
	if err != nil {
		return err
	}
	if err := validateSlug("owner", owner); err != nil {
		return err
	}
	if err := validateSlug("slug", slug); err != nil {
		return err
	}
	client, err := authenticatedClient()
	if err != nil {
		return err
	}
	resp, err := client.fetchSkill(owner, slug)
	if err != nil {
		return err
	}
	if resp.Skill.ArchivedAt != "" {
		return errors.New("remote skill is archived")
	}
	manifest, err := loadManifest()
	if err != nil {
		return err
	}
	if _, err := installSkillResponse(&manifest, resp, target, confirm); err != nil {
		return err
	}
	if confirm.DryRun {
		return nil
	}
	if err := saveManifest(manifest); err != nil {
		return err
	}
	return nil
}

func runInstallDefaults(args []string) error {
	target, confirm, err := parseInstallDefaultsArgs(args)
	if err != nil {
		return errors.New("usage: skillsdex install-defaults [--target codex] [--dry-run] [--yes|--no]")
	}
	target, err = resolveTarget(target)
	if err != nil {
		return err
	}
	return installMatchingSkills(target, confirm, "recommended default", func(s Skill) bool {
		return s.RecommendedDefault && s.ArchivedAt == ""
	})
}

func runInstallPinned(args []string) error {
	target, confirm, err := parseInstallDefaultsArgs(args)
	if err != nil {
		return errors.New("usage: skillsdex install-pinned [--target codex] [--dry-run] [--yes|--no]")
	}
	target, err = resolveTarget(target)
	if err != nil {
		return err
	}
	return installMatchingSkills(target, confirm, "pinned", func(s Skill) bool {
		return s.PinnedInstall && s.ArchivedAt == ""
	})
}

func runInstallSet(args []string) error {
	setSlug, target, confirm, err := parseTargetArgs(args, true)
	if err != nil {
		return errors.New("usage: skillsdex install-set <set-slug> [--target codex] [--dry-run] [--yes|--no]")
	}
	target, err = resolveTarget(target)
	if err != nil {
		return err
	}
	if err := validateSlug("set slug", setSlug); err != nil {
		return err
	}
	client, err := authenticatedClient()
	if err != nil {
		return err
	}
	set, err := client.fetchInstallSet(setSlug)
	if err != nil {
		return err
	}
	if len(set.Skills) == 0 {
		fmt.Printf("Install set %s has no active skills.\n", set.Set.Slug)
		return nil
	}
	manifest, err := loadManifest()
	if err != nil {
		return err
	}
	installed := 0
	for _, skill := range set.Skills {
		resp, err := client.fetchSkill(skill.OwnerSlug, skill.Slug)
		if err != nil {
			return err
		}
		if _, err := installSkillResponse(&manifest, resp, target, confirm); err != nil {
			return err
		}
		installed++
	}
	if confirm.DryRun {
		fmt.Printf("Would install %d skill(s) from set %s.\n", installed, set.Set.Slug)
		return nil
	}
	if err := saveManifest(manifest); err != nil {
		return err
	}
	fmt.Printf("Installed %d skill(s) from set %s.\n", installed, set.Set.Slug)
	return nil
}

func installMatchingSkills(target string, confirm confirmOptions, label string, include func(Skill) bool) error {
	client, err := authenticatedClient()
	if err != nil {
		return err
	}
	var list struct {
		Skills []Skill `json:"skills"`
	}
	if err := client.getJSON("/v1/skills", &list); err != nil {
		return err
	}
	var selected []Skill
	for _, s := range list.Skills {
		if include(s) {
			selected = append(selected, s)
		}
	}
	sort.Slice(selected, func(i, j int) bool {
		return selected[i].OwnerSlug+"/"+selected[i].Slug < selected[j].OwnerSlug+"/"+selected[j].Slug
	})
	if len(selected) == 0 {
		fmt.Printf("No %s skills found.\n", label)
		return nil
	}
	manifest, err := loadManifest()
	if err != nil {
		return err
	}
	installed := 0
	for _, s := range selected {
		resp, err := client.fetchSkill(s.OwnerSlug, s.Slug)
		if err != nil {
			return err
		}
		if _, err := installSkillResponse(&manifest, resp, target, confirm); err != nil {
			return err
		}
		installed++
	}
	if confirm.DryRun {
		fmt.Printf("Would install %d %s skill(s).\n", installed, label)
		return nil
	}
	if err := saveManifest(manifest); err != nil {
		return err
	}
	fmt.Printf("Installed %d %s skill(s).\n", installed, label)
	return nil
}

func installSkillResponse(manifest *Manifest, resp skillResponse, target string, confirm confirmOptions) (string, error) {
	if resp.Skill.ArchivedAt != "" {
		return "", errors.New("remote skill is archived")
	}
	owner, slug := resp.Skill.OwnerSlug, resp.Skill.Slug
	skillPath, err := targetPath(target, owner, slug)
	if err != nil {
		return "", err
	}
	_, existing := manifest.find(owner, slug, target)
	files, err := skillPackageFiles(resp)
	if err != nil {
		return "", err
	}
	baseDir := filepath.Dir(skillPath)
	for _, file := range files {
		dst := packageFileDestination(skillPath, file.Path)
		installedHash := installedHashForFile(existing, file.Path)
		if err := confirmExecutableFile(file, installedHash, confirm); err != nil {
			return "", err
		}
		if err := confirmPackageFileWrite(dst, installedHash, []byte(file.Content), confirm); err != nil {
			return "", err
		}
	}
	if existing != nil {
		nextPaths := map[string]bool{}
		for _, file := range files {
			nextPaths[file.Path] = true
		}
		for _, file := range installedFiles(existing) {
			if nextPaths[file.Path] {
				continue
			}
			dst := packageFileDestination(skillPath, file.Path)
			if err := confirmManagedFileRemoval(dst, file.Hash, confirm); err != nil {
				return "", err
			}
		}
	}
	if confirm.DryRun {
		fmt.Printf("Would install %s/%s to %s\n", owner, slug, skillPath)
		printSkillPackageFileSummary(files)
		return skillPath, nil
	}
	if err := os.MkdirAll(baseDir, 0700); err != nil {
		return "", err
	}
	if existing != nil {
		nextPaths := map[string]bool{}
		for _, file := range files {
			nextPaths[file.Path] = true
		}
		for _, file := range installedFiles(existing) {
			if nextPaths[file.Path] {
				continue
			}
			dst := packageFileDestination(skillPath, file.Path)
			if err := os.Remove(dst); err != nil && !errors.Is(err, os.ErrNotExist) {
				return "", err
			}
			removeEmptyParents(filepath.Dir(dst), baseDir)
		}
	}
	for _, file := range files {
		dst := packageFileDestination(skillPath, file.Path)
		if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
			return "", err
		}
		if err := os.WriteFile(dst, []byte(file.Content), fileMode(file)); err != nil {
			return "", err
		}
	}
	manifest.upsert(newInstallPackage(owner, slug, target, skillPath, resp.Skill.CurrentRevisionID, files))
	fmt.Printf("Installed %s/%s to %s\n", owner, slug, skillPath)
	return skillPath, nil
}

func skillPackageFiles(resp skillResponse) ([]SkillFile, error) {
	files := resp.Files
	if len(files) == 0 {
		files = []SkillFile{{Path: "SKILL.md", Content: resp.Raw, ContentType: "text/markdown; charset=utf-8"}}
	}
	out := make([]SkillFile, 0, len(files)+1)
	seen := map[string]bool{}
	hasMain := false
	for _, file := range files {
		file.Path = strings.TrimSpace(file.Path)
		if file.Path == "" {
			continue
		}
		if err := validateSkillFilePath(file.Path); err != nil {
			return nil, err
		}
		if seen[file.Path] {
			return nil, fmt.Errorf("duplicate package file %q", file.Path)
		}
		seen[file.Path] = true
		if file.Path == "SKILL.md" {
			hasMain = true
			if file.Content == "" && resp.Raw != "" {
				file.Content = resp.Raw
			}
		}
		out = append(out, file)
	}
	if !hasMain {
		out = append(out, SkillFile{Path: "SKILL.md", Content: resp.Raw, ContentType: "text/markdown; charset=utf-8"})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path == "SKILL.md" {
			return true
		}
		if out[j].Path == "SKILL.md" {
			return false
		}
		return out[i].Path < out[j].Path
	})
	return out, nil
}

func fileMode(file SkillFile) os.FileMode {
	if file.Executable {
		return 0700
	}
	return 0600
}
