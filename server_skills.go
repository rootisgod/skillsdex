package skillsdex

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (a *app) handleSkills(w http.ResponseWriter, r *http.Request) {
	u := webUser(r)
	if r.URL.Path != "/skills" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	selectedTag := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("tag")))
	if selectedTag != "" {
		if err := validateTagSlug(selectedTag); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	skills, err := listSkillsFiltered(r.Context(), a.db, u.ID, r.URL.Query().Get("archived") == "1", selectedTag)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	allSkills, err := listSkills(r.Context(), a.db, u.ID, r.URL.Query().Get("archived") == "1")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, r, u, "Skills", skillsPage(skills, allSkills, r.URL.Query().Get("archived") == "1", selectedTag))
}

func (a *app) handleSkillRoute(w http.ResponseWriter, r *http.Request) {
	u := webUser(r)
	rel := strings.Trim(strings.TrimPrefix(r.URL.Path, "/skills/"), "/")
	if rel == "new" {
		a.handleSkillNew(w, r, u)
		return
	}
	if rel == "import" {
		a.handleSkillImport(w, r, u)
		return
	}
	if rel == "top" {
		a.handleSkillTop(w, r, u)
		return
	}
	if rel == "export" {
		a.handleSkillExport(w, r, u)
		return
	}
	if rel == "export/github" {
		a.handleSkillGitHubExport(w, r, u)
		return
	}
	parts := strings.Split(rel, "/")
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}
	owner, slug := parts[0], parts[1]
	if len(parts) == 2 {
		a.handleSkillDetail(w, r, u, owner, slug)
		return
	}
	switch parts[2] {
	case "edit":
		a.handleSkillEdit(w, r, u, owner, slug)
	case "raw":
		a.handleSkillRaw(w, r, u, owner, slug)
	case "archive":
		a.handleSkillArchive(w, r, u, owner, slug)
	case "refresh":
		a.handleSkillRefresh(w, r, u, owner, slug)
	case "delete":
		a.handleSkillDelete(w, r, u, owner, slug)
	case "revisions":
		if len(parts) == 3 {
			a.handleRevisions(w, r, u, owner, slug)
			return
		}
		if len(parts) == 4 {
			a.handleRevision(w, r, u, owner, slug, parts[3])
			return
		}
		http.NotFound(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (a *app) handleSkillNew(w http.ResponseWriter, r *http.Request, u *User) {
	in := SkillInput{Owner: u.Username, Body: "# New Skill\n\nInstructions go here."}
	if r.Method == http.MethodGet {
		if tmpl := skillTemplate(r.URL.Query().Get("template"), u.Username); tmpl.Body != "" {
			in = tmpl
		}
		a.render(w, r, u, "New Skill", skillFormPage("/skills/new", true, in, ""))
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	in = skillInputFromForm(r, u.Username)
	if r.FormValue("action") == "preview" {
		a.render(w, r, u, "New Skill", skillFormPage("/skills/new", true, in, ""))
		return
	}
	s, err := createSkill(r.Context(), a.db, u, in)
	if err != nil {
		a.render(w, r, u, "New Skill", skillFormPage("/skills/new", true, in, err.Error()))
		return
	}
	a.audit(r, u, "skill_created", "skill", s.ID, s.OwnerSlug+"/"+s.Slug, map[string]any{"name": s.Name})
	http.Redirect(w, r, "/skills/"+s.OwnerSlug+"/"+s.Slug, http.StatusFound)
}

func (a *app) handleSkillImport(w http.ResponseWriter, r *http.Request, u *User) {
	switch r.Method {
	case http.MethodGet:
		query := strings.TrimSpace(r.URL.Query().Get("q"))
		var search agentSkillSearchResponse
		searchMsg := ""
		if query != "" {
			var err error
			search, err = searchAgentSkills(r.Context(), a.sourceHTTPClient(), query, 10)
			if err != nil {
				searchMsg = err.Error()
			}
		}
		a.render(w, r, u, "Import Skills", skillImportPage("", "", nil, query, search, searchMsg))
	case http.MethodPost:
		if isMultipartForm(r) {
			if err := r.ParseMultipartForm(maxSkillsDexExportBytes); err != nil {
				a.render(w, r, u, "Import Skills", skillImportPage("", "bad backup upload", nil, "", agentSkillSearchResponse{}, ""))
				return
			}
			file, _, err := r.FormFile("restore_zip")
			if err != nil {
				a.render(w, r, u, "Import Skills", skillImportPage("", "choose a SkillsDex backup zip", nil, "", agentSkillSearchResponse{}, ""))
				return
			}
			defer file.Close()
			data, err := io.ReadAll(io.LimitReader(file, maxSkillsDexExportBytes+1))
			if err != nil {
				a.render(w, r, u, "Import Skills", skillImportPage("", err.Error(), nil, "", agentSkillSearchResponse{}, ""))
				return
			}
			if len(data) > maxSkillsDexExportBytes {
				a.render(w, r, u, "Import Skills", skillImportPage("", "backup upload is too large", nil, "", agentSkillSearchResponse{}, ""))
				return
			}
			results, err := a.restoreSkillsDexExport(r.Context(), u, data)
			if err != nil {
				a.render(w, r, u, "Import Skills", skillImportPage("", err.Error(), nil, "", agentSkillSearchResponse{}, ""))
				return
			}
			a.audit(r, u, "backup_restored", "backup", "", "SkillsDex backup", map[string]any{"results": len(results)})
			a.render(w, r, u, "Import Skills", skillImportPage("", "", results, "", agentSkillSearchResponse{}, ""))
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		source := strings.TrimSpace(firstNonEmpty(r.FormValue("source_url"), r.FormValue("github_url")))
		results, err := a.importSource(r, u, source)
		if err != nil {
			a.render(w, r, u, "Import Skills", skillImportPage(source, err.Error(), nil, "", agentSkillSearchResponse{}, ""))
			return
		}
		a.render(w, r, u, "Import Skills", skillImportPage(source, "", results, "", agentSkillSearchResponse{}, ""))
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *app) importSource(r *http.Request, u *User, source string) ([]skillImportResult, error) {
	inputs, err := importSkillInputs(r.Context(), a.sourceHTTPClient(), source, u.Username)
	if err != nil {
		return nil, err
	}
	var results []skillImportResult
	for _, in := range inputs {
		s, status, err := upsertImportedSkill(r.Context(), a.db, u, in)
		if err != nil {
			results = append(results, skillImportResult{Identifier: in.Owner + "/" + in.Slug, Name: in.Name, Status: err.Error()})
			continue
		}
		a.audit(r, u, "skill_imported", "skill", s.ID, s.OwnerSlug+"/"+s.Slug, map[string]any{"source": source, "status": status})
		results = append(results, skillImportResult{Identifier: s.OwnerSlug + "/" + s.Slug, Name: s.Name, Status: status})
	}
	a.audit(r, u, "source_imported", "source", "", source, map[string]any{"results": len(results)})
	return results, nil
}

func (a *app) restoreSkillsDexExport(ctx context.Context, u *User, data []byte) ([]skillImportResult, error) {
	inputs, err := importSkillInputsFromSkillsDexExport(data, u.Username)
	if err != nil {
		return nil, err
	}
	var results []skillImportResult
	for _, restored := range inputs {
		in := restored.Input
		s, status, err := upsertImportedSkill(ctx, a.db, u, in)
		if err != nil {
			results = append(results, skillImportResult{Identifier: in.Owner + "/" + in.Slug, Name: in.Name, Status: err.Error()})
			continue
		}
		if restored.Archived {
			if err := archiveSkill(ctx, a.db, u.ID, s.OwnerSlug, s.Slug); err != nil {
				results = append(results, skillImportResult{Identifier: s.OwnerSlug + "/" + s.Slug, Name: s.Name, Status: err.Error()})
				continue
			}
			status += " and archived"
		}
		results = append(results, skillImportResult{Identifier: s.OwnerSlug + "/" + s.Slug, Name: s.Name, Status: status})
	}
	return results, nil
}

func (a *app) handleSkillTop(w http.ResponseWriter, r *http.Request, u *User) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	skills, err := topAgentSkills(r.Context(), a.sourceHTTPClient(), 100)
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	a.render(w, r, u, "Top Skills", topSkillsPage(skills, msg))
}

func (a *app) handleSkillExport(w http.ResponseWriter, r *http.Request, u *User) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	data, err := buildSkillsExport(r.Context(), a.db, u)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.audit(r, u, "skills_exported", "backup", "", u.Username, nil)
	filename := "skillsdex-" + exportPathPart(u.Username) + "-skills.zip"
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

func (a *app) handleSkillGitHubExport(w http.ResponseWriter, r *http.Request, u *User) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	data, err := buildGitHubExport(r.Context(), a.db, u)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.audit(r, u, "github_exported", "export", "", u.Username, nil)
	filename := "skillsdex-" + exportPathPart(u.Username) + "-github.zip"
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

type exportManifest struct {
	ExportedAt string            `json:"exported_at"`
	User       string            `json:"user"`
	Skills     []exportSkillMeta `json:"skills"`
}

type exportSkillMeta struct {
	Identifier         string   `json:"identifier"`
	Name               string   `json:"name"`
	Description        string   `json:"description"`
	ShortDescription   string   `json:"short_description,omitempty"`
	Tags               []string `json:"tags,omitempty"`
	RevisionNumber     int      `json:"revision_number"`
	Archived           bool     `json:"archived"`
	RecommendedDefault bool     `json:"recommended_default"`
	PinnedInstall      bool     `json:"pinned_install"`
	SourceProvider     string   `json:"source_provider,omitempty"`
	SourceURL          string   `json:"source_url,omitempty"`
	SourceAuthor       string   `json:"source_author,omitempty"`
	SourceName         string   `json:"source_name,omitempty"`
	SourcePath         string   `json:"source_path,omitempty"`
	SourceRef          string   `json:"source_ref,omitempty"`
	Path               string   `json:"path"`
}

func buildSkillsExport(ctx context.Context, db *sql.DB, u *User) ([]byte, error) {
	skills, err := listSkills(ctx, db, u.ID, true)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	manifest := exportManifest{ExportedAt: nowUTC().Format(time.RFC3339), User: u.Username}
	for _, s := range skills {
		files, _, err := currentSkillFiles(ctx, db, u.ID, s.OwnerSlug, s.Slug)
		if err != nil {
			return nil, err
		}
		dir := exportPathPart(s.OwnerSlug) + "--" + exportPathPart(s.Slug)
		skillPath := dir + "/SKILL.md"
		for _, file := range files {
			if err := validateSkillFilePath(file.Path); err != nil {
				return nil, err
			}
			f, err := zw.Create(dir + "/" + file.Path)
			if err != nil {
				return nil, err
			}
			if _, err := f.Write([]byte(file.Content)); err != nil {
				return nil, err
			}
		}
		manifest.Skills = append(manifest.Skills, exportSkillMeta{
			Identifier:         s.OwnerSlug + "/" + s.Slug,
			Name:               s.Name,
			Description:        s.Description,
			ShortDescription:   s.ShortDescription,
			Tags:               s.Tags,
			RevisionNumber:     s.RevisionNumber,
			Archived:           s.ArchivedAt != "",
			RecommendedDefault: s.RecommendedDefault,
			PinnedInstall:      s.PinnedInstall,
			SourceProvider:     s.SourceProvider,
			SourceURL:          s.SourceURL,
			SourceAuthor:       s.SourceAuthor,
			SourceName:         s.SourceName,
			SourcePath:         s.SourcePath,
			SourceRef:          s.SourceRef,
			Path:               skillPath,
		})
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	f, err := zw.Create("skillsdex-export.json")
	if err != nil {
		return nil, err
	}
	if _, err := f.Write(manifestBytes); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func buildGitHubExport(ctx context.Context, db *sql.DB, u *User) ([]byte, error) {
	skills, err := listSkills(ctx, db, u.ID, true)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	var readme strings.Builder
	readme.WriteString("# SkillsDex Export\n\n")
	readme.WriteString("This repository is a one-way export from SkillsDex. Edit in SkillsDex, export again, then commit the refreshed files.\n\n")
	readme.WriteString("## Skills\n\n")
	for _, s := range skills {
		files, _, err := currentSkillFiles(ctx, db, u.ID, s.OwnerSlug, s.Slug)
		if err != nil {
			return nil, err
		}
		dir := "skills/" + exportPathPart(s.OwnerSlug) + "/" + exportPathPart(s.Slug)
		skillPath := dir + "/SKILL.md"
		for _, file := range files {
			if err := validateSkillFilePath(file.Path); err != nil {
				return nil, err
			}
			f, err := zw.Create(dir + "/" + file.Path)
			if err != nil {
				return nil, err
			}
			if _, err := f.Write([]byte(file.Content)); err != nil {
				return nil, err
			}
		}
		status := ""
		if s.ArchivedAt != "" {
			status = " (archived)"
		}
		readme.WriteString("- [" + markdownInline(s.OwnerSlug+"/"+s.Slug) + "](" + skillPath + ") - " + markdownInline(s.Name) + status + "\n")
	}
	f, err := zw.Create("README.md")
	if err != nil {
		return nil, err
	}
	if _, err := f.Write([]byte(readme.String())); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func markdownInline(value string) string {
	replacer := strings.NewReplacer("\r", " ", "\n", " ", "[", "\\[", "]", "\\]")
	return replacer.Replace(strings.TrimSpace(value))
}

func exportPathPart(value string) string {
	value = strings.TrimSpace(value)
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(value) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if (r == '-' || r == '_' || r == '.') && b.Len() > 0 {
			b.WriteRune(r)
			lastDash = r == '-'
			continue
		}
		if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-._")
	if out == "" {
		return "skill"
	}
	return out
}

func (a *app) handleHelp(w http.ResponseWriter, r *http.Request) {
	u := webUser(r)
	if r.URL.Path != "/help" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	a.render(w, r, u, "Help", helpPage())
}

func (a *app) handleSkillEdit(w http.ResponseWriter, r *http.Request, u *User, owner, slug string) {
	raw, s, err := rawSkill(r.Context(), a.db, u.ID, owner, slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	in := SkillInput{Owner: owner, Slug: slug, Name: s.Name, Description: s.Description, ShortDescription: s.ShortDescription, RecommendedDefault: s.RecommendedDefault, PinnedInstall: s.PinnedInstall, SourceProvider: s.SourceProvider, SourceURL: s.SourceURL, SourceAuthor: s.SourceAuthor, SourceName: s.SourceName, SourcePath: s.SourcePath, SourceRef: s.SourceRef, Tags: s.Tags, TagsExplicit: true, Body: bodyFromSkillMarkdown(string(raw))}
	currentFiles, _, filesErr := currentSkillFiles(r.Context(), a.db, u.ID, owner, slug)
	if filesErr == nil {
		in.Files = supportingFilesOnly(currentFiles)
	}
	if r.Method == http.MethodGet {
		a.render(w, r, u, "Edit Skill", skillFormPage("/skills/"+owner+"/"+slug+"/edit", false, in, ""))
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	in = skillInputFromForm(r, owner)
	in.Slug = slug
	in.Owner = owner
	if r.FormValue("file_count") == "" && filesErr == nil {
		in.Files = supportingFilesOnly(currentFiles)
	}
	if r.FormValue("action") == "preview" {
		a.render(w, r, u, "Edit Skill", skillFormPage("/skills/"+owner+"/"+slug+"/edit", false, in, ""))
		return
	}
	updated, err := updateSkill(r.Context(), a.db, u, owner, slug, in)
	if err != nil {
		a.render(w, r, u, "Edit Skill", skillFormPage("/skills/"+owner+"/"+slug+"/edit", false, in, err.Error()))
		return
	}
	a.audit(r, u, "skill_updated", "skill", updated.ID, updated.OwnerSlug+"/"+updated.Slug, map[string]any{"revision": updated.RevisionNumber})
	http.Redirect(w, r, "/skills/"+owner+"/"+slug, http.StatusFound)
}

func supportingFilesOnly(files []SkillFile) []SkillFile {
	var out []SkillFile
	for _, file := range files {
		if file.Path != "SKILL.md" {
			out = append(out, file)
		}
	}
	return out
}

func (a *app) handleSkillDetail(w http.ResponseWriter, r *http.Request, u *User, owner, slug string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	files, s, err := currentSkillFiles(r.Context(), a.db, u.ID, owner, slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	raw := ""
	for _, file := range files {
		if file.Path == "SKILL.md" {
			raw = file.Content
			break
		}
	}
	activeSkills, _ := listSkills(r.Context(), a.db, u.ID, false)
	a.render(w, r, u, s.OwnerSlug+"/"+s.Slug, skillDetailPage(s, raw, supportingFilesOnly(files), activeSkills))
}

func (a *app) handleSkillRaw(w http.ResponseWriter, r *http.Request, u *User, owner, slug string) {
	raw, _, err := rawSkill(r.Context(), a.db, u.ID, owner, slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	_, _ = w.Write(raw)
}

func (a *app) handleSkillArchive(w http.ResponseWriter, r *http.Request, u *User, owner, slug string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := archiveSkill(r.Context(), a.db, u.ID, owner, slug); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.audit(r, u, "skill_archived", "skill", "", owner+"/"+slug, nil)
	http.Redirect(w, r, "/skills?archived=1", http.StatusFound)
}

func (a *app) handleSkillRefresh(w http.ResponseWriter, r *http.Request, u *User, owner, slug string) {
	switch r.Method {
	case http.MethodGet:
		s, status, changes, err := refreshImportedSkill(r.Context(), a.db, a.sourceHTTPClient(), u, owner, slug, true)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		a.render(w, r, u, "Refresh Source", skillRefreshPage(s, status, changes))
	case http.MethodPost:
		refreshed, status, _, err := refreshImportedSkill(r.Context(), a.db, a.sourceHTTPClient(), u, owner, slug, false)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		a.audit(r, u, "skill_refreshed", "skill", refreshed.ID, refreshed.OwnerSlug+"/"+refreshed.Slug, map[string]any{"status": status})
		http.Redirect(w, r, "/skills/"+owner+"/"+slug, http.StatusFound)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *app) handleSkillDelete(w http.ResponseWriter, r *http.Request, u *User, owner, slug string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := deleteSkill(r.Context(), a.db, u.ID, owner, slug); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.audit(r, u, "skill_deleted", "skill", "", owner+"/"+slug, nil)
	http.Redirect(w, r, "/skills", http.StatusFound)
}

func (a *app) handleRevisions(w http.ResponseWriter, r *http.Request, u *User, owner, slug string) {
	revs, err := listRevisions(r.Context(), a.db, u.ID, owner, slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	a.render(w, r, u, "Revisions", revisionsPage(owner, slug, revs))
}

func (a *app) handleRevision(w http.ResponseWriter, r *http.Request, u *User, owner, slug, revID string) {
	files, rev, s, err := getRevisionFiles(r.Context(), a.db, u.ID, owner, slug, revID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	prev, err := previousRevisionFiles(r.Context(), a.db, rev)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, r, u, "Revision", revisionPage(s, rev, files, packageDiffs(prev, files)))
}

func skillInputFromForm(r *http.Request, defaultOwner string) SkillInput {
	_ = r.ParseForm()
	owner := strings.TrimSpace(r.FormValue("owner"))
	if owner == "" {
		owner = defaultOwner
	}
	in := SkillInput{
		Owner:              owner,
		Slug:               strings.TrimSpace(r.FormValue("slug")),
		Name:               strings.TrimSpace(r.FormValue("name")),
		Description:        strings.TrimSpace(r.FormValue("description")),
		ShortDescription:   strings.TrimSpace(r.FormValue("short_description")),
		RecommendedDefault: r.FormValue("recommended_default") == "on",
		PinnedInstall:      r.FormValue("pinned_install") == "on",
		SourceProvider:     strings.TrimSpace(r.FormValue("source_provider")),
		SourceURL:          strings.TrimSpace(r.FormValue("source_url")),
		SourceAuthor:       strings.TrimSpace(r.FormValue("source_author")),
		SourceName:         strings.TrimSpace(r.FormValue("source_name")),
		SourcePath:         strings.TrimSpace(r.FormValue("source_path")),
		SourceRef:          strings.TrimSpace(r.FormValue("source_ref")),
		SourceExplicit:     true,
		Tags:               parseTagList(r.FormValue("tags")),
		TagsExplicit:       true,
		Body:               strings.TrimRight(r.FormValue("body"), "\r\n"),
	}
	in.Files = skillFilesFromForm(r)
	return in
}

func skillFilesFromForm(r *http.Request) []SkillFile {
	rawCount := strings.TrimSpace(r.FormValue("file_count"))
	if rawCount == "" {
		return nil
	}
	count, err := strconv.Atoi(rawCount)
	if err != nil || count < 0 {
		return nil
	}
	if count > 100 {
		count = 100
	}
	var files []SkillFile
	for i := 0; i < count; i++ {
		if r.FormValue(fmt.Sprintf("file_delete_%d", i)) == "on" {
			continue
		}
		filePath := strings.TrimSpace(r.FormValue(fmt.Sprintf("file_path_%d", i)))
		content := r.FormValue(fmt.Sprintf("file_content_%d", i))
		if filePath == "" && strings.TrimSpace(content) == "" {
			continue
		}
		files = append(files, SkillFile{
			Path:       filePath,
			Content:    strings.ReplaceAll(content, "\r\n", "\n"),
			Executable: r.FormValue(fmt.Sprintf("file_executable_%d", i)) == "on",
		})
	}
	return files
}

type skillTemplateOption struct {
	Key         string
	Label       string
	Slug        string
	Description string
}

func skillTemplateOptions(owner string) []skillTemplateOption {
	keys := []string{"workflow", "review", "research"}
	options := make([]skillTemplateOption, 0, len(keys))
	for _, key := range keys {
		tmpl := skillTemplate(key, owner)
		options = append(options, skillTemplateOption{
			Key:         key,
			Label:       tmpl.Name,
			Slug:        tmpl.Slug,
			Description: tmpl.Description,
		})
	}
	return options
}

func skillTemplateKeys() []string {
	options := skillTemplateOptions("")
	keys := make([]string, 0, len(options))
	for _, option := range options {
		keys = append(keys, option.Key)
	}
	return keys
}

func skillTemplateKeysText() string {
	return strings.Join(skillTemplateKeys(), ", ")
}

func skillTemplate(name, owner string) SkillInput {
	switch strings.TrimSpace(strings.ToLower(name)) {
	case "workflow":
		return SkillInput{
			Owner:            owner,
			Slug:             "new-workflow",
			Name:             "New Workflow",
			Description:      "Use when a repeatable workflow needs consistent steps and outputs.",
			ShortDescription: "Repeatable workflow with clear outputs",
			Body:             "# New Workflow\n\n## When To Use\n\nUse this skill when the task needs a repeatable process with clear checkpoints.\n\n## Steps\n\n- Confirm the goal and constraints.\n- Inspect the relevant source material before changing anything.\n- Make the smallest coherent change that satisfies the goal.\n- Verify the behavior with the narrowest useful check.\n\n## Output\n\nSummarize what changed, what was verified, and any remaining risk.",
		}
	case "review":
		return SkillInput{
			Owner:            owner,
			Slug:             "focused-review",
			Name:             "Focused Review",
			Description:      "Use when reviewing a change for bugs, regressions, missing tests, or security risks.",
			ShortDescription: "Review changes for concrete risks",
			Body:             "# Focused Review\n\n## Review Order\n\n- Identify the behavior the change is supposed to preserve or add.\n- Look for correctness, security, data-loss, and compatibility risks first.\n- Check whether tests cover the risky paths.\n- Separate confirmed findings from questions and style preferences.\n\n## Output\n\nLead with findings ordered by severity. Include file and line references when possible. Keep summaries brief.",
		}
	case "research":
		return SkillInput{
			Owner:            owner,
			Slug:             "research-brief",
			Name:             "Research Brief",
			Description:      "Use when current facts, product options, or external sources need to be compared before deciding.",
			ShortDescription: "Research current facts and compare options",
			Body:             "# Research Brief\n\n## Research Steps\n\n- Clarify the decision or question the research should answer.\n- Prefer primary sources and recent authoritative references.\n- Compare dates, version numbers, prices, and source quality.\n- Mark assumptions and inferences separately from confirmed facts.\n\n## Output\n\nProvide a concise answer, source links, and the practical recommendation or tradeoff.",
		}
	default:
		return SkillInput{}
	}
}

func bodyFromSkillMarkdown(raw string) string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	if strings.HasPrefix(raw, "---\n") {
		if idx := strings.Index(raw[4:], "\n---\n"); idx >= 0 {
			return strings.TrimLeft(raw[4+idx+5:], "\n")
		}
	}
	return raw
}
