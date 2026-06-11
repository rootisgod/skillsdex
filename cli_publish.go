package skillsdex

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

func runPublish(args []string) error {
	opts, err := parsePublishArgs(args)
	if err != nil {
		return errors.New("usage: skillsdex publish PATH [--slug SLUG] [--apply|--dry-run] [--strict] [--yes|--no]")
	}
	client, err := authenticatedClient()
	if err != nil {
		return err
	}
	var me struct {
		Username string `json:"username"`
	}
	if err := client.getJSON("/v1/me", &me); err != nil {
		return err
	}
	pkg, err := skillInputFromLocalPath(opts.Path, me.Username, opts.Slug)
	if err != nil {
		return err
	}
	in := pkg.Input
	if err := validateSlug("slug", in.Slug); err != nil {
		return err
	}
	report := newLintReport(in.Owner+"/"+in.Slug, skillQuality(in))
	printQualityReport(report)
	if opts.Strict && report.WarningCount > 0 {
		return errors.New("quality warnings found")
	}
	exists, err := client.skillExists(in.Owner, in.Slug)
	if err != nil {
		return err
	}
	action := "create"
	if exists {
		action = "update"
	}
	fileCount := len(in.Files) + 1
	if !opts.Apply {
		fmt.Printf("Would %s %s/%s from %s (%d package file(s)).\n", action, in.Owner, in.Slug, opts.Path, fileCount)
		printLocalPackageFileSummary(in)
		printSkippedLocalPackagePaths(pkg.Skipped)
		fmt.Println("Next steps:")
		fmt.Printf("  skillsdex publish %s --apply --yes\n", shellQuote(opts.Path))
		return nil
	}
	promptAction := "Create"
	doneAction := "Created"
	if exists {
		promptAction = "Update"
		doneAction = "Updated"
	}
	if err := requireConfirmation(fmt.Sprintf("%s %s/%s from %s?", promptAction, in.Owner, in.Slug, opts.Path), opts.Confirm); err != nil {
		return err
	}
	var resp struct {
		Skill Skill `json:"skill"`
	}
	if exists {
		if err := client.putJSON("/v1/skills/"+url.PathEscape(in.Owner)+"/"+url.PathEscape(in.Slug), in, &resp); err != nil {
			return err
		}
	} else {
		if err := client.postJSON("/v1/skills", in, &resp); err != nil {
			return err
		}
	}
	fmt.Printf("%s %s/%s revision %d from %s (%d package file(s)).\n", doneAction, resp.Skill.OwnerSlug, resp.Skill.Slug, resp.Skill.RevisionNumber, opts.Path, fileCount)
	printLocalPackageFileSummary(in)
	printSkippedLocalPackagePaths(pkg.Skipped)
	return nil
}

func skillInputFromLocalPath(inputPath, owner, slugOverride string) (localSkillPackage, error) {
	info, err := os.Stat(inputPath)
	if err != nil {
		return localSkillPackage{}, err
	}
	if info.IsDir() {
		return skillInputFromLocalDir(inputPath, owner, slugOverride)
	}
	raw, err := os.ReadFile(inputPath)
	if err != nil {
		return localSkillPackage{}, err
	}
	if len(raw) > maxImportedSkillBytes {
		return localSkillPackage{}, fmt.Errorf("%s is too large", inputPath)
	}
	fallback := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
	if slugOverride != "" {
		fallback = slugOverride
	}
	in := skillInputFromRawMarkdown(string(raw), owner, fallback, "")
	if slugOverride != "" {
		in.Slug = slugOverride
	}
	in.Owner = owner
	return localSkillPackage{Input: in}, nil
}

