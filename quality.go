package main

import (
	"strconv"
	"strings"
	"time"
)

type SkillQualityIssue struct {
	Severity string
	Title    string
	Detail   string
}

type SkillQualityContext struct {
	CurrentIdentifier string
	SourceProvider    string
	SourceURL         string
	UpdatedAt         time.Time
	Skills            []Skill
}

func skillQuality(in SkillInput) []SkillQualityIssue {
	return skillQualityWithContext(in, SkillQualityContext{})
}

func skillQualityWithContext(in SkillInput, ctx SkillQualityContext) []SkillQualityIssue {
	var issues []SkillQualityIssue
	description := strings.TrimSpace(in.Description)
	shortDescription := strings.TrimSpace(in.ShortDescription)
	body := strings.TrimSpace(in.Body)

	if wordCount(description) < 6 {
		issues = append(issues, SkillQualityIssue{
			Severity: "warn",
			Title:    "Description is too short",
			Detail:   "Codex relies heavily on the description to decide when to load a skill.",
		})
	}
	if description != "" && !hasTriggerPrefix(description) {
		issues = append(issues, SkillQualityIssue{
			Severity: "warn",
			Title:    "Trigger language is unclear",
			Detail:   `Start with a phrase like "Use when..." or "Use to..." so the agent can select it reliably.`,
		})
		if suggestion := suggestedTriggerDescription(in); suggestion != "" {
			issues = append(issues, SkillQualityIssue{
				Severity: "info",
				Title:    "Suggested trigger rewrite",
				Detail:   `Try: "` + suggestion + `"`,
			})
		}
	}
	if shortDescription == "" {
		issues = append(issues, SkillQualityIssue{
			Severity: "info",
			Title:    "Short description is missing",
			Detail:   "A compact summary makes browse and install decisions easier.",
		})
	}
	if body == "" || wordCount(body) < 20 {
		issues = append(issues, SkillQualityIssue{
			Severity: "warn",
			Title:    "Instructions are sparse",
			Detail:   "Add concrete workflow steps, boundaries, and expected outputs.",
		})
	}
	if body != "" && !hasMarkdownH1(body) {
		issues = append(issues, SkillQualityIssue{
			Severity: "info",
			Title:    "Top-level heading is missing",
			Detail:   "A single H1 makes the skill easier to scan in previews and exports.",
		})
	}
	if len(body) > 14000 {
		issues = append(issues, SkillQualityIssue{
			Severity: "warn",
			Title:    "Loaded context is very large",
			Detail:   "Move long references, examples, and templates into supporting files when full skill folders are available.",
		})
	} else if len(body) > 8000 {
		issues = append(issues, SkillQualityIssue{
			Severity: "info",
			Title:    "Loaded context is getting large",
			Detail:   "Keep the always-loaded SKILL.md focused so the agent has room for task context.",
		})
	}
	if total := supportingFilesSize(in.Files); total > 75000 {
		issues = append(issues, SkillQualityIssue{
			Severity: "info",
			Title:    "Supporting files are large",
			Detail:   "This skill package has " + humanBytes(total) + " in supporting files. Keep references focused so agents can find the right context quickly.",
		})
	}
	if mentionsSensitiveTerm(body) {
		issues = append(issues, SkillQualityIssue{
			Severity: "warn",
			Title:    "Possible sensitive material",
			Detail:   "Check that the skill does not contain real credentials, tokens, passwords, or private operational secrets.",
		})
	}
	if supportingFilesMentionSensitiveTerm(in.Files) {
		issues = append(issues, SkillQualityIssue{
			Severity: "warn",
			Title:    "Supporting files may contain sensitive material",
			Detail:   "Check references, scripts, assets, and templates for real credentials, tokens, passwords, or private operational secrets.",
		})
	}
	if file := nonStandardSupportingFile(in.Files); file != "" {
		issues = append(issues, SkillQualityIssue{
			Severity: "info",
			Title:    "Supporting file is outside a standard folder",
			Detail:   file + " is not under references/, scripts/, assets/, or templates/. Move it into a standard folder so agents and exports are easier to scan.",
		})
	}
	if script := nonExecutableScript(in.Files); script != "" {
		issues = append(issues, SkillQualityIssue{
			Severity: "info",
			Title:    "Script is not executable",
			Detail:   script + " is under scripts/ but is not marked executable. Use chmod +x if agents should run it directly.",
		})
	}
	if strings.Contains(strings.ToLower(body), "todo") {
		issues = append(issues, SkillQualityIssue{
			Severity: "info",
			Title:    "TODO text found",
			Detail:   "Replace placeholders before installing this as a daily-use skill.",
		})
	}
	if duplicate := duplicateSkill(ctx, in); duplicate != "" {
		issues = append(issues, SkillQualityIssue{
			Severity: "info",
			Title:    "Possible duplicate skill",
			Detail:   "Similar metadata found in " + duplicate + ". Merge or sharpen the trigger if both should remain.",
		})
	}
	if sourceMayBeStale(ctx) {
		issues = append(issues, SkillQualityIssue{
			Severity: "info",
			Title:    "Source may be stale",
			Detail:   "This imported skill has not changed locally in over 90 days. Use Refresh Source to check upstream before relying on it.",
		})
	}
	return issues
}

