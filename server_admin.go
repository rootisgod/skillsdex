package skillsdex

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (a *app) handleAdminAudit(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/admin/audit" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	events, err := listAuditEvents(r.Context(), a.db, 200)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, r, webUser(r), "Audit Log", auditPage(events))
}

func (a *app) handleAdminMaintenance(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/admin/maintenance" {
		http.NotFound(w, r)
		return
	}
	const defaultAuditDays = 180
	const defaultTokenDays = 90
	const defaultRevisionKeep = 50
	switch r.Method {
	case http.MethodGet:
		stats, err := retentionStats(r.Context(), a.db, defaultAuditDays, defaultTokenDays, defaultRevisionKeep)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		a.render(w, r, webUser(r), "Maintenance", maintenancePage(stats, ""))
	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		auditDays, err := parseRetentionDays(r.FormValue("audit_days"), defaultAuditDays)
		if err != nil {
			stats, _ := retentionStats(r.Context(), a.db, defaultAuditDays, defaultTokenDays, defaultRevisionKeep)
			a.render(w, r, webUser(r), "Maintenance", maintenancePage(stats, err.Error()))
			return
		}
		tokenDays, err := parseRetentionDays(r.FormValue("revoked_token_days"), defaultTokenDays)
		if err != nil {
			stats, _ := retentionStats(r.Context(), a.db, defaultAuditDays, defaultTokenDays, defaultRevisionKeep)
			a.render(w, r, webUser(r), "Maintenance", maintenancePage(stats, err.Error()))
			return
		}
		revisionKeep, err := parseRevisionKeep(r.FormValue("revision_keep"), defaultRevisionKeep)
		if err != nil {
			stats, _ := retentionStats(r.Context(), a.db, defaultAuditDays, defaultTokenDays, defaultRevisionKeep)
			a.render(w, r, webUser(r), "Maintenance", maintenancePage(stats, err.Error()))
			return
		}
		pruned, err := pruneRetention(r.Context(), a.db, auditDays, tokenDays, revisionKeep)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		a.audit(r, webUser(r), "retention_pruned", "maintenance", "", "retention", map[string]any{
			"expired_sessions":   pruned.ExpiredSessions,
			"revoked_api_tokens": pruned.RevokedAPITokens,
			"old_audit_events":   pruned.OldAuditEvents,
			"skill_revisions":    pruned.PrunedRevisions,
			"revision_files":     pruned.PrunedFiles,
			"audit_days":         auditDays,
			"revoked_token_days": tokenDays,
			"revision_keep":      revisionKeep,
		})
		stats, err := retentionStats(r.Context(), a.db, auditDays, tokenDays, revisionKeep)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		msg := fmt.Sprintf("Pruned %d expired session(s), %d revoked API token(s), %d audit event(s), %d old revision(s), and %d revision file(s).", pruned.ExpiredSessions, pruned.RevokedAPITokens, pruned.OldAuditEvents, pruned.PrunedRevisions, pruned.PrunedFiles)
		a.render(w, r, webUser(r), "Maintenance", maintenancePage(stats, msg))
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func parseRevisionKeep(raw string, fallback int) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	keep, err := strconv.Atoi(raw)
	if err != nil || keep < 1 || keep > maxSkillRevisions {
		return 0, fmt.Errorf("revision keep count must be between 1 and %d", maxSkillRevisions)
	}
	return keep, nil
}

func parseRetentionDays(raw string, fallback int) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	days, err := strconv.Atoi(raw)
	if err != nil || days < 1 || days > 3650 {
		return 0, errors.New("retention days must be between 1 and 3650")
	}
	return days, nil
}

