package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
)

const maxImportedSkillBytes = 1 << 20

var errGitHubNotFound = errors.New("github content not found")

type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type githubSource struct {
	Owner string
	Repo  string
	Ref   string
	Path  string
	Raw   bool
}

type githubContent struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Type        string `json:"type"`
	DownloadURL string `json:"download_url"`
	HTMLURL     string `json:"html_url"`
}

type agentSkillSource struct {
	Slug string
}

type agentSkillInstallResponse struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Owner       string `json:"owner"`
	Description string `json:"description"`
	SkillMd     string `json:"skillMd"`
}

type agentSkillSearchResponse struct {
	Results []agentSkillSearchResult `json:"results"`
	Total   int                      `json:"total"`
	HasMore bool                     `json:"hasMore"`
}

type agentSkillListResponse struct {
	Data    []agentSkillSearchResult `json:"data"`
	Total   int                      `json:"total"`
	HasMore bool                     `json:"hasMore"`
}

type agentSkillSearchResult struct {
	Slug                 string `json:"slug"`
	Name                 string `json:"name"`
	Owner                string `json:"owner"`
	Description          string `json:"description"`
	Category             string `json:"category"`
	InstallCount         int    `json:"installCount"`
	GitHubStars          int    `json:"githubStars"`
	SecurityScore        *int   `json:"securityScore"`
	ContentQualityScore  *int   `json:"contentQualityScore"`
	ContentQualityScore2 *int   `json:"content_quality_score"`
}

func importSkillInputs(ctx context.Context, client httpDoer, source, owner string) ([]SkillInput, error) {
	if _, err := parseAgentSkillSource(source); err == nil {
		return importSkillInputsFromAgentSkill(ctx, client, source, owner)
	}
	return importSkillInputsFromGitHub(ctx, client, source, owner)
}

func importSkillInputsFromGitHub(ctx context.Context, client httpDoer, sourceURL, owner string) ([]SkillInput, error) {
	source, err := parseGitHubSource(sourceURL)
	if err != nil {
		return nil, err
	}
	var skills []SkillInput
	if source.Raw {
		raw, err := fetchGitHubRaw(ctx, client, sourceURL)
		if err != nil {
			return nil, err
		}
		fallback := strings.TrimSuffix(path.Base(source.Path), path.Ext(source.Path))
		skills = append(skills, withSource(skillInputFromRawMarkdown(raw, owner, fallback, "Imported from GitHub."), "github", sourceURL))
		return skills, nil
	}

	if strings.EqualFold(path.Base(source.Path), "SKILL.md") {
		raw, sourceHTMLURL, err := fetchGitHubFile(ctx, client, source, source.Path)
		if err != nil {
			return nil, err
		}
		fallback := strings.TrimSuffix(path.Base(path.Dir(source.Path)), path.Ext(path.Dir(source.Path)))
		skills = append(skills, withSource(skillInputFromRawMarkdown(raw, owner, fallback, "Imported from GitHub."), "github", firstNonEmpty(sourceHTMLURL, sourceURL)))
		return skills, nil
	}

	if source.Path != "" {
		raw, sourceHTMLURL, err := fetchGitHubFile(ctx, client, source, path.Join(source.Path, "SKILL.md"))
		if err != nil {
			if errors.Is(err, errGitHubNotFound) {
				return nil, fmt.Errorf("no SKILL.md found at %s", sourceURL)
			}
			return nil, err
		}
		skills = append(skills, withSource(skillInputFromRawMarkdown(raw, owner, path.Base(source.Path), "Imported from GitHub."), "github", firstNonEmpty(sourceHTMLURL, sourceURL)))
		return skills, nil
	}

	entries, err := fetchGitHubDirectory(ctx, client, source, "")
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		switch entry.Type {
		case "file":
			if strings.EqualFold(entry.Name, "SKILL.md") {
				raw, sourceHTMLURL, err := fetchGitHubFile(ctx, client, source, entry.Path)
				if err != nil {
					return nil, err
				}
				skills = append(skills, withSource(skillInputFromRawMarkdown(raw, owner, source.Repo, "Imported from GitHub."), "github", firstNonEmpty(sourceHTMLURL, entry.HTMLURL, sourceURL)))
			}
		case "dir":
			raw, sourceHTMLURL, err := fetchGitHubFile(ctx, client, source, path.Join(entry.Path, "SKILL.md"))
			if errors.Is(err, errGitHubNotFound) {
				continue
			}
			if err != nil {
				return nil, err
			}
			skills = append(skills, withSource(skillInputFromRawMarkdown(raw, owner, entry.Name, "Imported from GitHub."), "github", firstNonEmpty(entry.HTMLURL, sourceHTMLURL, sourceURL)))
		}
	}
	return skills, nil
}

