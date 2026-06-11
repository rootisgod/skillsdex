package skillsdex

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"
)

func runLint(args []string) error {
	target, filePath, all, strict, jsonOutput, err := parseLintArgs(args)
	if err != nil {
		return errors.New("usage: skillsdex lint [--strict] [--json] (--all | --file PATH | <owner>/<slug>)")
	}
	if all {
		reports, err := lintAllRemoteSkills()
		if err != nil {
			return err
		}
		if jsonOutput {
			if err := printQualityReportsJSON(reports); err != nil {
				return err
			}
		} else {
			printQualityReports(reports)
		}
		if strict {
			return strictLintError(reports)
		}
		return nil
	}

	report, err := lintOneSkill(target, filePath)
	if err != nil {
		return err
	}
	if jsonOutput {
		if err := printQualityReportJSON(report); err != nil {
			return err
		}
	} else {
		printQualityReport(report)
	}
	if strict {
		return strictLintError([]lintReport{report})
	}
	return nil
}

func lintOneSkill(target, filePath string) (lintReport, error) {
	var in SkillInput
	var ctx SkillQualityContext
	label := target
	if filePath != "" {
		pkg, err := skillInputFromLocalPath(filePath, "local", "")
		if err != nil {
			return lintReport{}, err
		}
		in = pkg.Input
		if label == "" {
			label = filePath
		}
	} else {
		owner, slug, err := parseIdentifier(target)
		if err != nil {
			return lintReport{}, err
		}
		client, err := authenticatedClient()
		if err != nil {
			return lintReport{}, err
		}
		resp, err := client.fetchSkill(owner, slug)
		if err != nil {
			return lintReport{}, err
		}
		in = skillInputFromResponse(resp)
		var list struct {
			Skills []Skill `json:"skills"`
		}
		_ = client.getJSON("/v1/skills", &list)
		ctx = qualityContextForSkill(resp.Skill, list.Skills)
	}
	return newLintReport(label, skillQualityWithContext(in, ctx)), nil
}

func lintAllRemoteSkills() ([]lintReport, error) {
	client, err := authenticatedClient()
	if err != nil {
		return nil, err
	}
	var list struct {
		Skills []Skill `json:"skills"`
	}
	if err := client.getJSON("/v1/skills", &list); err != nil {
		return nil, err
	}
	skills := append([]Skill(nil), list.Skills...)
	sort.Slice(skills, func(i, j int) bool {
		return skills[i].OwnerSlug+"/"+skills[i].Slug < skills[j].OwnerSlug+"/"+skills[j].Slug
	})
	reports := make([]lintReport, 0, len(skills))
	for _, skill := range skills {
		if skill.ArchivedAt != "" {
			continue
		}
		resp, err := client.fetchSkill(skill.OwnerSlug, skill.Slug)
		if err != nil {
			return nil, fmt.Errorf("%s/%s: %w", skill.OwnerSlug, skill.Slug, err)
		}
		report := newLintReport(skill.OwnerSlug+"/"+skill.Slug, skillQualityWithContext(skillInputFromResponse(resp), qualityContextForSkill(resp.Skill, list.Skills)))
		reports = append(reports, report)
	}
	return reports, nil
}

func skillInputFromResponse(resp skillResponse) SkillInput {
	return SkillInput{
		Owner:              resp.Skill.OwnerSlug,
		Slug:               resp.Skill.Slug,
		Name:               resp.Skill.Name,
		Description:        resp.Skill.Description,
		ShortDescription:   resp.Skill.ShortDescription,
		RecommendedDefault: resp.Skill.RecommendedDefault,
		PinnedInstall:      resp.Skill.PinnedInstall,
		SourceProvider:     resp.Skill.SourceProvider,
		SourceURL:          resp.Skill.SourceURL,
		SourceAuthor:       resp.Skill.SourceAuthor,
		SourceName:         resp.Skill.SourceName,
		SourcePath:         resp.Skill.SourcePath,
		SourceRef:          resp.Skill.SourceRef,
		Tags:               resp.Skill.Tags,
		TagsExplicit:       true,
		Body:               bodyFromSkillMarkdown(resp.Raw),
		Files:              supportingFilesOnly(resp.Files),
	}
}

func qualityContextForSkill(skill Skill, skills []Skill) SkillQualityContext {
	return SkillQualityContext{
		CurrentIdentifier: skill.OwnerSlug + "/" + skill.Slug,
		SourceProvider:    skill.SourceProvider,
		SourceURL:         skill.SourceURL,
		UpdatedAt:         skill.UpdatedAt,
		Skills:            skills,
	}
}

func newLintReport(label string, issues []SkillQualityIssue) lintReport {
	report := lintReport{Target: label, Summary: qualitySummary(issues), IssueCount: len(issues), Issues: issues}
	for _, issue := range issues {
		if issue.Severity == "warn" {
			report.WarningCount++
		}
	}
	return report
}

func strictLintError(reports []lintReport) error {
	for _, report := range reports {
		if report.WarningCount > 0 {
			return errors.New("quality warnings found")
		}
	}
	return nil
}

func printQualityReportJSON(report lintReport) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

func printQualityReportsJSON(reports []lintReport) error {
	aggregate := aggregateLintReports(reports)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(aggregate)
}

func aggregateLintReports(reports []lintReport) lintReports {
	aggregate := lintReports{Summary: "ready", SkillCount: len(reports), Reports: reports}
	for _, report := range reports {
		aggregate.IssueCount += report.IssueCount
		aggregate.WarningCount += report.WarningCount
		if report.Summary == "needs attention" {
			aggregate.Summary = "needs attention"
		} else if report.Summary == "review" && aggregate.Summary == "ready" {
			aggregate.Summary = "review"
		}
	}
	return aggregate
}

func printQualityReports(reports []lintReport) {
	fmt.Printf("Linted %d active skill(s): %s\n", len(reports), aggregateLintReports(reports).Summary)
	if len(reports) == 0 {
		fmt.Println("No active skills found.")
		return
	}
	for _, report := range reports {
		printQualityReport(report)
	}
}

func printQualityReport(report lintReport) {
	fmt.Printf("%s: %s\n", report.Target, report.Summary)
	if len(report.Issues) == 0 {
		fmt.Println("No obvious quality issues found.")
		return
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SEVERITY\tISSUE\tDETAIL")
	for _, issue := range report.Issues {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", issue.Severity, issue.Title, issue.Detail)
	}
	_ = tw.Flush()
}
