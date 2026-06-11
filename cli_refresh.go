package skillsdex

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

func runRefresh(args []string) error {
	target, all, apply, strict, showDiff, jsonOutput, confirm, err := parseRefreshArgs(args)
	if err != nil {
		return errors.New("usage: skillsdex refresh (--all | <owner>/<slug>) [--apply|--dry-run] [--strict] [--diff] [--json] [--yes|--no]")
	}
	client, err := authenticatedClient()
	if err != nil {
		return err
	}
	dryRun := !apply
	if jsonOutput && !dryRun {
		return errors.New("--json is preview-only for refresh; omit --apply")
	}
	if all {
		skills, err := refreshableSkills(client)
		if err != nil {
			return err
		}
		if jsonOutput {
			return runRefreshAllJSON(client, skills, strict)
		}
		if len(skills) == 0 {
			fmt.Println("No active imported skills with source URLs found.")
			return nil
		}
		if dryRun {
			fmt.Printf("Previewing source refresh for %d imported skill(s). Use --apply --yes to update changed skills.\n", len(skills))
		} else if err := requireConfirmation(fmt.Sprintf("Refresh %d imported skill(s) from upstream?", len(skills)), confirm); err != nil {
			return err
		}
		statusCounts := map[string]int{}
		failures := 0
		for _, skill := range skills {
			resp, err := client.refreshSkill(skill.OwnerSlug, skill.Slug, dryRun)
			if err != nil {
				failures++
				fmt.Fprintf(os.Stderr, "%s/%s: refresh failed: %v\n", skill.OwnerSlug, skill.Slug, err)
				continue
			}
			statusCounts[resp.Status]++
			fmt.Printf("%s/%s: %s\n", resp.Skill.OwnerSlug, resp.Skill.Slug, resp.Status)
			printSourceRefreshChanges(resp.Changes, showDiff)
		}
		printRefreshNextSteps(dryRun, statusCounts, "skillsdex refresh --all --apply --yes")
		if failures > 0 {
			return fmt.Errorf("%d source refresh(es) failed", failures)
		}
		if strict && statusCounts["update available"] > 0 {
			return fmt.Errorf("%d source update(s) available", statusCounts["update available"])
		}
		return nil
	}
	owner, slug, err := parseIdentifier(target)
	if err != nil {
		return err
	}
	if err := validateSlug("owner", owner); err != nil {
		return err
	}
	if err := validateSlug("slug", slug); err != nil {
		return err
	}
	if jsonOutput {
		return runRefreshOneJSON(client, owner, slug, strict)
	}
	if dryRun {
		fmt.Println("Previewing source refresh. Use --apply --yes to update if upstream changed.")
	} else if err := requireConfirmation(fmt.Sprintf("Refresh %s/%s from upstream?", owner, slug), confirm); err != nil {
		return err
	}
	resp, err := client.refreshSkill(owner, slug, dryRun)
	if err != nil {
		return err
	}
	fmt.Printf("%s/%s: %s\n", resp.Skill.OwnerSlug, resp.Skill.Slug, resp.Status)
	printSourceRefreshChanges(resp.Changes, showDiff)
	printRefreshNextSteps(dryRun, map[string]int{resp.Status: 1}, fmt.Sprintf("skillsdex refresh %s/%s --apply --yes", owner, slug))
	if strict && resp.Status == "update available" {
		return errors.New("source update available")
	}
	return nil
}

func runRefreshOneJSON(client *apiClient, owner, slug string, strict bool) error {
	resp, err := client.refreshSkill(owner, slug, true)
	if err != nil {
		return err
	}
	report := newRefreshReport(true, strict, false, owner+"/"+slug)
	addRefreshResponseToReport(&report, resp)
	report.NextSteps = refreshNextSteps(report.StatusCounts, fmt.Sprintf("skillsdex refresh %s/%s --apply --yes", owner, slug))
	report.Healthy = report.UpdateCount == 0 && report.FailureCount == 0
	if err := printRefreshReportJSON(report); err != nil {
		return err
	}
	if strict && report.UpdateCount > 0 {
		return errors.New("source update available")
	}
	return nil
}

