package skillsdex

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

func runSetup(args []string) error {
	opts, err := parseSetupArgs(args)
	if err != nil {
		return errors.New("usage: skillsdex setup [--target codex] [--set SLUG|--pinned|--defaults] [--apply|--dry-run] [--json] [--yes|--no]")
	}
	if opts.JSON && opts.Apply {
		return errors.New("--json is preview-only for setup; omit --apply")
	}
	opts.Target, err = resolveTarget(opts.Target)
	if err != nil {
		return err
	}
	client, err := authenticatedClient()
	if err != nil {
		return err
	}
	var me struct {
		Username string `json:"username"`
	}
	if err := client.getJSON("/v1/me", &me); err != nil {
		return fmt.Errorf("server check failed: %w", err)
	}

	selection, err := chooseSetupInstall(client, opts)
	if err != nil {
		return err
	}
	confirm := opts.Confirm
	confirm.DryRun = !opts.Apply
	if opts.JSON {
		return printSetupPlanJSON(client.base, me.Username, opts.Target, selection)
	}
	fmt.Printf("Server: %s reachable as %s\n", client.base, me.Username)
	if confirm.DryRun {
		fmt.Println("Mode: preview only; no files will be written.")
	} else {
		fmt.Println("Mode: apply; local managed skill files may be written after confirmation.")
	}
	switch selection.Mode {
	case setupInstallSet:
		fmt.Printf("Selected install set: %s\n", selection.SetSlug)
		if err := runInstallSet(append(setupInstallArgs(opts.Target, confirm), selection.SetSlug)); err != nil {
			return err
		}
	case setupInstallPinned:
		fmt.Println("Selected pinned skills.")
		if err := runInstallPinned(setupInstallArgs(opts.Target, confirm)); err != nil {
			return err
		}
	case setupInstallDefaults:
		fmt.Println("Selected recommended defaults.")
		if err := runInstallDefaults(setupInstallArgs(opts.Target, confirm)); err != nil {
			return err
		}
	default:
		return errors.New("no setup install selection found")
	}
	if confirm.DryRun {
		fmt.Println("Next steps:")
		fmt.Printf("  %s\n", setupApplyCommand(opts.Target, selection))
		fmt.Println("  skillsdex sync --dry-run --strict")
		printSetupQualityCommands()
		printSetupBackupCommands()
		return nil
	}
	fmt.Println("Sync preview:")
	if err := runSync([]string{"--dry-run", "--strict"}); err != nil {
		return err
	}
	printSetupQualityCommands()
	printSetupBackupCommands()
	return nil
}

func printSetupQualityCommands() {
	fmt.Println("Quality gate:")
	for _, command := range setupQualityCommands() {
		fmt.Printf("  %s\n", command)
	}
}

func printSetupBackupCommands() {
	fmt.Println("Backup confidence:")
	for _, command := range setupBackupCommands() {
		fmt.Printf("  %s\n", command)
	}
}

func setupQualityCommands() []string {
	return []string{"skillsdex lint --all --strict"}
}

func setupBackupCommands() []string {
	return []string{
		"skillsdex backup export --out ./skillsdex-backup.zip",
		"skillsdex backup restore --file ./skillsdex-backup.zip --dry-run",
	}
}

func printSetupPlanJSON(serverURL, username, target string, selection setupOptions) error {
	nextSteps := []string{
		setupApplyCommand(target, selection),
		"skillsdex sync --dry-run --strict",
	}
	nextSteps = append(nextSteps, setupQualityCommands()...)
	nextSteps = append(nextSteps, setupBackupCommands()...)
	report := setupPlanReport{
		ServerURL:    serverURL,
		Username:     username,
		Target:       target,
		Mode:         string(selection.Mode),
		SetSlug:      selection.SetSlug,
		Apply:        false,
		PreviewOnly:  true,
		ApplyCommand: setupApplyCommand(target, selection),
		NextSteps:    nextSteps,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

func setupInstallArgs(target string, confirm confirmOptions) []string {
	args := []string{"--target", target}
	if confirm.DryRun {
		args = append(args, "--dry-run")
	}
	if confirm.Yes {
		args = append(args, "--yes")
	}
	if confirm.No {
		args = append(args, "--no")
	}
	return args
}

func setupApplyCommand(target string, selection setupOptions) string {
	args := []string{"skillsdex setup", "--target", target}
	switch selection.Mode {
	case setupInstallSet:
		args = append(args, "--set", selection.SetSlug)
	case setupInstallPinned:
		args = append(args, "--pinned")
	case setupInstallDefaults:
		args = append(args, "--defaults")
	}
	args = append(args, "--apply", "--yes")
	return strings.Join(args, " ")
}

func resolveTarget(target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		cfg, err := loadLocalConfig()
		if err != nil {
			return "", err
		}
		target = cfg.DefaultTarget
	}
	if err := validateTarget(target); err != nil {
		return "", err
	}
	return target, nil
}

func chooseSetupInstall(client *apiClient, opts setupOptions) (setupOptions, error) {
	if opts.Mode != setupInstallAuto {
		return opts, nil
	}
	var sets installSetsResponse
	if err := client.getJSON("/v1/install-sets", &sets); err != nil {
		return setupOptions{}, err
	}
	nonEmptySets := make([]InstallSet, 0, len(sets.Sets))
	for _, set := range sets.Sets {
		if set.SkillCount > 0 {
			nonEmptySets = append(nonEmptySets, set)
		}
	}
	if len(nonEmptySets) == 1 {
		opts.Mode = setupInstallSet
		opts.SetSlug = nonEmptySets[0].Slug
		return opts, nil
	}
	if len(nonEmptySets) > 1 {
		slugs := make([]string, 0, len(nonEmptySets))
		for _, set := range nonEmptySets {
			slugs = append(slugs, set.Slug)
		}
		sort.Strings(slugs)
		return setupOptions{}, fmt.Errorf("multiple install sets found (%s); choose one with --set SLUG", strings.Join(slugs, ", "))
	}

	var list struct {
		Skills []Skill `json:"skills"`
	}
	if err := client.getJSON("/v1/skills", &list); err != nil {
		return setupOptions{}, err
	}
	pinned, defaults := 0, 0
	for _, skill := range list.Skills {
		if skill.ArchivedAt != "" {
			continue
		}
		if skill.PinnedInstall {
			pinned++
		}
		if skill.RecommendedDefault {
			defaults++
		}
	}
	if pinned > 0 {
		opts.Mode = setupInstallPinned
		return opts, nil
	}
	if defaults > 0 {
		opts.Mode = setupInstallDefaults
		return opts, nil
	}
	return setupOptions{}, errors.New("no install set, pinned skill, or recommended default found; create or import a skill first")
}
