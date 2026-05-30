package main

import (
	"strings"
	"testing"
)

func TestRenderSkillMarkdown(t *testing.T) {
	raw := renderSkillMarkdown(SkillInput{
		Name:             "debugging",
		Description:      "Use when debugging.",
		ShortDescription: "Debug failures",
		Body:             "# Debugging\n\nInstructions.",
	})
	for _, want := range []string{
		`name: "debugging"`,
		`description: "Use when debugging."`,
		`short-description: "Debug failures"`,
		"# Debugging",
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("rendered markdown missing %q:\n%s", want, raw)
		}
	}
}

func TestBodyFromSkillMarkdown(t *testing.T) {
	raw := "---\nname: test\n---\n\n# Body\n"
	if got := bodyFromSkillMarkdown(raw); got != "# Body\n" {
		t.Fatalf("body = %q", got)
	}
}

func TestUnifiedDiff(t *testing.T) {
	diff := unifiedDiff("a\nb\n", "a\nc\n")
	if !strings.Contains(diff, "- b\n") || !strings.Contains(diff, "+ c\n") {
		t.Fatalf("unexpected diff:\n%s", diff)
	}
}