func qualitySummary(issues []SkillQualityIssue) string {
	if len(issues) == 0 {
		return "ready"
	}
	for _, issue := range issues {
		if issue.Severity == "warn" {
			return "needs attention"
		}
	}
	return "review"
}

func hasTriggerPrefix(description string) bool {
	lower := strings.ToLower(strings.TrimSpace(description))
	for _, prefix := range []string{"use when ", "use to ", "use for ", "use before ", "use after ", "use if "} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func hasMarkdownH1(body string) bool {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "# ") {
			return true
		}
	}
	return false
}

func mentionsSensitiveTerm(body string) bool {
	lower := strings.ToLower(body)
	for _, term := range []string{"api key", "apikey", "password", "secret", "access token", "bearer token", "private key"} {
		if strings.Contains(lower, term) {
			return true
		}
	}
	return false
}

func supportingFilesSize(files []SkillFile) int {
	total := 0
	for _, file := range files {
		if file.Path == "SKILL.md" {
			continue
		}
		if file.SizeBytes > 0 {
			total += int(file.SizeBytes)
			continue
		}
		total += len(file.Content)
	}
	return total
}

func supportingFilesMentionSensitiveTerm(files []SkillFile) bool {
	for _, file := range files {
		if file.Path == "SKILL.md" {
			continue
		}
		if mentionsSensitiveTerm(file.Path) || mentionsSensitiveTerm(file.Content) {
			return true
		}
	}
	return false
}

func nonStandardSupportingFile(files []SkillFile) string {
	for _, file := range files {
		if file.Path == "SKILL.md" {
			continue
		}
		if !isStandardSupportingFilePath(file.Path) {
			return file.Path
		}
	}
	return ""
}

func isStandardSupportingFilePath(path string) bool {
	for _, prefix := range []string{"references/", "scripts/", "assets/", "templates/"} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func nonExecutableScript(files []SkillFile) string {
	for _, file := range files {
		if file.Path == "SKILL.md" || file.Executable {
			continue
		}
		if strings.HasPrefix(file.Path, "scripts/") {
			return file.Path
		}
	}
	return ""
}

func humanBytes(n int) string {
	if n < 1024 {
		return strconv.Itoa(n) + " bytes"
	}
	kib := n / 1024
	if kib < 1024 {
		return strconv.Itoa(kib) + " KiB"
	}
	return strconv.Itoa(kib/1024) + " MiB"
}

func suggestedTriggerDescription(in SkillInput) string {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = "this skill"
	}
	short := strings.TrimSpace(in.ShortDescription)
	if short != "" {
		return "Use when " + strings.ToLower(firstWordLower(short)) + strings.TrimPrefix(short, firstWord(short)) + "."
	}
	desc := strings.TrimSpace(in.Description)
	if desc != "" && !strings.HasSuffix(desc, ".") {
		desc += "."
	}
	if desc == "" {
		desc = "the task needs " + strings.ToLower(name) + "."
	}
	return "Use when " + strings.TrimSuffix(strings.ToLower(desc), ".") + "."
}

func firstWord(value string) string {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func firstWordLower(value string) string {
	return strings.ToLower(firstWord(value))
}

func duplicateSkill(ctx SkillQualityContext, in SkillInput) string {
	desc := normalizeQualityText(in.Description)
	short := normalizeQualityText(in.ShortDescription)
	name := normalizeQualityText(in.Name)
	for _, skill := range ctx.Skills {
		id := skill.OwnerSlug + "/" + skill.Slug
		if id == ctx.CurrentIdentifier || skill.ArchivedAt != "" {
			continue
		}
		if desc != "" && desc == normalizeQualityText(skill.Description) {
			return id
		}
		if short != "" && short == normalizeQualityText(skill.ShortDescription) {
			return id
		}
		if name != "" && name == normalizeQualityText(skill.Name) && descWordsOverlap(desc, normalizeQualityText(skill.Description)) {
			return id
		}
	}
	return ""
}

func descWordsOverlap(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	seen := map[string]bool{}
	for _, word := range strings.Fields(a) {
		if len(word) >= 5 {
			seen[word] = true
		}
	}
	for _, word := range strings.Fields(b) {
		if seen[word] {
			return true
		}
	}
	return false
}

func normalizeQualityText(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(value))), " ")
}

func sourceMayBeStale(ctx SkillQualityContext) bool {
	if strings.TrimSpace(ctx.SourceProvider) == "" && strings.TrimSpace(ctx.SourceURL) == "" {
		return false
	}
	if ctx.UpdatedAt.IsZero() {
		return false
	}
	return ctx.UpdatedAt.Before(nowUTC().AddDate(0, 0, -90))
}

func wordCount(s string) int {
	return len(strings.Fields(s))
}
