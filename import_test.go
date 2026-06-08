package main

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"path/filepath"
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

func TestImportSkillsDexExportRejectsTraversalPath(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	manifest, err := zw.Create("skillsdex-export.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manifest.Write([]byte(`{"skills":[{"identifier":"admin/evil","name":"Evil","description":"Use when testing.","path":"../evil/SKILL.md"}]}`)); err != nil {
		t.Fatal(err)
	}
	skill, err := zw.Create("../evil/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := skill.Write([]byte("# Evil")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = importSkillInputsFromSkillsDexExport(buf.Bytes(), "admin")
	if err == nil || !strings.Contains(err.Error(), "traversal") {
		t.Fatalf("expected traversal error, got %v", err)
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
	if got.SourceProvider != "agentskill.sh" || got.SourceURL != "https://agentskill.sh/@openclaw/openclaw-debugging" {
		t.Fatalf("unexpected source: %s %s", got.SourceProvider, got.SourceURL)
	}
	if strings.Contains(got.Body, "agentskill.sh") || got.Body != "# OpenClaw Debugging\n\nUse logs." {
		t.Fatalf("body was not normalized: %q", got.Body)
	}
}

func TestRefreshImportedSkillUpdatesFromSourceAndPreservesInstallFlags(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	admin, err := getUserByUsername(ctx, db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	created, err := createSkill(ctx, db, admin, SkillInput{
		Owner:            "admin",
		Slug:             "openclaw-debugging",
		Name:             "openclaw-debugging",
		Description:      "Use when debugging OpenClaw logs.",
		ShortDescription: "Debug logs",
		PinnedInstall:    true,
		SourceProvider:   "agentskill.sh",
		SourceURL:        "https://agentskill.sh/@openclaw/openclaw-debugging",
		SourceExplicit:   true,
		Body:             "# OpenClaw Debugging\n\nOld instructions.",
	})
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://agentskill.sh/api/agent/skills/openclaw%2Fopenclaw-debugging/install" {
			t.Fatalf("unexpected request: %s", r.URL.String())
		}
		return testResponse(http.StatusOK, `{
			"slug": "openclaw/openclaw-debugging",
			"name": "openclaw-debugging",
			"description": "Use when debugging OpenClaw logs.",
			"skillMd": "---\nname: openclaw-debugging\ndescription: Use when debugging OpenClaw logs.\nmetadata:\n  short-description: Debug logs\n---\n\n# OpenClaw Debugging\n\nNew instructions.\n"
		}`), nil
	})}

	updated, status, changes, err := refreshImportedSkill(ctx, db, client, admin, "admin", "openclaw-debugging", false)
	if err != nil {
		t.Fatal(err)
	}
	if status != "updated from source" {
		t.Fatalf("status = %q, want updated from source", status)
	}
	if len(changes) != 1 || changes[0].Path != "SKILL.md" || changes[0].Status != "modified" {
		t.Fatalf("changes = %#v, want modified SKILL.md", changes)
	}
	if !updated.PinnedInstall {
		t.Fatal("refresh did not preserve pinned install")
	}
	if updated.RevisionNumber != created.RevisionNumber+1 {
		t.Fatalf("revision = %d, want %d", updated.RevisionNumber, created.RevisionNumber+1)
	}
	raw, _, err := rawSkill(ctx, db, admin.ID, "admin", "openclaw-debugging")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "New instructions.") || strings.Contains(string(raw), "Old instructions.") {
		t.Fatalf("refresh did not update raw skill:\n%s", raw)
	}
}

func TestRefreshImportedSkillDryRunDoesNotUpdateRevision(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	admin, err := getUserByUsername(ctx, db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	created, err := createSkill(ctx, db, admin, SkillInput{
		Owner:          "admin",
		Slug:           "openclaw-debugging",
		Name:           "openclaw-debugging",
		Description:    "Use when debugging OpenClaw logs.",
		SourceProvider: "agentskill.sh",
		SourceURL:      "https://agentskill.sh/@openclaw/openclaw-debugging",
		SourceExplicit: true,
		Body:           "# OpenClaw Debugging\n\nOld instructions.",
	})
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return testResponse(http.StatusOK, `{
			"slug": "openclaw/openclaw-debugging",
			"name": "openclaw-debugging",
			"description": "Use when debugging OpenClaw logs.",
			"skillMd": "---\nname: openclaw-debugging\ndescription: Use when debugging OpenClaw logs.\n---\n\n# OpenClaw Debugging\n\nNew instructions.\n"
		}`), nil
	})}

	preview, status, changes, err := refreshImportedSkill(ctx, db, client, admin, "admin", "openclaw-debugging", true)
	if err != nil {
		t.Fatal(err)
	}
	if status != "update available" {
		t.Fatalf("status = %q, want update available", status)
	}
	if len(changes) != 1 || changes[0].Path != "SKILL.md" || changes[0].Status != "modified" {
		t.Fatalf("changes = %#v, want modified SKILL.md", changes)
	}
	if preview.RevisionNumber != created.RevisionNumber {
		t.Fatalf("preview revision = %d, want %d", preview.RevisionNumber, created.RevisionNumber)
	}
	raw, _, err := rawSkill(ctx, db, admin.ID, "admin", "openclaw-debugging")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "Old instructions.") || strings.Contains(string(raw), "New instructions.") {
		t.Fatalf("dry-run changed raw skill:\n%s", raw)
	}
}