func skillInputFromLocalDir(dir, owner, slugOverride string) (localSkillPackage, error) {
	mainPath := filepath.Join(dir, "SKILL.md")
	raw, err := os.ReadFile(mainPath)
	if err != nil {
		return localSkillPackage{}, fmt.Errorf("read SKILL.md: %w", err)
	}
	if len(raw) > maxImportedSkillBytes {
		return localSkillPackage{}, fmt.Errorf("%s is too large", mainPath)
	}
	fallback := filepath.Base(dir)
	if slugOverride != "" {
		fallback = slugOverride
	}
	in := skillInputFromRawMarkdown(string(raw), owner, fallback, "")
	if slugOverride != "" {
		in.Slug = slugOverride
	}
	in.Owner = owner
	var total int64 = int64(len(raw))
	var skipped []string
	err = filepath.WalkDir(dir, func(pathName string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if pathName == dir || pathName == mainPath {
			return nil
		}
		rel, err := filepath.Rel(dir, pathName)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			if shouldSkipLocalSkillPackageDir(entry.Name()) {
				skipped = append(skipped, rel+"/")
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s: symlink package files are not supported", pathName)
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		if shouldSkipLocalSkillPackageFile(rel) {
			skipped = append(skipped, rel)
			return nil
		}
		if err := validateSkillFilePath(rel); err != nil {
			return err
		}
		data, err := os.ReadFile(pathName)
		if err != nil {
			return err
		}
		if len(data) > maxImportedSkillBytes {
			return fmt.Errorf("%s is too large", rel)
		}
		total += int64(len(data))
		if total > maxSkillsDexExportBytes {
			return errors.New("skill package is too large")
		}
		stat, err := entry.Info()
		if err != nil {
			return err
		}
		in.Files = append(in.Files, SkillFile{Path: rel, Content: string(data), ContentType: contentTypeForPath(rel), SizeBytes: int64(len(data)), Executable: stat.Mode()&0111 != 0})
		return nil
	})
	if err != nil {
		return localSkillPackage{}, err
	}
	sort.Slice(in.Files, func(i, j int) bool {
		return in.Files[i].Path < in.Files[j].Path
	})
	sort.Strings(skipped)
	return localSkillPackage{Input: in, Skipped: skipped}, nil
}

func printLocalPackageFileSummary(in SkillInput) {
	fmt.Println("Package files:")
	mainSize := len(renderSkillMarkdown(in))
	fmt.Printf("  SKILL.md (%d bytes)\n", mainSize)
	for _, file := range in.Files {
		size := file.SizeBytes
		if size == 0 && file.Content != "" {
			size = int64(len(file.Content))
		}
		mode := ""
		if file.Executable {
			mode = " executable"
		}
		fmt.Printf("  %s (%d bytes%s)\n", file.Path, size, mode)
	}
}

func printSkillPackageFileSummary(files []SkillFile) {
	if len(files) == 0 {
		return
	}
	fmt.Println("Package files:")
	for _, file := range files {
		size := file.SizeBytes
		if size == 0 && file.Content != "" {
			size = int64(len(file.Content))
		}
		mode := ""
		if file.Executable {
			mode = " executable"
		}
		fmt.Printf("  %s (%d bytes%s)\n", file.Path, size, mode)
	}
}

func printSkippedLocalPackagePaths(paths []string) {
	if len(paths) == 0 {
		return
	}
	fmt.Printf("Skipped %d local package path(s):\n", len(paths))
	for _, skippedPath := range paths {
		fmt.Printf("  %s\n", skippedPath)
	}
}

func shouldSkipLocalSkillPackageDir(name string) bool {
	switch name {
	case ".git", ".hg", ".svn", ".idea", ".vscode", "__pycache__", "node_modules":
		return true
	default:
		return false
	}
}

func shouldSkipLocalSkillPackageFile(rel string) bool {
	base := path.Base(rel)
	if base == ".DS_Store" || base == "Thumbs.db" || base == ".env" || base == ".envrc" {
		return true
	}
	if strings.HasPrefix(base, ".env.") && base != ".env.example" && base != ".env.sample" {
		return true
	}
	if strings.HasSuffix(base, "~") || strings.HasSuffix(base, ".swp") || strings.HasSuffix(base, ".swo") || strings.HasSuffix(base, ".tmp") {
		return true
	}
	return false
}