func (a *app) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		users, err := listUsers(r.Context(), a.db)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		a.render(w, r, webUser(r), "Users", usersPage(users, ""))
	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		err := createUser(r.Context(), a.db, strings.TrimSpace(r.FormValue("username")), r.FormValue("email"), r.FormValue("password"), r.FormValue("is_admin") == "on")
		if err != nil {
			users, _ := listUsers(r.Context(), a.db)
			a.render(w, r, webUser(r), "Users", usersPage(users, err.Error()))
			return
		}
		a.audit(r, webUser(r), "user_created", "user", "", strings.TrimSpace(r.FormValue("username")), map[string]any{"admin": r.FormValue("is_admin") == "on"})
		http.Redirect(w, r, "/admin/users", http.StatusFound)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *app) handleAdminInvites(w http.ResponseWriter, r *http.Request) {
	u := webUser(r)
	switch r.Method {
	case http.MethodGet:
		invites, err := listSignupInvites(r.Context(), a.db)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		a.render(w, r, u, "Invites", invitesPage(invites, "", ""))
	case http.MethodPost:
		emailTo := strings.TrimSpace(r.FormValue("email"))
		if emailTo != "" {
			if err := validateInviteEmailAddress(emailTo); err != nil {
				invites, _ := listSignupInvites(r.Context(), a.db)
				a.render(w, r, u, "Invites", invitesPage(invites, "", err.Error()))
				return
			}
		}
		result, err := createSignupInvite(r.Context(), a.db, u.ID)
		if err != nil {
			invites, _ := listSignupInvites(r.Context(), a.db)
			a.render(w, r, u, "Invites", invitesPage(invites, "", err.Error()))
			return
		}
		if err := a.finishSignupInviteCreate(r.Context(), r, &result, emailTo); err != nil {
			invites, _ := listSignupInvites(r.Context(), a.db)
			a.render(w, r, u, "Invites", invitesPage(invites, result.URL, err.Error()))
			return
		}
		a.audit(r, u, "signup_invite_created", "invite", result.Invite.ID, result.Invite.TokenPreview, map[string]any{"expires_at": result.Invite.ExpiresAt.Format(time.RFC3339), "email_requested": result.EmailTo != "", "email_sent": result.EmailID != ""})
		invites, err := listSignupInvites(r.Context(), a.db)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		a.render(w, r, u, "Invites", invitesPage(invites, result.URL, signupInviteCreateMessage(result)))
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *app) handleAdminInviteAction(w http.ResponseWriter, r *http.Request) {
	rel := strings.Trim(strings.TrimPrefix(r.URL.Path, "/admin/invites/"), "/")
	parts := strings.Split(rel, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] != "revoke" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	invite, err := revokeSignupInvite(r.Context(), a.db, parts[0])
	if err != nil {
		invites, _ := listSignupInvites(r.Context(), a.db)
		a.render(w, r, webUser(r), "Invites", invitesPage(invites, "", err.Error()))
		return
	}
	a.audit(r, webUser(r), "signup_invite_revoked", "invite", invite.ID, invite.TokenPreview, nil)
	http.Redirect(w, r, "/admin/invites", http.StatusFound)
}

func (a *app) handleAdminUserEdit(w http.ResponseWriter, r *http.Request) {
	rel := strings.Trim(strings.TrimPrefix(r.URL.Path, "/admin/users/"), "/")
	parts := strings.Split(rel, "/")
	if len(parts) == 0 || parts[0] == "" || len(parts) > 2 {
		http.NotFound(w, r)
		return
	}
	id := parts[0]
	u, err := getUserByID(r.Context(), a.db, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if len(parts) == 2 {
		if parts[1] != "delete" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		actor := webUser(r)
		if actor != nil && actor.ID == id {
			a.render(w, r, actor, "Edit User", userEditPage(u, "cannot delete your own account"))
			return
		}
		if err := deleteUser(r.Context(), a.db, id); err != nil {
			a.render(w, r, actor, "Edit User", userEditPage(u, err.Error()))
			return
		}
		a.audit(r, actor, "user_deleted", "user", id, u.Username, nil)
		http.Redirect(w, r, "/admin/users", http.StatusFound)
		return
	}
	if r.Method == http.MethodGet {
		a.render(w, r, webUser(r), "Edit User", userEditPage(u, ""))
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	err = updateUser(r.Context(), a.db, id, r.FormValue("email"), r.FormValue("password"), r.FormValue("is_admin") == "on", r.FormValue("deactivated") == "on")
	if err != nil {
		a.render(w, r, webUser(r), "Edit User", userEditPage(u, err.Error()))
		return
	}
	a.audit(r, webUser(r), "user_updated", "user", u.ID, u.Username, map[string]any{"admin": r.FormValue("is_admin") == "on", "deactivated": r.FormValue("deactivated") == "on", "password_changed": strings.TrimSpace(r.FormValue("password")) != ""})
	http.Redirect(w, r, "/admin/users", http.StatusFound)
}
