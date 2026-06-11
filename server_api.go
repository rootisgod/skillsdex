package skillsdex

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (a *app) handleAPI(w http.ResponseWriter, r *http.Request) {
	u, err := a.apiUser(r)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/"), "/")
	if path == "me" && r.Method == http.MethodGet {
		writeJSON(w, map[string]any{"username": u.Username, "is_admin": u.IsAdmin})
		return
	}
	if path == "skills" && r.Method == http.MethodGet {
		tag := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("tag")))
		if tag != "" {
			if err := validateTagSlug(tag); err != nil {
				writeJSONError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		skills, err := listSkillsFiltered(r.Context(), a.db, u.ID, false, tag)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, map[string]any{"skills": skills})
		return
	}
	if path == "skills" && r.Method == http.MethodPost {
		var in SkillInput
		if err := json.NewDecoder(io.LimitReader(r.Body, maxRequestBodyBytes+1)).Decode(&in); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid json")
			return
		}
		if in.Owner == "" {
			in.Owner = u.Username
		}
		s, err := createSkill(r.Context(), a.db, u, in)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		a.audit(r, u, "api_skill_created", "skill", s.ID, s.OwnerSlug+"/"+s.Slug, map[string]any{"name": s.Name})
		writeJSON(w, map[string]any{"skill": s})
		return
	}
	if path == "public/search" && r.Method == http.MethodGet {
		query := strings.TrimSpace(r.URL.Query().Get("q"))
		limit := queryLimit(r, 20)
		results, err := searchAgentSkills(r.Context(), a.sourceHTTPClient(), query, limit)
		if err != nil {
			writeJSONError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, results)
		return
	}
	if path == "public/top" && r.Method == http.MethodGet {
		results, err := topAgentSkills(r.Context(), a.sourceHTTPClient(), queryLimit(r, 100))
		if err != nil {
			writeJSONError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, results)
		return
	}
	if path == "import" && r.Method == http.MethodPost {
		var req struct {
			Source string `json:"source"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, maxImportedSkillBytes)).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid json")
			return
		}
		results, err := a.importSource(r, u, strings.TrimSpace(req.Source))
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, map[string]any{"results": results})
		return
	}
	if path == "backup" {
		a.handleBackupAPI(w, r, u)
		return
	}
	if path == "export/github" && r.Method == http.MethodGet {
		data, err := buildGitHubExport(r.Context(), a.db, u)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		a.audit(r, u, "api_github_exported", "export", "", u.Username, nil)
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(data)
		return
	}
	if path == "install-sets" && r.Method == http.MethodGet {
		sets, err := listInstallSets(r.Context(), a.db, u.ID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, map[string]any{"sets": sets})
		return
	}
	if strings.HasPrefix(path, "install-sets/") && r.Method == http.MethodGet {
		slug := strings.Trim(strings.TrimPrefix(path, "install-sets/"), "/")
		if slug == "" || strings.Contains(slug, "/") {
			writeJSONError(w, http.StatusNotFound, "not found")
			return
		}
		set, skills, err := listInstallSetSkills(r.Context(), a.db, u.ID, slug)
		if err != nil {
			writeJSONError(w, http.StatusNotFound, "not found")
			return
		}
		writeJSON(w, map[string]any{"set": set, "skills": skills})
		return
	}
	if path == "invites" {
		if !u.IsAdmin {
			writeJSONError(w, http.StatusForbidden, "forbidden")
			return
		}
		switch r.Method {
		case http.MethodGet:
			invites, err := listSignupInvites(r.Context(), a.db)
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, map[string]any{"invites": invites})
		case http.MethodPost:
			var in SignupInviteCreateRequest
			if err := json.NewDecoder(io.LimitReader(r.Body, maxRequestBodyBytes+1)).Decode(&in); err != nil && !errors.Is(err, io.EOF) {
				writeJSONError(w, http.StatusBadRequest, "invalid json")
				return
			}
			if strings.TrimSpace(in.Email) != "" {
				if err := validateInviteEmailAddress(in.Email); err != nil {
					writeJSONError(w, http.StatusBadRequest, err.Error())
					return
				}
			}
			result, err := createSignupInvite(r.Context(), a.db, u.ID)
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if err := a.finishSignupInviteCreate(r.Context(), r, &result, in.Email); err != nil {
				writeJSONError(w, http.StatusBadRequest, err.Error())
				return
			}
			a.audit(r, u, "signup_invite_created", "invite", result.Invite.ID, result.Invite.TokenPreview, map[string]any{"expires_at": result.Invite.ExpiresAt.Format(time.RFC3339), "api": true, "email_requested": result.EmailTo != "", "email_sent": result.EmailID != ""})
			writeJSON(w, result)
		default:
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}
	if strings.HasPrefix(path, "invites/") {
		if !u.IsAdmin {
			writeJSONError(w, http.StatusForbidden, "forbidden")
			return
		}
		rel := strings.Trim(strings.TrimPrefix(path, "invites/"), "/")
		parts := strings.Split(rel, "/")
		if len(parts) == 2 && parts[0] != "" && parts[1] == "revoke" && r.Method == http.MethodPost {
			invite, err := revokeSignupInvite(r.Context(), a.db, parts[0])
			if err != nil {
				writeJSONError(w, http.StatusBadRequest, err.Error())
				return
			}
			a.audit(r, u, "signup_invite_revoked", "invite", invite.ID, invite.TokenPreview, map[string]any{"api": true})
			writeJSON(w, map[string]any{"invite": invite})
			return
		}
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	if path == "plugins" && r.Method == http.MethodGet {
		target := strings.TrimSpace(r.URL.Query().Get("target"))
		if target != "" {
			if err := validateSlug("plugin target", target); err != nil {
				writeJSONError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		plugins, err := listPluginInstalls(r.Context(), a.db, u.ID, target)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, map[string]any{"plugins": plugins})
		return
	}
	if path == "plugins" && r.Method == http.MethodPost {
		var in PluginInstallInput
		if err := json.NewDecoder(io.LimitReader(r.Body, maxRequestBodyBytes+1)).Decode(&in); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid json")
			return
		}
		plugin, err := createPluginInstall(r.Context(), a.db, u.ID, in)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		a.audit(r, u, "api_plugin_tracked", "plugin", plugin.ID, plugin.Slug, map[string]any{"name": plugin.Name, "target": plugin.Target})
		writeJSON(w, map[string]any{"plugin": plugin})
		return
	}
	if strings.HasPrefix(path, "plugins/") && r.Method == http.MethodGet {
		slug := strings.Trim(strings.TrimPrefix(path, "plugins/"), "/")
		if slug == "" || strings.Contains(slug, "/") {
			writeJSONError(w, http.StatusNotFound, "not found")
			return
		}
		plugin, err := getPluginInstall(r.Context(), a.db, u.ID, slug)
		if err != nil {
			writeJSONError(w, http.StatusNotFound, "not found")
			return
		}
		writeJSON(w, map[string]any{"plugin": plugin})
		return
	}
	if strings.HasPrefix(path, "skills/") {
		a.handleSkillAPI(w, r, u, strings.TrimPrefix(path, "skills/"))
		return
	}
	writeJSONError(w, http.StatusNotFound, "not found")
}

func (a *app) handleBackupAPI(w http.ResponseWriter, r *http.Request, u *User) {
	switch r.Method {
	case http.MethodGet:
		data, err := buildSkillsExport(r.Context(), a.db, u)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		a.audit(r, u, "api_backup_exported", "backup", "", u.Username, nil)
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(data)
	case http.MethodPost:
		data, err := io.ReadAll(io.LimitReader(r.Body, maxSkillsDexExportBytes+1))
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		if len(data) > maxSkillsDexExportBytes {
			writeJSONError(w, http.StatusRequestEntityTooLarge, "backup upload is too large")
			return
		}
		results, err := a.restoreSkillsDexExport(r.Context(), u, data)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		a.audit(r, u, "api_backup_restored", "backup", "", "SkillsDex backup", map[string]any{"results": len(results)})
		writeJSON(w, map[string]any{"results": results})
	default:
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (a *app) apiUser(r *http.Request) (*User, error) {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return nil, errors.New("missing bearer token")
	}
	return userForAPIToken(r.Context(), a.db, strings.TrimSpace(strings.TrimPrefix(auth, "Bearer ")))
}

func queryLimit(r *http.Request, fallback int) int {
	limit := fallback
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}
	if limit <= 0 {
		return fallback
	}
	if limit > 100 {
		return 100
	}
	return limit
}

func truthyQuery(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "y":
		return true
	default:
		return false
	}
}

func (a *app) handleSkillAPI(w http.ResponseWriter, r *http.Request, u *User, rest string) {
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) < 2 {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	owner, slug := parts[0], parts[1]
	if len(parts) == 2 {
		switch r.Method {
		case http.MethodGet:
			files, s, err := currentSkillFiles(r.Context(), a.db, u.ID, owner, slug)
			if err != nil {
				status := http.StatusInternalServerError
				if isNotFound(err) {
					status = http.StatusNotFound
				}
				writeJSONError(w, status, "not found")
				return
			}
			raw := ""
			for _, file := range files {
				if file.Path == "SKILL.md" {
					raw = file.Content
					break
				}
			}
			writeJSON(w, map[string]any{"skill": s, "raw": raw, "files": files})
		case http.MethodPut:
			var in SkillInput
			if err := json.NewDecoder(io.LimitReader(r.Body, maxRequestBodyBytes+1)).Decode(&in); err != nil {
				writeJSONError(w, http.StatusBadRequest, "invalid json")
				return
			}
			in.Owner = owner
			in.Slug = slug
			s, err := updateSkill(r.Context(), a.db, u, owner, slug, in)
			if err != nil {
				writeJSONError(w, http.StatusBadRequest, err.Error())
				return
			}
			a.audit(r, u, "api_skill_updated", "skill", s.ID, s.OwnerSlug+"/"+s.Slug, map[string]any{"revision": s.RevisionNumber})
			writeJSON(w, map[string]any{"skill": s})
		default:
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}
	if len(parts) == 3 && parts[2] == "raw" && r.Method == http.MethodGet {
		raw, _, err := rawSkill(r.Context(), a.db, u.ID, owner, slug)
		if err != nil {
			writeJSONError(w, http.StatusNotFound, "not found")
			return
		}
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		_, _ = w.Write(raw)
		return
	}
	if len(parts) == 3 && parts[2] == "flags" && r.Method == http.MethodPut {
		var in SkillFlagUpdate
		if err := json.NewDecoder(io.LimitReader(r.Body, maxRequestBodyBytes+1)).Decode(&in); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid json")
			return
		}
		if in.RecommendedDefault == nil && in.PinnedInstall == nil {
			writeJSONError(w, http.StatusBadRequest, "at least one flag is required")
			return
		}
		current, err := getSkill(r.Context(), a.db, u.ID, owner, slug)
		if err != nil {
			status := http.StatusInternalServerError
			if isNotFound(err) {
				status = http.StatusNotFound
			}
			writeJSONError(w, status, "not found")
			return
		}
		recommended := current.RecommendedDefault
		pinned := current.PinnedInstall
		if in.RecommendedDefault != nil {
			recommended = *in.RecommendedDefault
		}
		if in.PinnedInstall != nil {
			pinned = *in.PinnedInstall
		}
		updated, err := updateSkillFlags(r.Context(), a.db, u, owner, slug, recommended, pinned)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		a.audit(r, u, "api_skill_flags_updated", "skill", updated.ID, updated.OwnerSlug+"/"+updated.Slug, map[string]any{"recommended_default": recommended, "pinned_install": pinned})
		writeJSON(w, map[string]any{"skill": updated})
		return
	}
	if len(parts) == 3 && parts[2] == "refresh" && r.Method == http.MethodPost {
		dryRun := truthyQuery(r.URL.Query().Get("dry_run"))
		refreshed, status, changes, err := refreshImportedSkill(r.Context(), a.db, a.sourceHTTPClient(), u, owner, slug, dryRun)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		if !dryRun {
			a.audit(r, u, "api_skill_refreshed", "skill", refreshed.ID, refreshed.OwnerSlug+"/"+refreshed.Slug, map[string]any{"status": status})
		}
		writeJSON(w, map[string]any{"skill": refreshed, "status": status, "dry_run": dryRun, "changes": changes})
		return
	}
	if len(parts) == 3 && parts[2] == "archive" && r.Method == http.MethodPost {
		if err := archiveSkill(r.Context(), a.db, u.ID, owner, slug); err != nil {
			writeJSONError(w, http.StatusNotFound, "not found")
			return
		}
		a.audit(r, u, "api_skill_archived", "skill", "", owner+"/"+slug, nil)
		writeJSON(w, map[string]any{"ok": true})
		return
	}
	if len(parts) == 3 && parts[2] == "revisions" && r.Method == http.MethodGet {
		revs, err := listRevisions(r.Context(), a.db, u.ID, owner, slug)
		if err != nil {
			writeJSONError(w, http.StatusNotFound, "not found")
			return
		}
		writeJSON(w, map[string]any{"revisions": revs})
		return
	}
	writeJSONError(w, http.StatusNotFound, "not found")
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