func TestRefreshImportedSkillReportsPackageFileChanges(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	admin, err := getUserByUsername(ctx, db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	_, err = createSkill(ctx, db, admin, SkillInput{
		Owner:          "admin",
		Slug:           "folder-skill",
		Name:           "folder-skill",
		Description:    "Use when testing folder refresh.",
		SourceProvider: "github",
		SourceURL:      "https://github.com/acme/skills/tree/main/folder-skill",
		SourceExplicit: true,
		Body:           "# Folder Skill\n\nOld instructions.",
		Files: []SkillFile{
			{Path: "references/old.md", Content: "old\n"},
			{Path: "scripts/run.sh", Content: "echo old\n", Executable: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.String() {
		case "https://api.github.com/repos/acme/skills/contents/folder-skill?ref=main":
			return testResponse(http.StatusOK, `[
				{"name":"SKILL.md","path":"folder-skill/SKILL.md","type":"file","download_url":"https://raw.githubusercontent.com/acme/skills/main/folder-skill/SKILL.md"},
				{"name":"references","path":"folder-skill/references","type":"dir","download_url":""},
				{"name":"scripts","path":"folder-skill/scripts","type":"dir","download_url":""}
			]`), nil
		case "https://api.github.com/repos/acme/skills/contents/folder-skill/SKILL.md?ref=main":
			return testResponse(http.StatusOK, `{"name":"SKILL.md","path":"folder-skill/SKILL.md","type":"file","download_url":"https://raw.githubusercontent.com/acme/skills/main/folder-skill/SKILL.md","html_url":"https://github.com/acme/skills/blob/main/folder-skill/SKILL.md"}`), nil
		case "https://api.github.com/repos/acme/skills/contents/folder-skill/references?ref=main":
			return testResponse(http.StatusOK, `[
				{"name":"new.md","path":"folder-skill/references/new.md","type":"file","download_url":"https://raw.githubusercontent.com/acme/skills/main/folder-skill/references/new.md"}
			]`), nil
		case "https://api.github.com/repos/acme/skills/contents/folder-skill/scripts?ref=main":
			return testResponse(http.StatusOK, `[
				{"name":"run.sh","path":"folder-skill/scripts/run.sh","type":"file","download_url":"https://raw.githubusercontent.com/acme/skills/main/folder-skill/scripts/run.sh"}
			]`), nil
		case "https://raw.githubusercontent.com/acme/skills/main/folder-skill/SKILL.md":
			return testResponse(http.StatusOK, "---\nname: folder-skill\ndescription: Use when testing folder refresh.\n---\n\n# Folder Skill\n\nNew instructions.\n"), nil
		case "https://raw.githubusercontent.com/acme/skills/main/folder-skill/references/new.md":
			return testResponse(http.StatusOK, "new\n"), nil
		case "https://raw.githubusercontent.com/acme/skills/main/folder-skill/scripts/run.sh":
			return testResponse(http.StatusOK, "echo new\n"), nil
		default:
			t.Fatalf("unexpected request: %s", r.URL.String())
			return nil, nil
		}
	})}

	_, status, changes, err := refreshImportedSkill(ctx, db, client, admin, "admin", "folder-skill", true)
	if err != nil {
		t.Fatal(err)
	}
	if status != "update available" {
		t.Fatalf("status = %q, want update available", status)
	}
	got := make([]string, 0, len(changes))
	for _, change := range changes {
		got = append(got, change.Status+" "+change.Path)
	}
	want := []string{"modified SKILL.md", "added references/new.md", "removed references/old.md", "modified scripts/run.sh"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("changes = %#v, want %#v", got, want)
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
			return testResponse(http.StatusOK, `{"name":"SKILL.md","path":"workflow/SKILL.md","type":"file","download_url":"https://raw.githubusercontent.com/acme/skills/main/workflow/SKILL.md","html_url":"https://github.com/acme/skills/blob/main/workflow/SKILL.md"}`), nil
		case "https://api.github.com/repos/acme/skills/contents/workflow":
			return testResponse(http.StatusOK, `[{"name":"SKILL.md","path":"workflow/SKILL.md","type":"file","download_url":"https://raw.githubusercontent.com/acme/skills/main/workflow/SKILL.md"},{"name":"references","path":"workflow/references","type":"dir"}]`), nil
		case "https://api.github.com/repos/acme/skills/contents/workflow/references":
			return testResponse(http.StatusOK, `[{"name":"guide.md","path":"workflow/references/guide.md","type":"file","download_url":"https://raw.githubusercontent.com/acme/skills/main/workflow/references/guide.md"}]`), nil
		case "https://api.github.com/repos/acme/skills/contents/notes/SKILL.md":
			return testResponse(http.StatusNotFound, `{"message":"Not Found"}`), nil
		case "https://raw.githubusercontent.com/acme/skills/main/workflow/SKILL.md":
			return testResponse(http.StatusOK, "---\nname: workflow\ndescription: Use when planning work.\n---\n\n# Workflow\n"), nil
		case "https://raw.githubusercontent.com/acme/skills/main/workflow/references/guide.md":
			return testResponse(http.StatusOK, "# Guide\n"), nil
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
	if inputs[0].SourceProvider != "github" || inputs[0].SourceURL != "https://github.com/acme/skills/blob/main/workflow/SKILL.md" {
		t.Fatalf("unexpected source: %s %s", inputs[0].SourceProvider, inputs[0].SourceURL)
	}
	if len(inputs[0].Files) != 1 || inputs[0].Files[0].Path != "references/guide.md" || inputs[0].Files[0].Content != "# Guide\n" {
		t.Fatalf("unexpected supporting files: %#v", inputs[0].Files)
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