func importSkillInputsFromAgentSkill(ctx context.Context, client httpDoer, sourceURL, owner string) ([]SkillInput, error) {
	source, err := parseAgentSkillSource(sourceURL)
	if err != nil {
		return nil, err
	}
	installURL := "https://agentskill.sh/api/agent/skills/" + url.PathEscape(source.Slug) + "/install"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, installURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "skillsdex")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("agentskill.sh skill not found: %s", source.Slug)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("agentskill.sh returned %s", resp.Status)
	}
	var payload agentSkillInstallResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxImportedSkillBytes)).Decode(&payload); err != nil {
		return nil, errors.New("invalid agentskill.sh response")
	}
	if strings.TrimSpace(payload.SkillMd) == "" {
		return nil, errors.New("agentskill.sh response did not include SKILL.md")
	}
	fallbackSlug := firstNonEmpty(payload.Name, path.Base(source.Slug))
	fallbackDescription := firstNonEmpty(payload.Description, "Imported from agentskill.sh.")
	in := skillInputFromRawMarkdown(payload.SkillMd, owner, fallbackSlug, fallbackDescription)
	in.Body = stripAgentSkillMetadataHeader(in.Body)
	in = withSource(in, "agentskill.sh", "https://agentskill.sh/@"+source.Slug)
	return []SkillInput{in}, nil
}

func searchAgentSkills(ctx context.Context, client httpDoer, query string, limit int) (agentSkillSearchResponse, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return agentSkillSearchResponse{}, nil
	}
	return fetchAgentSkillSearch(ctx, client, query, limit)
}

func topAgentSkills(ctx context.Context, client httpDoer, limit int) (agentSkillSearchResponse, error) {
	if limit <= 0 {
		limit = 100
	} else if limit > 100 {
		limit = 100
	}
	listURL := "https://agentskill.sh/api/skills?limit=" + url.QueryEscape(strconv.Itoa(limit))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, listURL, nil)
	if err != nil {
		return agentSkillSearchResponse{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "skillsdex")
	resp, err := client.Do(req)
	if err != nil {
		return agentSkillSearchResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return agentSkillSearchResponse{}, fmt.Errorf("agentskill.sh returned %s", resp.Status)
	}
	var payload agentSkillListResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxImportedSkillBytes)).Decode(&payload); err != nil {
		return agentSkillSearchResponse{}, errors.New("invalid agentskill.sh skills response")
	}
	normalizeAgentSkillResults(payload.Data)
	return agentSkillSearchResponse{Results: payload.Data, Total: payload.Total, HasMore: payload.HasMore}, nil
}

func fetchAgentSkillSearch(ctx context.Context, client httpDoer, query string, limit int) (agentSkillSearchResponse, error) {
	if limit <= 0 {
		limit = 20
	} else if limit > 100 {
		limit = 100
	}
	searchURL := "https://agentskill.sh/api/agent/search?limit=" + url.QueryEscape(strconv.Itoa(limit))
	if strings.TrimSpace(query) != "" {
		searchURL += "&q=" + url.QueryEscape(strings.TrimSpace(query))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return agentSkillSearchResponse{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "skillsdex")
	resp, err := client.Do(req)
	if err != nil {
		return agentSkillSearchResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return agentSkillSearchResponse{}, fmt.Errorf("agentskill.sh returned %s", resp.Status)
	}
	var payload agentSkillSearchResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxImportedSkillBytes)).Decode(&payload); err != nil {
		return agentSkillSearchResponse{}, errors.New("invalid agentskill.sh search response")
	}
	normalizeAgentSkillResults(payload.Results)
	return payload, nil
}

func normalizeAgentSkillResults(results []agentSkillSearchResult) {
	for i := range results {
		if results[i].ContentQualityScore == nil {
			results[i].ContentQualityScore = results[i].ContentQualityScore2
		}
	}
}

