package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}

func TestParseGitHubSource(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want githubSource
	}{
		{
			name: "repo root",
			raw:  "https://github.com/DannyMac180/skills",
			want: githubSource{Owner: "DannyMac180", Repo: "skills"},
		},
		{
			name: "tree path",
			raw:  "https://github.com/DannyMac180/skills/tree/main/codex-dynamic-workflows",
			want: githubSource{Owner: "DannyMac180", Repo: "skills", Ref: "main", Path: "codex-dynamic-workflows"},
		},
		{
			name: "raw skill",
			raw:  "https://raw.githubusercontent.com/DannyMac180/skills/main/codex-dynamic-workflows/SKILL.md",
			want: githubSource{Owner: "DannyMac180", Repo: "skills", Ref: "main", Path: "codex-dynamic-workflows/SKILL.md", Raw: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseGitHubSource(tt.raw)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("source = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestSkillInputFromRawMarkdown(t *testing.T) {
	raw := "---\nname: \"dynamic-workflows\"\ndescription: Use when orchestrating work.\nmetadata:\n  short-description: Workflow orchestration\n---\n\n# Dynamic Workflows\n\nInstructions.\n"
	in := skillInputFromRawMarkdown(raw, "alice", "fallback", "Imported.")
	if in.Owner != "alice" || in.Slug != "dynamic-workflows" || in.Name != "dynamic-workflows" {
		t.Fatalf("unexpected identity: %#v", in)
	}
	if in.Description != "Use when orchestrating work." {
		t.Fatalf("description = %q", in.Description)
	}
	if in.ShortDescription != "Workflow orchestration" {
		t.Fatalf("short description = %q", in.ShortDescription)
	}
	if in.Body != "# Dynamic Workflows\n\nInstructions." {
		t.Fatalf("body = %q", in.Body)
	}
}

func TestParseAgentSkillSource(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want agentSkillSource
	}{
		{
			name: "page with at owner",
			raw:  "https://agentskill.sh/@openclaw/openclaw-debugging",
			want: agentSkillSource{Slug: "openclaw/openclaw-debugging"},
		},
		{
			name: "page without at owner",
			raw:  "https://agentskill.sh/openclaw/openclaw-debugging",
			want: agentSkillSource{Slug: "openclaw/openclaw-debugging"},
		},
		{
			name: "plain slug",
			raw:  "@openclaw/openclaw-debugging",
			want: agentSkillSource{Slug: "openclaw/openclaw-debugging"},
		},
		{
			name: "api install endpoint",
			raw:  "https://agentskill.sh/api/agent/skills/openclaw%2Fopenclaw-debugging/install",
			want: agentSkillSource{Slug: "openclaw/openclaw-debugging"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAgentSkillSource(tt.raw)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("source = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestImportSkillInputsFromAgentSkill(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://agentskill.sh/api/agent/skills/openclaw%2Fopenclaw-debugging/install" {
			t.Fatalf("unexpected request: %s", r.URL.String())
		}
		return testResponse(http.StatusOK, `{
			"slug": "openclaw/openclaw-debugging",
			"name": "openclaw-debugging",
			"description": "Debug OpenClaw logs.",
			"skillMd": "---\nname: openclaw-debugging\ndescription: Debug OpenClaw logs.\n---\n# --- agentskill.sh ---\n# slug: openclaw/openclaw-debugging\n# installed: 2026-05-31T11:53:37.659Z\n# ---\n\n# OpenClaw Debugging\n\nUse logs.\n"
		}`), nil
	})}

	inputs, err := importSkillInputs(context.Background(), client, "https://agentskill.sh/@openclaw/openclaw-debugging", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 1 {
		t.Fatalf("input count = %d, want 1", len(inputs))
	}
	got := inputs[0]
	if got.Owner != "alice" || got.Slug != "openclaw-debugging" || got.Name != "openclaw-debugging" {
		t.Fatalf("unexpected input identity: %#v", got)
	}
	if strings.Contains(got.Body, "agentskill.sh") || got.Body != "# OpenClaw Debugging\n\nUse logs." {
		t.Fatalf("body was not normalized: %q", got.Body)
	}
}

func TestSearchAgentSkills(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://agentskill.sh/api/agent/search?limit=2&q=code+review" {
			t.Fatalf("unexpected request: %s", r.URL.String())
		}
		return testResponse(http.StatusOK, `{
			"results": [{
				"slug": "acme/code-review",
				"name": "code-review",
				"owner": "acme",
				"description": "Review code.",
				"installCount": 12,
				"githubStars": 345,
				"securityScore": 95,
				"contentQualityScore": 83
			}],
			"total": 1,
			"hasMore": false
		}`), nil
	})}

	results, err := searchAgentSkills(context.Background(), client, "code review", 2)
	if err != nil {
		t.Fatal(err)
	}
	if results.Total != 1 || len(results.Results) != 1 {
		t.Fatalf("unexpected search results: %#v", results)
	}
	got := results.Results[0]
	if got.Slug != "acme/code-review" || got.GitHubStars != 345 || got.SecurityScore == nil || *got.SecurityScore != 95 || got.ContentQualityScore == nil || *got.ContentQualityScore != 83 {
		t.Fatalf("unexpected result: %#v", got)
	}
}

func TestTopAgentSkills(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://agentskill.sh/api/skills?limit=100" {
			t.Fatalf("unexpected request: %s", r.URL.String())
		}
		return testResponse(http.StatusOK, `{
			"data": [{
				"slug": "openclaw/openclaw-changelog-update",
				"name": "openclaw-changelog-update",
				"owner": "openclaw",
				"description": "Regenerate changelogs.",
				"githubStars": 375331,
				"securityScore": 100,
				"contentQualityScore": 75
			}],
			"total": 193000,
			"hasMore": true
		}`), nil
	})}

	results, err := topAgentSkills(context.Background(), client, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(results.Results) != 1 || results.Results[0].Slug != "openclaw/openclaw-changelog-update" {
		t.Fatalf("unexpected top results: %#v", results)
	}
}

func TestImportSkillInputsFromGitHubRoot(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.String() {
		case "https://api.github.com/repos/acme/skills/contents":
			return testResponse(http.StatusOK, `[{"name":"workflow","path":"workflow","type":"dir"},{"name":"notes","path":"notes","type":"dir"}]`), nil
		case "https://api.github.com/repos/acme/skills/contents/workflow/SKILL.md":
			return testResponse(http.StatusOK, `{"name":"SKILL.md","path":"workflow/SKILL.md","type":"file","download_url":"https://raw.githubusercontent.com/acme/skills/main/workflow/SKILL.md"}`), nil
		case "https://api.github.com/repos/acme/skills/contents/notes/SKILL.md":
			return testResponse(http.StatusNotFound, `{"message":"Not Found"}`), nil
		case "https://raw.githubusercontent.com/acme/skills/main/workflow/SKILL.md":
			return testResponse(http.StatusOK, "---\nname: workflow\ndescription: Use when planning work.\n---\n\n# Workflow\n"), nil
		default:
			t.Fatalf("unexpected request: %s", r.URL.String())
			return nil, nil
		}
	})}

	inputs, err := importSkillInputsFromGitHub(context.Background(), client, "https://github.com/acme/skills", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 1 {
		t.Fatalf("input count = %d, want 1", len(inputs))
	}
	if inputs[0].Owner != "alice" || inputs[0].Slug != "workflow" || !strings.Contains(inputs[0].Body, "# Workflow") {
		t.Fatalf("unexpected input: %#v", inputs[0])
	}
}

func testResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}
