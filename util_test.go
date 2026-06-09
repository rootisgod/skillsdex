package skillsdex

import (
	"reflect"
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

func TestParsePluginInstallCommand(t *testing.T) {
	args, err := parsePluginInstallCommand(`codex plugin install "https://github.com/TheGreenCedar/codex-autoresearch"`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"codex", "plugin", "install", "https://github.com/TheGreenCedar/codex-autoresearch"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
	if _, err := parsePluginInstallCommand(`bash -lc "echo unsafe"`); err == nil || !strings.Contains(err.Error(), "must not invoke a shell") {
		t.Fatalf("shell command err = %v, want shell refusal", err)
	}
	if _, err := parsePluginInstallCommand(`codex plugin install "unterminated`); err == nil || !strings.Contains(err.Error(), "unterminated quote") {
		t.Fatalf("unterminated err = %v, want quote refusal", err)
	}
}

func TestUnifiedDiff(t *testing.T) {
	diff := unifiedDiff("a\nb\n", "a\nc\n")
	if !strings.Contains(diff, "- b\n") || !strings.Contains(diff, "+ c\n") {
		t.Fatalf("unexpected diff:\n%s", diff)
	}
}