func upsertImportedSkill(ctx context.Context, db *sql.DB, user *User, in SkillInput) (*Skill, string, error) {
	in.Owner = user.Username
	if existing, err := getSkill(ctx, db, user.ID, in.Owner, in.Slug); err == nil {
		if existing.ArchivedAt != "" {
			return nil, "", errors.New("archived skills cannot be imported")
		}
		raw, _, err := rawSkill(ctx, db, user.ID, in.Owner, in.Slug)
		if err != nil {
			return nil, "", err
		}
		if string(raw) == renderSkillMarkdown(in) {
			updated, err := updateSkillSource(ctx, db, user.ID, in.Owner, in.Slug, in)
			return updated, "unchanged", err
		}
		in.RecommendedDefault = existing.RecommendedDefault
		updated, err := updateSkill(ctx, db, user, in.Owner, in.Slug, in)
		return updated, "updated", err
	} else if !isNotFound(err) {
		return nil, "", err
	}
	created, err := createSkill(ctx, db, user, in)
	return created, "created", err
}

func withSource(in SkillInput, provider, sourceURL string) SkillInput {
	in.SourceProvider = provider
	in.SourceURL = strings.TrimSpace(sourceURL)
	in.SourceExplicit = true
	return in
}

func parseGitHubSource(raw string) (githubSource, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return githubSource{}, errors.New("enter a valid GitHub URL")
	}
	host := strings.ToLower(u.Hostname())
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if host == "raw.githubusercontent.com" {
		if len(parts) < 4 {
			return githubSource{}, errors.New("raw GitHub URLs must include owner, repo, ref, and path")
		}
		return githubSource{Owner: parts[0], Repo: strings.TrimSuffix(parts[1], ".git"), Ref: parts[2], Path: strings.Join(parts[3:], "/"), Raw: true}, nil
	}
	if host != "github.com" && host != "www.github.com" {
		return githubSource{}, errors.New("only github.com URLs are supported")
	}
	if len(parts) < 2 {
		return githubSource{}, errors.New("GitHub URL must include owner and repo")
	}
	source := githubSource{Owner: parts[0], Repo: strings.TrimSuffix(parts[1], ".git")}
	if source.Owner == "" || source.Repo == "" {
		return githubSource{}, errors.New("GitHub URL must include owner and repo")
	}
	if len(parts) >= 4 && (parts[2] == "tree" || parts[2] == "blob") {
		source.Ref = parts[3]
		if len(parts) > 4 {
			source.Path = strings.Join(parts[4:], "/")
		}
	}
	return source, nil
}

func parseAgentSkillSource(raw string) (agentSkillSource, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return agentSkillSource{}, errors.New("enter an agentskill.sh skill URL or @owner/skill")
	}
	slug := raw
	if u, err := url.Parse(raw); err == nil && u.Scheme != "" && u.Host != "" {
		host := strings.ToLower(u.Hostname())
		if host != "agentskill.sh" && host != "www.agentskill.sh" {
			return agentSkillSource{}, errors.New("not an agentskill.sh URL")
		}
		escapedPath := u.EscapedPath()
		if escapedPath == "" {
			escapedPath = u.Path
		}
		parts := strings.Split(strings.Trim(escapedPath, "/"), "/")
		if len(parts) >= 5 && parts[0] == "api" && parts[1] == "agent" && parts[2] == "skills" && parts[4] == "install" {
			unescaped, err := url.PathUnescape(parts[3])
			if err != nil {
				return agentSkillSource{}, errors.New("invalid agentskill.sh skill slug")
			}
			slug = unescaped
		} else if len(parts) == 2 {
			slug = strings.TrimPrefix(parts[0], "@") + "/" + parts[1]
		} else {
			return agentSkillSource{}, errors.New("agentskill.sh URL must point to a skill page")
		}
	} else {
		slug = strings.TrimPrefix(slug, "@")
	}
	parts := strings.Split(strings.Trim(slug, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return agentSkillSource{}, errors.New("agentskill.sh skill must be @owner/skill")
	}
	for _, reserved := range []string{"api", "install", "readme", "submit", "docs", "skillsets", "agents", "plugins", "creators", "reviews", "blog"} {
		if parts[0] == reserved {
			return agentSkillSource{}, errors.New("agentskill.sh URL must point to a skill page")
		}
	}
	return agentSkillSource{Slug: parts[0] + "/" + parts[1]}, nil
}

