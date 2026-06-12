package skillsdex

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
)

func runSync(args []string) error {
	opts, err := parseSyncArgs(args)
	if err != nil {
		return errors.New("usage: skillsdex sync [--dry-run] [--strict] [--json] [--yes|--no]")
	}
	confirm := opts.Confirm
	if opts.JSON && !confirm.DryRun {
		return errors.New("--json requires --dry-run for sync")
	}
	manifest, err := loadManifest()
	if err != nil {
		return err
	}
	client, err := authenticatedClient()
	if err != nil {
		return err
	}
	sort.Slice(manifest.Installs, func(i, j int) bool {
		return manifest.Installs[i].Owner+"/"+manifest.Installs[i].Slug < manifest.Installs[j].Owner+"/"+manifest.Installs[j].Slug
	})
	if opts.JSON {
		return runSyncDryRunJSON(opts, manifest, client)
	}
	changed := false
	pendingChanges := false
	failures := 0
	for _, in := range manifest.Installs {
		id := in.Owner + "/" + in.Slug
		resp, err := client.fetchSkill(in.Owner, in.Slug)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: remote-deleted or unavailable; leaving local file in place\n", id)
			failures++
			continue
		}
		if resp.Skill.ArchivedAt != "" {
			fmt.Fprintf(os.Stderr, "%s: remote archived; leaving local file in place\n", id)
			failures++
			continue
		}
		localModified, localMissing := installHasLocalChanges(in)
		if !localModified && !localMissing && resp.Skill.CurrentRevisionID == in.RemoteRevision {
			fmt.Printf("%s: clean\n", id)
			continue
		}
		files, err := skillPackageFiles(resp)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: invalid package: %v\n", id, err)
			failures++
			continue
		}
		if localModified {
			showPackageDiffs(in, files)
			if confirm.DryRun {
				fmt.Printf("%s: local file modified; would require confirmation before updating to revision %d\n", id, resp.Skill.RevisionNumber)
				printSourceRefreshChanges(syncPackageChanges(in, files), false)
				pendingChanges = true
				continue
			}
			if err := requireConfirmation(fmt.Sprintf("%s was edited locally. Overwrite with remote revision?", in.Path), confirm); err != nil {
				fmt.Fprintf(os.Stderr, "%s: skipped: %v\n", id, err)
				continue
			}
		}
		if confirm.DryRun {
			if localMissing {
				fmt.Printf("%s: would reinstall missing package file(s) at revision %d\n", id, resp.Skill.RevisionNumber)
			} else {
				fmt.Printf("%s: would update package to revision %d\n", id, resp.Skill.RevisionNumber)
			}
			printSourceRefreshChanges(syncPackageChanges(in, files), false)
			pendingChanges = true
			continue
		}
		writeConfirm := confirm
		if localModified {
			writeConfirm.Yes = true
		}
		if _, err := installSkillResponse(&manifest, resp, in.Target, writeConfirm); err != nil {
			return err
		}
		changed = true
		fmt.Printf("%s: updated to revision %d\n", id, resp.Skill.RevisionNumber)
	}
	if changed {
		if err := saveManifest(manifest); err != nil {
			return err
		}
	}
	if confirm.DryRun && pendingChanges {
		fmt.Println("Next steps:")
		fmt.Println("  skillsdex sync --yes")
	}
	if failures > 0 {
		return fmt.Errorf("%d sync issue(s) need attention", failures)
	}
	if opts.Strict && pendingChanges {
		return errors.New("sync updates available")
	}
	return nil
}

func runSyncDryRunJSON(opts syncOptions, manifest Manifest, client *apiClient) error {
	report, err := buildSyncDryRunReport(opts, manifest, client)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		return err
	}
	if report.FailureCount > 0 {
		return fmt.Errorf("%d sync issue(s) need attention", report.FailureCount)
	}
	if opts.Strict && report.PendingCount > 0 {
		return errors.New("sync updates available")
	}
	return nil
}

func buildSyncDryRunReport(opts syncOptions, manifest Manifest, client *apiClient) (syncDryRunReport, error) {
	report := syncDryRunReport{
		DryRun:       true,
		Strict:       opts.Strict,
		InstallCount: len(manifest.Installs),
		StatusCounts: map[string]int{},
	}
	for _, in := range manifest.Installs {
		id := in.Owner + "/" + in.Slug
		result := syncDryRunResult{
			Identifier:     id,
			Owner:          in.Owner,
			Slug:           in.Slug,
			Target:         in.Target,
			Path:           in.Path,
			RemoteRevision: in.RemoteRevision,
		}
		resp, err := client.fetchSkill(in.Owner, in.Slug)
		if err != nil {
			result.Status = "remote-unavailable"
			result.NeedsAttention = true
			result.Error = err.Error()
			report.FailureCount++
			report.StatusCounts[result.Status]++
			report.Results = append(report.Results, result)
			continue
		}
		if resp.Skill.ArchivedAt != "" {
			result.Status = "remote-archived"
			result.NeedsAttention = true
			report.FailureCount++
			report.StatusCounts[result.Status]++
			report.Results = append(report.Results, result)
			continue
		}
		localModified, localMissing := installHasLocalChanges(in)
		result.NextRevision = resp.Skill.RevisionNumber
		if !localModified && !localMissing && resp.Skill.CurrentRevisionID == in.RemoteRevision {
			result.Status = "clean"
			report.CleanCount++
			report.StatusCounts[result.Status]++
			report.Results = append(report.Results, result)
			continue
		}
		files, err := skillPackageFiles(resp)
		if err != nil {
			result.Status = "invalid-package"
			result.NeedsAttention = true
			result.Error = err.Error()
			report.FailureCount++
			report.StatusCounts[result.Status]++
			report.Results = append(report.Results, result)
			continue
		}
		result.Changes = syncPackageChanges(in, files)
		result.NeedsAttention = true
		switch {
		case localModified:
			result.Status = "local-modified"
		case localMissing:
			result.Status = "missing"
		default:
			result.Status = "update-available"
		}
		report.PendingCount++
		report.StatusCounts[result.Status]++
		report.Results = append(report.Results, result)
	}
	if report.PendingCount > 0 {
		report.NextSteps = append(report.NextSteps, "skillsdex sync --yes")
	}
	report.Healthy = report.PendingCount == 0 && report.FailureCount == 0
	return report, nil
}

func syncPackageChanges(in Install, files []SkillFile) []SourceRefreshChange {
	current := map[string]InstalledFile{}
	for _, file := range installedFiles(&in) {
		current[file.Path] = file
	}
	next := map[string]SkillFile{}
	paths := map[string]bool{}
	for _, file := range files {
		next[file.Path] = file
		paths[file.Path] = true
	}
	for path := range current {
		paths[path] = true
	}
	var sorted []string
	for path := range paths {
		sorted = append(sorted, path)
	}
	sort.Strings(sorted)
	var changes []SourceRefreshChange
	for _, path := range sorted {
		oldFile, hadOld := current[path]
		newFile, hasNew := next[path]
		switch {
		case !hadOld && hasNew:
			changes = append(changes, SourceRefreshChange{Path: path, Status: "added"})
		case hadOld && !hasNew:
			changes = append(changes, SourceRefreshChange{Path: path, Status: "removed"})
		case hadOld && hasNew:
			nextHash := sha256String([]byte(newFile.Content))
			if oldFile.Hash != nextHash || oldFile.Executable != newFile.Executable {
				changes = append(changes, SourceRefreshChange{Path: path, Status: "modified"})
			}
		}
	}
	return changes
}