func runRefreshAllJSON(client *apiClient, skills []Skill, strict bool) error {
	report := newRefreshReport(true, strict, true, "")
	if len(skills) == 0 {
		report.Healthy = true
		return printRefreshReportJSON(report)
	}
	for _, skill := range skills {
		resp, err := client.refreshSkill(skill.OwnerSlug, skill.Slug, true)
		if err != nil {
			report.FailureCount++
			report.Results = append(report.Results, refreshResult{
				Identifier: skill.OwnerSlug + "/" + skill.Slug,
				Status:     "refresh failed",
				Error:      err.Error(),
			})
			report.StatusCounts["refresh failed"]++
			continue
		}
		addRefreshResponseToReport(&report, resp)
	}
	report.NextSteps = refreshNextSteps(report.StatusCounts, "skillsdex refresh --all --apply --yes")
	report.Healthy = report.UpdateCount == 0 && report.FailureCount == 0
	if err := printRefreshReportJSON(report); err != nil {
		return err
	}
	if report.FailureCount > 0 {
		return fmt.Errorf("%d source refresh(es) failed", report.FailureCount)
	}
	if strict && report.UpdateCount > 0 {
		return fmt.Errorf("%d source update(s) available", report.UpdateCount)
	}
	return nil
}

func newRefreshReport(dryRun, strict, all bool, target string) refreshReport {
	return refreshReport{
		DryRun:       dryRun,
		Strict:       strict,
		All:          all,
		Target:       target,
		StatusCounts: map[string]int{},
	}
}

func addRefreshResponseToReport(report *refreshReport, resp refreshResponse) {
	identifier := resp.Skill.OwnerSlug + "/" + resp.Skill.Slug
	report.CheckedCount++
	report.StatusCounts[resp.Status]++
	if resp.Status == "update available" {
		report.UpdateCount++
	}
	skill := resp.Skill
	report.Results = append(report.Results, refreshResult{
		Identifier: identifier,
		Status:     resp.Status,
		Skill:      &skill,
		Changes:    resp.Changes,
	})
}

func printRefreshReportJSON(report refreshReport) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

func printSourceRefreshChanges(changes []SourceRefreshChange, showDiff bool) {
	if len(changes) == 0 {
		return
	}
	for _, change := range changes {
		fmt.Printf("  %s\t%s\n", change.Status, change.Path)
		if showDiff && strings.TrimSpace(change.Diff) != "" {
			fmt.Print(indentBlock(change.Diff, "    "))
			if !strings.HasSuffix(change.Diff, "\n") {
				fmt.Println()
			}
		}
	}
}

func indentBlock(value, prefix string) string {
	if value == "" {
		return ""
	}
	lines := strings.SplitAfter(value, "\n")
	var b strings.Builder
	for _, line := range lines {
		if line == "" {
			continue
		}
		b.WriteString(prefix)
		b.WriteString(line)
	}
	return b.String()
}

func printRefreshNextSteps(dryRun bool, statusCounts map[string]int, applyCommand string) {
	steps := refreshNextStepsForMode(dryRun, statusCounts, applyCommand)
	if len(steps) == 0 {
		return
	}
	fmt.Println("Next steps:")
	for _, step := range steps {
		fmt.Println("  " + step)
	}
}

func refreshNextSteps(statusCounts map[string]int, applyCommand string) []string {
	return refreshNextStepsForMode(true, statusCounts, applyCommand)
}

func refreshNextStepsForMode(dryRun bool, statusCounts map[string]int, applyCommand string) []string {
	if dryRun {
		if statusCounts["update available"] > 0 {
			return []string{applyCommand, "skillsdex sync --dry-run --strict"}
		}
		return nil
	}
	if statusCounts["updated from source"] > 0 {
		return []string{"skillsdex sync --dry-run --strict"}
	}
	return nil
}

func refreshableSkills(client *apiClient) ([]Skill, error) {
	var list struct {
		Skills []Skill `json:"skills"`
	}
	if err := client.getJSON("/v1/skills", &list); err != nil {
		return nil, err
	}
	var skills []Skill
	for _, skill := range list.Skills {
		if skill.ArchivedAt != "" || strings.TrimSpace(skill.SourceURL) == "" {
			continue
		}
		skills = append(skills, skill)
	}
	sort.Slice(skills, func(i, j int) bool {
		return skills[i].OwnerSlug+"/"+skills[i].Slug < skills[j].OwnerSlug+"/"+skills[j].Slug
	})
	return skills, nil
}
