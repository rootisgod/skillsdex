package main

import (
	"strings"
	"testing"
)

func TestSkillQualityFlagsWeakSkill(t *testing.T) {
	issues := skillQuality(SkillInput{
		Name:        "Weak",
		Description: "Do things.",
		Body:        "TODO",
	})
	var titles []string
	for _, issue := range issues {
		titles = append(titles, issue.Title)
	}
	got := strings.Join(titles, "\n")
	for _, want := range []string{
		"Description is too short",
		"Trigger language is unclear",
		"Suggested trigger rewrite",
		"Short description is missing",
		"Instructions are sparse",
		"Top-level heading is missing",
		"TODO text found",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("quality issues missing %q:\n%s", want, got)
		}
	}
	if summary := qualitySummary(issues); summary != "needs attention" {
		t.Fatalf("summary = %q, want needs attention", summary)
	}
}

func TestSkillQualityAcceptsStrongSkill(t *testing.T) {
	issues := skillQuality(SkillInput{
		Name:             "Review",
		Description:      "Use when reviewing a change for bugs, regressions, or missing tests.",
		ShortDescription: "Review changes for concrete risks",
		Body:             "# Review\n\nInspect the goal, affected files, tests, and behavior. Report confirmed findings first, with file references and severity. Keep questions separate from bugs.",
	})
	if len(issues) != 0 {
		t.Fatalf("strong skill got issues: %#v", issues)
	}
	if summary := qualitySummary(issues); summary != "ready" {
		t.Fatalf("summary = %q, want ready", summary)
	}
}

func TestSkillQualityContextFlagsDuplicateAndStaleSource(t *testing.T) {
	issues := skillQualityWithContext(SkillInput{
		Name:             "Review",
		Description:      "Use when reviewing a change for bugs and regressions.",
		ShortDescription: "Review changes for concrete risks",
		Body:             "# Review\n\nInspect the goal, affected files, tests, and behavior. Report confirmed findings first, with file references and severity.",
	}, SkillQualityContext{
		CurrentIdentifier: "admin/review",
		SourceProvider:    "github",
		SourceURL:         "https://github.com/example/skills/tree/main/review",
		UpdatedAt:         nowUTC().AddDate(0, 0, -120),
		Skills: []Skill{
			{OwnerSlug: "admin", Slug: "review", Name: "Review", Description: "Use when reviewing a change for bugs and regressions.", ShortDescription: "Review changes for concrete risks"},
			{OwnerSlug: "admin", Slug: "code-review", Name: "Code Review", Description: "Use when reviewing a change for bugs and regressions.", ShortDescription: "Review changes for concrete risks"},
		},
	})
	var titles []string
	for _, issue := range issues {
		titles = append(titles, issue.Title)
	}
	got := strings.Join(titles, "\n")
	for _, want := range []string{"Possible duplicate skill", "Source may be stale"} {
		if !strings.Contains(got, want) {
			t.Fatalf("quality issues missing %q:\n%s", want, got)
		}
	}
}

func TestSkillQualityFlagsSupportingFileRisks(t *testing.T) {
	issues := skillQuality(SkillInput{
		Name:             "Review",
		Description:      "Use when reviewing a change for bugs, regressions, or missing tests.",
		ShortDescription: "Review changes for concrete risks",
		Body:             "# Review\n\nInspect the goal, affected files, tests, and behavior. Report confirmed findings first, with file references and severity. Keep questions separate from bugs.",
		Files: []SkillFile{
			{Path: "references/private.md", Content: "The password is example-only but should be removed."},
			{Path: "scripts/check.sh", Content: "#!/bin/sh\nexit 0\n"},
			{Path: "references/large.md", Content: strings.Repeat("context ", 12000)},
		},
	})
	var titles []string
	for _, issue := range issues {
		titles = append(titles, issue.Title)
	}
	got := strings.Join(titles, "\n")
	for _, want := range []string{"Supporting files are large", "Supporting files may contain sensitive material", "Script is not executable"} {
		if !strings.Contains(got, want) {
			t.Fatalf("quality issues missing %q:\n%s", want, got)
		}
	}
	if summary := qualitySummary(issues); summary != "needs attention" {
		t.Fatalf("summary = %q, want needs attention", summary)
	}
}

func TestSkillQualityFlagsNonStandardSupportingFileLocation(t *testing.T) {
	issues := skillQuality(SkillInput{
		Name:             "Review",
		Description:      "Use when reviewing a change for bugs, regressions, or missing tests.",
		ShortDescription: "Review changes for concrete risks",
		Body:             "# Review\n\nInspect the goal, affected files, tests, and behavior. Report confirmed findings first, with file references and severity. Keep questions separate from bugs.",
		Files: []SkillFile{
			{Path: "notes.md", Content: "# Notes\n"},
			{Path: "references/guide.md", Content: "# Guide\n"},
		},
	})
	var titles []string
	for _, issue := range issues {
		titles = append(titles, issue.Title)
	}
	got := strings.Join(titles, "\n")
	if !strings.Contains(got, "Supporting file is outside a standard folder") {
		t.Fatalf("quality issues missing package organization signal:\n%s", got)
	}
	if summary := qualitySummary(issues); summary != "review" {
		t.Fatalf("summary = %q, want review", summary)
	}
}