func fetchGitHubDirectory(ctx context.Context, client httpDoer, source githubSource, contentPath string) ([]githubContent, error) {
	body, err := fetchGitHubAPI(ctx, client, source, contentPath)
	if err != nil {
		return nil, err
	}
	var entries []githubContent
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, errors.New("GitHub path is not a directory")
	}
	return entries, nil
}

func fetchGitHubFile(ctx context.Context, client httpDoer, source githubSource, contentPath string) (string, string, error) {
	body, err := fetchGitHubAPI(ctx, client, source, contentPath)
	if err != nil {
		return "", "", err
	}
	var file githubContent
	if err := json.Unmarshal(body, &file); err != nil {
		return "", "", errors.New("GitHub path is not a file")
	}
	if file.Type != "file" || file.DownloadURL == "" {
		return "", "", errGitHubNotFound
	}
	raw, err := fetchGitHubRaw(ctx, client, file.DownloadURL)
	return raw, file.HTMLURL, err
}

func fetchGitHubAPI(ctx context.Context, client httpDoer, source githubSource, contentPath string) ([]byte, error) {
	apiURL := "https://api.github.com/repos/" + url.PathEscape(source.Owner) + "/" + url.PathEscape(source.Repo) + "/contents"
	if contentPath != "" {
		apiURL += "/" + escapeGitHubPath(contentPath)
	}
	if source.Ref != "" {
		apiURL += "?ref=" + url.QueryEscape(source.Ref)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "skillsdex")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, errGitHubNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GitHub returned %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxImportedSkillBytes))
}

func fetchGitHubRaw(ctx context.Context, client httpDoer, rawURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "skillsdex")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", errGitHubNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("GitHub returned %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxImportedSkillBytes))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func escapeGitHubPath(contentPath string) string {
	parts := strings.Split(strings.Trim(contentPath, "/"), "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func skillInputFromRawMarkdown(raw, owner, fallbackSlug, fallbackDescription string) SkillInput {
	frontmatter, body := parseSkillFrontmatter(raw)
	slug := slugify(firstNonEmpty(frontmatter["name"], fallbackSlug, "imported-skill"))
	name := firstNonEmpty(frontmatter["name"], fallbackSlug, slug)
	return SkillInput{
		Owner:            owner,
		Slug:             slug,
		Name:             name,
		Description:      firstNonEmpty(frontmatter["description"], fallbackDescription),
		ShortDescription: firstNonEmpty(frontmatter["metadata.short-description"], frontmatter["metadata.short_description"]),
		Body:             strings.TrimRight(strings.TrimLeft(body, "\n"), "\r\n"),
	}
}

func stripAgentSkillMetadataHeader(body string) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	const start = "# --- agentskill.sh ---\n"
	if !strings.HasPrefix(body, start) {
		return body
	}
	if idx := strings.Index(body[len(start):], "\n# ---"); idx >= 0 {
		after := body[len(start)+idx+len("\n# ---"):]
		after = strings.TrimPrefix(after, "\n")
		return strings.TrimLeft(after, "\n")
	}
	return body
}

func parseSkillFrontmatter(raw string) (map[string]string, string) {
	out := map[string]string{}
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return out, normalized
	}
	end := strings.Index(normalized[4:], "\n---")
	if end < 0 {
		return out, normalized
	}
	fm := normalized[4 : 4+end]
	bodyStart := 4 + end + len("\n---")
	if len(normalized) > bodyStart && normalized[bodyStart] == '\n' {
		bodyStart++
	}
	section := ""
	for _, line := range strings.Split(fm, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if indent == 0 && value == "" {
			section = key
			continue
		}
		if indent > 0 && section != "" {
			key = section + "." + key
		} else if indent == 0 {
			section = ""
		}
		out[key] = yamlScalar(value)
	}
	return out, normalized[bodyStart:]
}

func yamlScalar(value string) string {
	value = strings.TrimSpace(value)
	if unquoted, err := strconv.Unquote(value); err == nil {
		return unquoted
	}
	if len(value) >= 2 {
		first, last := value[0], value[len(value)-1]
		if (first == '\'' && last == '\'') || (first == '"' && last == '"') {
			return value[1 : len(value)-1]
		}
	}
	return value
}

func slugify(value string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(value) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if len(slug) > 63 {
		slug = strings.Trim(slug[:63], "-")
	}
	if slug == "" {
		return "imported-skill"
	}
	return slug
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
