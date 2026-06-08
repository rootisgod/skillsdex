package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"html/template"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type app struct {
	db         *sql.DB
	httpClient httpDoer
}

var defaultSourceHTTPClient = &http.Client{Timeout: 30 * time.Second}

type ctxKey string

const userKey ctxKey = "user"

func runServe(addr, dbPath string) error {
	if dbPath == "" {
		dir, err := skillsdexDir()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
		dbPath = filepath.Join(dir, "skillsdex.db")
	}
	db, err := openDB(dbPath)
	if err != nil {
		return dbPathError(dbPath, err)
	}
	defer db.Close()

	a := &app{db: db}
	mux := http.NewServeMux()
	a.routes(mux)
	localURL := "http://127.0.0.1" + addr[strings.LastIndex(addr, ":"):]
	log.Printf("skillsdex web/API listening on http://%s", addr)
	log.Printf("open %s", localURL)
	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	return server.ListenAndServe()
}

func (a *app) routes(mux *http.ServeMux) {
	mux.HandleFunc("/static/style.css", a.handleCSS)
	mux.HandleFunc("/login", a.handleLogin)
	mux.HandleFunc("/logout", a.requireWeb(a.handleLogout))
	mux.HandleFunc("/admin/audit", a.requireAdmin(a.handleAdminAudit))
	mux.HandleFunc("/admin/maintenance", a.requireAdmin(a.handleAdminMaintenance))
	mux.HandleFunc("/admin/users", a.requireAdmin(a.handleAdminUsers))
	mux.HandleFunc("/admin/users/", a.requireAdmin(a.handleAdminUserEdit))
	mux.HandleFunc("/tokens", a.requireWeb(a.handleTokens))
	mux.HandleFunc("/tokens/", a.requireWeb(a.handleTokenAction))
	mux.HandleFunc("/sets", a.requireWeb(a.handleSets))
	mux.HandleFunc("/sets/", a.requireWeb(a.handleSetRoute))
	mux.HandleFunc("/skills", a.requireWeb(a.handleSkills))
	mux.HandleFunc("/skills/", a.requireWeb(a.handleSkillRoute))
	mux.HandleFunc("/setup/password", a.requireWeb(a.handleSetupPassword))
	mux.HandleFunc("/setup", a.requireWeb(a.handleSetup))
	mux.HandleFunc("/help", a.requireWeb(a.handleHelp))
	mux.HandleFunc("/v1/", a.handleAPI)
	mux.HandleFunc("/", a.requireWeb(a.handleHome))
}

func (a *app) sourceHTTPClient() httpDoer {
	if a.httpClient != nil {
		return a.httpClient
	}
	return defaultSourceHTTPClient
}

func (a *app) handleCSS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	_, _ = io.WriteString(w, css)
}

func (a *app) requireWeb(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("skillsdex_session")
		if err != nil || c.Value == "" {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		u, err := userForSession(r.Context(), a.db, c.Value)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		if r.Method == http.MethodPost && isMultipartForm(r) {
			r.Body = http.MaxBytesReader(w, r.Body, maxSkillsDexExportBytes)
		}
		if r.Method == http.MethodPost && !a.validCSRF(r, c.Value) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if u.MustChangePassword && r.URL.Path != "/setup/password" && r.URL.Path != "/logout" {
			http.Redirect(w, r, "/setup/password", http.StatusFound)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userKey, u)))
	}
}

func (a *app) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return a.requireWeb(func(w http.ResponseWriter, r *http.Request) {
		u := webUser(r)
		if !u.IsAdmin {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next(w, r)
	})
}

func (a *app) validCSRF(r *http.Request, sessionToken string) bool {
	expected, err := csrfTokenForSession(r.Context(), a.db, sessionToken)
	if err != nil || expected == "" {
		return false
	}
	if isMultipartForm(r) {
		if err := r.ParseMultipartForm(maxSkillsDexExportBytes); err != nil {
			return false
		}
	} else {
		if err := r.ParseForm(); err != nil {
			return false
		}
	}
	submitted := r.FormValue("csrf_token")
	if submitted == "" {
		submitted = r.Header.Get("X-CSRF-Token")
	}
	if submitted == "" || len(submitted) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(submitted), []byte(expected)) == 1
}

func isMultipartForm(r *http.Request) bool {
	return strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data")
}

func webUser(r *http.Request) *User {
	u, _ := r.Context().Value(userKey).(*User)
	return u
}

func (a *app) handleHome(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/skills", http.StatusFound)
}

type setupStatus struct {
	SkillCount       int
	TokenCount       int
	PinnedCount      int
	RecommendedCount int
}

func (a *app) handleSetup(w http.ResponseWriter, r *http.Request) {
	u := webUser(r)
	if r.URL.Path != "/setup" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	status, err := a.setupStatus(r.Context(), u)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, r, u, "Setup", setupPage(status))
}

func (a *app) handleSetupPassword(w http.ResponseWriter, r *http.Request) {
	u := webUser(r)
	switch r.Method {
	case http.MethodGet:
		a.render(w, r, u, "Change Password", passwordChangePage(""))
	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			a.render(w, r, u, "Change Password", passwordChangePage("Invalid form."))
			return
		}
		current := r.FormValue("current_password")
		next := r.FormValue("new_password")
		if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(current)) != nil {
			a.render(w, r, u, "Change Password", passwordChangePage("Current password is incorrect."))
			return
		}
		if next != r.FormValue("confirm_password") {
			a.render(w, r, u, "Change Password", passwordChangePage("New passwords do not match."))
			return
		}
		if next == "admin" {
			a.render(w, r, u, "Change Password", passwordChangePage("Choose a password other than the default."))
			return
		}
		if err := setUserPassword(r.Context(), a.db, u.ID, next); err != nil {
			a.render(w, r, u, "Change Password", passwordChangePage(err.Error()))
			return
		}
		a.audit(r, u, "password_changed", "user", u.ID, u.Username, map[string]any{"self_service": true})
		http.Redirect(w, r, "/setup", http.StatusFound)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *app) setupStatus(ctx context.Context, u *User) (setupStatus, error) {
	skills, err := listSkills(ctx, a.db, u.ID, false)
	if err != nil {
		return setupStatus{}, err
	}
	tokens, err := listAPITokens(ctx, a.db, u.ID)
	if err != nil {
		return setupStatus{}, err
	}
	var status setupStatus
	for _, s := range skills {
		if s.ArchivedAt != "" {
			continue
		}
		status.SkillCount++
		if s.PinnedInstall {
			status.PinnedCount++
		}
		if s.RecommendedDefault {
			status.RecommendedCount++
		}
	}
	for _, token := range tokens {
		if token.RevokedAt == "" {
			status.TokenCount++
		}
	}
	return status, nil
}

func (a *app) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		a.render(w, r, nil, "Login", loginForm(""))
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		a.render(w, r, nil, "Login", loginForm("Invalid form."))
		return
	}
	u, err := getUserByUsername(r.Context(), a.db, r.FormValue("username"))
	if err != nil || u.DeactivatedAt != nil || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(r.FormValue("password"))) != nil {
		a.audit(r, nil, "login_failed", "user", "", strings.TrimSpace(r.FormValue("username")), nil)
		a.render(w, r, nil, "Login", loginForm("Invalid username or password."))
		return
	}
	token, err := createWebSession(r.Context(), a.db, u.ID)
	if err != nil {
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}
	redirectTo := "/skills"
	if u.Username == "admin" && bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte("admin")) == nil {
		if err := setUserMustChangePassword(r.Context(), a.db, u.ID, true); err != nil {
			http.Error(w, "password setup error", http.StatusInternalServerError)
			return
		}
		redirectTo = "/setup/password"
	}
	http.SetCookie(w, sessionCookie(r, token, 0))
	a.audit(r, u, "login", "user", u.ID, u.Username, map[string]any{"password_change_required": redirectTo == "/setup/password"})
	http.Redirect(w, r, redirectTo, http.StatusFound)
}

func (a *app) handleLogout(w http.ResponseWriter, r *http.Request) {
	u := webUser(r)
	if c, err := r.Cookie("skillsdex_session"); err == nil {
		_ = deleteWebSession(r.Context(), a.db, c.Value)
	}
	http.SetCookie(w, sessionCookie(r, "", -1))
	a.audit(r, u, "logout", "user", u.ID, u.Username, nil)
	http.Redirect(w, r, "/login", http.StatusFound)
}

func (a *app) audit(r *http.Request, actor *User, action, targetType, targetID, targetLabel string, details map[string]any) {
	detailJSON := "{}"
	if len(details) > 0 {
		if raw, err := json.Marshal(details); err == nil {
			detailJSON = string(raw)
		}
	}
	event := AuditEvent{
		Action:      action,
		TargetType:  targetType,
		TargetID:    targetID,
		TargetLabel: targetLabel,
		DetailJSON:  detailJSON,
		RemoteAddr:  remoteAddr(r),
		UserAgent:   r.UserAgent(),
	}
	if actor != nil {
		event.ActorUserID = actor.ID
		event.ActorUsername = actor.Username
	}
	if err := createAuditEvent(r.Context(), a.db, event); err != nil {
		log.Printf("audit event failed: %v", err)
	}
}

func remoteAddr(r *http.Request) string {
	host := r.RemoteAddr
	if strings.TrimSpace(host) == "" {
		return ""
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}

func sessionCookie(r *http.Request, value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     "skillsdex_session",
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isSecureRequest(r),
	}
}

func isSecureRequest(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

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
	switch r.Method {
	case http.MethodGet:
		stats, err := retentionStats(r.Context(), a.db, defaultAuditDays, defaultTokenDays)
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
			stats, _ := retentionStats(r.Context(), a.db, defaultAuditDays, defaultTokenDays)
			a.render(w, r, webUser(r), "Maintenance", maintenancePage(stats, err.Error()))
			return
		}
		tokenDays, err := parseRetentionDays(r.FormValue("revoked_token_days"), defaultTokenDays)
		if err != nil {
			stats, _ := retentionStats(r.Context(), a.db, defaultAuditDays, defaultTokenDays)
			a.render(w, r, webUser(r), "Maintenance", maintenancePage(stats, err.Error()))
			return
		}
		pruned, err := pruneRetention(r.Context(), a.db, auditDays, tokenDays)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		a.audit(r, webUser(r), "retention_pruned", "maintenance", "", "retention", map[string]any{
			"expired_sessions":   pruned.ExpiredSessions,
			"revoked_api_tokens": pruned.RevokedAPITokens,
			"old_audit_events":   pruned.OldAuditEvents,
			"audit_days":         auditDays,
			"revoked_token_days": tokenDays,
		})
		stats, err := retentionStats(r.Context(), a.db, auditDays, tokenDays)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		msg := fmt.Sprintf("Pruned %d expired session(s), %d revoked API token(s), and %d audit event(s).", pruned.ExpiredSessions, pruned.RevokedAPITokens, pruned.OldAuditEvents)
		a.render(w, r, webUser(r), "Maintenance", maintenancePage(stats, msg))
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
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

func (a *app) handleAdminUserEdit(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/admin/users/"), "/")
	u, err := getUserByID(r.Context(), a.db, id)
	if err != nil {
		http.NotFound(w, r)
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

func (a *app) handleTokens(w http.ResponseWriter, r *http.Request) {
	u := webUser(r)
	switch r.Method {
	case http.MethodGet:
		tokens, err := listAPITokens(r.Context(), a.db, u.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		a.render(w, r, u, "API Tokens", tokensPage(tokens, "", ""))
	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		name := strings.TrimSpace(r.FormValue("name"))
		if name == "" {
			name = "default"
		}
		token, err := createAPIToken(r.Context(), a.db, u.ID, name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		a.audit(r, u, "api_token_created", "api_token", "", name, map[string]any{"preview": tokenPreview(token)})
		tokens, err := listAPITokens(r.Context(), a.db, u.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		a.render(w, r, u, "API Tokens", tokensPage(tokens, "Copy the new token now. It will not be shown again.", token))
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *app) handleTokenAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/revoke") {
		http.NotFound(w, r)
		return
	}
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/tokens/"), "/revoke")
	id = strings.Trim(id, "/")
	if err := revokeAPIToken(r.Context(), a.db, webUser(r).ID, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.audit(r, webUser(r), "api_token_revoked", "api_token", id, id, nil)
	http.Redirect(w, r, "/tokens", http.StatusFound)
}

func (a *app) handleSets(w http.ResponseWriter, r *http.Request) {
	u := webUser(r)
	if r.URL.Path != "/sets" {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		sets, err := listInstallSets(r.Context(), a.db, u.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		a.render(w, r, u, "Install Sets", installSetsPage(sets, ""))
	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		slug := strings.TrimSpace(r.FormValue("slug"))
		name := strings.TrimSpace(r.FormValue("name"))
		description := strings.TrimSpace(r.FormValue("description"))
		set, err := createInstallSet(r.Context(), a.db, u.ID, slug, name, description)
		if err != nil {
			sets, _ := listInstallSets(r.Context(), a.db, u.ID)
			a.render(w, r, u, "Install Sets", installSetsPage(sets, err.Error()))
			return
		}
		a.audit(r, u, "install_set_created", "install_set", set.ID, set.Slug, map[string]any{"name": set.Name})
		http.Redirect(w, r, "/sets/"+set.Slug, http.StatusFound)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *app) handleSetRoute(w http.ResponseWriter, r *http.Request) {
	u := webUser(r)
	rel := strings.Trim(strings.TrimPrefix(r.URL.Path, "/sets/"), "/")
	if rel == "" {
		http.NotFound(w, r)
		return
	}
	parts := strings.Split(rel, "/")
	slug := parts[0]
	if len(parts) == 2 && parts[1] == "delete" {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := deleteInstallSet(r.Context(), a.db, u.ID, slug); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		a.audit(r, u, "install_set_deleted", "install_set", "", slug, nil)
		http.Redirect(w, r, "/sets", http.StatusFound)
		return
	}
	if len(parts) != 1 {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		a.renderInstallSet(w, r, u, slug, "")
	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		set, err := updateInstallSet(r.Context(), a.db, u.ID, slug, r.FormValue("name"), r.FormValue("description"), r.Form["skill_id"])
		if err != nil {
			a.renderInstallSet(w, r, u, slug, err.Error())
			return
		}
		a.audit(r, u, "install_set_updated", "install_set", set.ID, set.Slug, map[string]any{"skill_count": len(r.Form["skill_id"])})
		http.Redirect(w, r, "/sets/"+slug, http.StatusFound)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *app) renderInstallSet(w http.ResponseWriter, r *http.Request, u *User, slug, msg string) {
	set, selectedSkills, err := listInstallSetSkills(r.Context(), a.db, u.ID, slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	allSkills, err := listSkills(r.Context(), a.db, u.ID, false)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	selected := map[string]bool{}
	for _, skill := range selectedSkills {
		selected[skill.ID] = true
	}
	a.render(w, r, u, set.Name, installSetPage(set, allSkills, selected, msg))
}

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
	skills, err := listSkills(r.Context(), a.db, u.ID, r.URL.Query().Get("archived") == "1")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, r, u, "Skills", skillsPage(skills, r.URL.Query().Get("archived") == "1"))
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
	Identifier         string `json:"identifier"`
	Name               string `json:"name"`
	Description        string `json:"description"`
	ShortDescription   string `json:"short_description,omitempty"`
	RevisionNumber     int    `json:"revision_number"`
	Archived           bool   `json:"archived"`
	RecommendedDefault bool   `json:"recommended_default"`
	PinnedInstall      bool   `json:"pinned_install"`
	SourceProvider     string `json:"source_provider,omitempty"`
	SourceURL          string `json:"source_url,omitempty"`
	Path               string `json:"path"`
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
			RevisionNumber:     s.RevisionNumber,
			Archived:           s.ArchivedAt != "",
			RecommendedDefault: s.RecommendedDefault,
			PinnedInstall:      s.PinnedInstall,
			SourceProvider:     s.SourceProvider,
			SourceURL:          s.SourceURL,
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
	in := SkillInput{Owner: owner, Slug: slug, Name: s.Name, Description: s.Description, ShortDescription: s.ShortDescription, RecommendedDefault: s.RecommendedDefault, PinnedInstall: s.PinnedInstall, SourceProvider: s.SourceProvider, SourceURL: s.SourceURL, Body: bodyFromSkillMarkdown(string(raw))}
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
		SourceExplicit:     true,
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

func (a *app) handleAPI(w http.ResponseWriter, r *http.Request) {
	u, err := a.apiUser(r)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/"), "/")
	if path == "me" && r.Method == http.MethodGet {
		writeJSON(w, map[string]any{"username": u.Username, "is_admin": u.IsAdmin})
		return
	}
	if path == "skills" && r.Method == http.MethodGet {
		skills, err := listSkills(r.Context(), a.db, u.ID, false)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, map[string]any{"skills": skills})
		return
	}
	if path == "skills" && r.Method == http.MethodPost {
		var in SkillInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
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
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
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
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
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

func (a *app) render(w http.ResponseWriter, r *http.Request, u *User, title string, body template.HTML) {
	csrfField := template.HTML("")
	if u != nil {
		if csrfToken := a.csrfTokenForRequest(r); csrfToken != "" {
			csrfField = csrfInput(csrfToken)
			body = addCSRFToPostForms(body, csrfToken)
		}
	}
	data := struct {
		Title     string
		User      *User
		Path      string
		CSRFField template.HTML
		Body      template.HTML
	}{Title: title, User: u, Path: r.URL.Path, CSRFField: csrfField, Body: body}
	t := template.Must(template.New("layout").Parse(layoutTmpl))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.Execute(w, data); err != nil {
		log.Printf("render: %v", err)
	}
}

func (a *app) csrfTokenForRequest(r *http.Request) string {
	c, err := r.Cookie("skillsdex_session")
	if err != nil || c.Value == "" {
		return ""
	}
	token, err := csrfTokenForSession(r.Context(), a.db, c.Value)
	if err != nil {
		return ""
	}
	return token
}

func addCSRFToPostForms(body template.HTML, csrfToken string) template.HTML {
	if csrfToken == "" {
		return body
	}
	input := string(csrfInput(csrfToken))
	raw := string(body)
	var out strings.Builder
	for {
		idx := strings.Index(raw, "<form")
		if idx < 0 {
			out.WriteString(raw)
			break
		}
		out.WriteString(raw[:idx])
		raw = raw[idx:]
		end := strings.Index(raw, ">")
		if end < 0 {
			out.WriteString(raw)
			break
		}
		tag := raw[:end+1]
		out.WriteString(tag)
		lower := strings.ToLower(tag)
		if strings.Contains(lower, `method="post"`) || strings.Contains(lower, `method='post'`) || strings.Contains(lower, `method=post`) {
			out.WriteString(input)
		}
		raw = raw[end+1:]
	}
	return template.HTML(out.String())
}

func csrfInput(csrfToken string) template.HTML {
	return template.HTML(`<input type="hidden" name="csrf_token" value="` + html.EscapeString(csrfToken) + `">`)
}

func icon(name string) string {
	icons := map[string]string{
		"archive":  `<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M3 7h18M5 7l1 13h12l1-13M9 11h6M5 4h14v3H5z"/></svg>`,
		"book":     `<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20M4 4.5A2.5 2.5 0 0 1 6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5z"/></svg>`,
		"bolt":     `<svg viewBox="0 0 24 24" aria-hidden="true"><path d="m13 2-9 13h8l-1 7 9-13h-8z"/></svg>`,
		"check":    `<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M20 6 9 17l-5-5"/></svg>`,
		"code":     `<svg viewBox="0 0 24 24" aria-hidden="true"><path d="m8 9-4 3 4 3M16 9l4 3-4 3M14 4l-4 16"/></svg>`,
		"download": `<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 3v12M8 11l4 4 4-4M5 21h14"/></svg>`,
		"edit":     `<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 20h9M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4z"/></svg>`,
		"help":     `<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M9.1 9a3 3 0 1 1 5.8 1c-.8 1.3-2.9 1.4-2.9 4M12 18h.01M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0z"/></svg>`,
		"import":   `<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 3v12M8 11l4 4 4-4M4 17v3h16v-3"/></svg>`,
		"key":      `<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M15 7a5 5 0 1 0-3.5 8.5L14 18h2v2h2v2h3v-3l-6.5-6.5A5 5 0 0 0 15 7z"/></svg>`,
		"layers":   `<svg viewBox="0 0 24 24" aria-hidden="true"><path d="m12 2 9 5-9 5-9-5 9-5zM3 12l9 5 9-5M3 17l9 5 9-5"/></svg>`,
		"logout":   `<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4M16 17l5-5-5-5M21 12H9"/></svg>`,
		"plus":     `<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 5v14M5 12h14"/></svg>`,
		"search":   `<svg viewBox="0 0 24 24" aria-hidden="true"><path d="m21 21-4.3-4.3M10 18a8 8 0 1 1 0-16 8 8 0 0 1 0 16z"/></svg>`,
		"spark":    `<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 3l1.7 5.2L19 10l-5.3 1.8L12 17l-1.7-5.2L5 10l5.3-1.8zM19 15l.8 2.2L22 18l-2.2.8L19 21l-.8-2.2L16 18l2.2-.8z"/></svg>`,
		"sun":      `<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 1v2M12 21v2M4.2 4.2l1.4 1.4M18.4 18.4l1.4 1.4M1 12h2M21 12h2M4.2 19.8l1.4-1.4M18.4 5.6l1.4-1.4M17 12a5 5 0 1 1-10 0 5 5 0 0 1 10 0z"/></svg>`,
		"trash":    `<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M3 6h18M8 6V4h8v2M6 6l1 16h10l1-16M10 11v6M14 11v6"/></svg>`,
		"users":    `<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2M9 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8M22 21v-2a4 4 0 0 0-3-3.9M16 3.1a4 4 0 0 1 0 7.8"/></svg>`,
	}
	if v, ok := icons[name]; ok {
		return `<span class="icon">` + v + `</span>`
	}
	return ""
}

func iconLabel(name, text string) string {
	return icon(name) + `<span>` + html.EscapeString(text) + `</span>`
}

func loginForm(msg string) template.HTML {
	var b strings.Builder
	b.WriteString(`<section class="login-shell"><div class="login-brand"><div class="product-lockup"><div class="mark">SD</div><div><p class="eyebrow">Private skill library</p><h1>SkillsDex</h1></div></div><p>Curate reusable agent skills, keep revisions visible, and install the right guidance into Codex from one local control room.</p><div class="login-graphic" aria-hidden="true"><div class="stack-card primary"><span>admin/code-review</span><strong>r4</strong></div><div class="stack-card"><span>admin/test-strategy</span><strong>r2</strong></div><div class="stack-card muted-card"><span>sync target</span><strong>codex</strong></div></div></div><form class="panel login-panel" method="post" action="/login"><div class="panel-head"><div class="panel-icon">` + icon("key") + `</div><div><h2>Log in</h2><p>Use a local SkillsDex account.</p></div></div>`)
	if msg != "" {
		b.WriteString(`<p class="flash">` + html.EscapeString(msg) + `</p>`)
	}
	b.WriteString(`<label>Username <input name="username" autocomplete="username" required autofocus></label>
		<label>Password <input name="password" type="password" autocomplete="current-password" required></label>
		<button type="submit">` + iconLabel("key", "Log in") + `</button>
	</form></section>`)
	return template.HTML(b.String())
}

func passwordChangePage(msg string) template.HTML {
	var b strings.Builder
	b.WriteString(`<section class="panel narrow"><p class="eyebrow">Setup</p><h1>Change Password</h1><p>Replace the default admin password before continuing.</p>`)
	if msg != "" {
		b.WriteString(`<p class="flash">` + html.EscapeString(msg) + `</p>`)
	}
	b.WriteString(`<form method="post" action="/setup/password">
		<label>Current password <input name="current_password" type="password" autocomplete="current-password" required autofocus></label>
		<label>New password <input name="new_password" type="password" autocomplete="new-password" minlength="12" required></label>
		<label>Confirm password <input name="confirm_password" type="password" autocomplete="new-password" minlength="12" required></label>
		<button type="submit">` + iconLabel("key", "Update Password") + `</button>
	</form></section>`)
	return template.HTML(b.String())
}

func usersPage(users []User, msg string) template.HTML {
	var b strings.Builder
	b.WriteString(`<header class="pagehead"><div><p class="eyebrow">Admin</p><h1>Users</h1><p>Manage local accounts and administrative access.</p></div><div class="page-art users-art" aria-hidden="true">` + icon("users") + `</div></header><div class="split"><section class="surface">`)
	if msg != "" {
		b.WriteString(`<p class="flash">` + html.EscapeString(msg) + `</p>`)
	}
	b.WriteString(`<table><thead><tr><th>Username</th><th>Email</th><th>Role</th><th>Status</th><th></th></tr></thead><tbody>`)
	for _, u := range users {
		status := "active"
		if u.DeactivatedAt != nil {
			status = "deactivated"
		}
		role := "user"
		if u.IsAdmin {
			role = "admin"
		}
		b.WriteString(`<tr><td><div class="cell-title"><strong>` + html.EscapeString(u.Username) + `</strong></div></td><td>` + html.EscapeString(u.Email) + `</td><td><span class="pill">` + role + `</span></td><td><span class="pill ` + status + `">` + status + `</span></td><td><a class="table-action" href="/admin/users/` + html.EscapeString(u.ID) + `">` + iconLabel("edit", "Edit") + `</a></td></tr>`)
	}
	b.WriteString(`</tbody></table></section><section class="panel sticky"><div class="panel-head"><div class="panel-icon">` + icon("users") + `</div><div><h2>Create User</h2><p>Add another operator account.</p></div></div><form method="post" action="/admin/users">
		<label>Username <input name="username" required pattern="[a-z0-9][a-z0-9-]{0,62}"></label>
		<label>Email <input name="email" type="email"></label>
		<label>Password <input name="password" type="password" required></label>
		<label class="check"><input name="is_admin" type="checkbox"> Admin</label>
		<button type="submit">` + iconLabel("plus", "Create") + `</button>
	</form></section></div>`)
	return template.HTML(b.String())
}

func auditPage(events []AuditEvent) template.HTML {
	var b strings.Builder
	b.WriteString(`<header class="pagehead"><div><p class="eyebrow">Admin</p><h1>Audit Log</h1><p>Recent security and library changes.</p></div><div class="page-art users-art" aria-hidden="true">` + icon("archive") + `</div></header><section class="surface data-surface"><table><thead><tr><th>Time</th><th>Actor</th><th>Action</th><th>Target</th><th>Details</th><th>IP</th></tr></thead><tbody>`)
	if len(events) == 0 {
		b.WriteString(`<tr><td colspan="6"><div class="empty">No audit events yet.</div></td></tr>`)
	}
	for _, event := range events {
		actor := event.ActorUsername
		if actor == "" {
			actor = "system"
		}
		target := strings.TrimSpace(event.TargetLabel)
		if target == "" {
			target = strings.TrimSpace(event.TargetType)
		}
		detail := compactAuditDetail(event.DetailJSON)
		b.WriteString(`<tr><td>` + event.CreatedAt.Format("2006-01-02 15:04") + `</td><td>` + html.EscapeString(actor) + `</td><td><span class="pill">` + html.EscapeString(event.Action) + `</span></td><td>` + html.EscapeString(target) + `</td><td><code>` + html.EscapeString(detail) + `</code></td><td>` + html.EscapeString(event.RemoteAddr) + `</td></tr>`)
	}
	b.WriteString(`</tbody></table></section>`)
	return template.HTML(b.String())
}

func maintenancePage(stats RetentionStats, msg string) template.HTML {
	var b strings.Builder
	b.WriteString(`<header class="pagehead"><div><p class="eyebrow">Admin</p><h1>Maintenance</h1><p>Prune old operational records without touching skills, revisions, backups, or users.</p></div><div class="page-art users-art" aria-hidden="true">` + icon("archive") + `</div></header><div class="split"><section class="surface">`)
	if msg != "" {
		b.WriteString(`<p class="flash">` + html.EscapeString(msg) + `</p>`)
	}
	b.WriteString(`<div class="metrics maintenance-metrics">`)
	b.WriteString(`<span><strong>` + fmt.Sprintf("%d", stats.ExpiredSessions) + `</strong> expired sessions</span>`)
	b.WriteString(`<span><strong>` + fmt.Sprintf("%d", stats.RevokedAPITokens) + `</strong> revoked tokens older than ` + fmt.Sprintf("%d", stats.TokenRetentionDays) + ` days</span>`)
	b.WriteString(`<span><strong>` + fmt.Sprintf("%d", stats.OldAuditEvents) + `</strong> audit events older than ` + fmt.Sprintf("%d", stats.AuditRetentionDays) + ` days</span>`)
	b.WriteString(`</div><table><thead><tr><th>Record</th><th>Retention</th><th>Action</th></tr></thead><tbody>`)
	b.WriteString(`<tr><td>Expired web sessions</td><td>Removed when already expired</td><td>Delete session rows only</td></tr>`)
	b.WriteString(`<tr><td>Revoked API tokens</td><td>` + fmt.Sprintf("%d", stats.TokenRetentionDays) + ` days after revocation</td><td>Delete token metadata for already-revoked tokens</td></tr>`)
	b.WriteString(`<tr><td>Audit events</td><td>` + fmt.Sprintf("%d", stats.AuditRetentionDays) + ` days after event time</td><td>Delete old audit rows, then record this prune</td></tr>`)
	b.WriteString(`</tbody></table></section><aside class="panel sticky"><div class="panel-head"><div class="panel-icon">` + icon("trash") + `</div><div><h2>Prune Data</h2><p>Keep the database small while preserving active access and skill history.</p></div></div><form method="post" action="/admin/maintenance">
		<label>Audit event retention days <input name="audit_days" type="number" min="1" max="3650" value="` + fmt.Sprintf("%d", stats.AuditRetentionDays) + `" required></label>
		<label>Revoked token retention days <input name="revoked_token_days" type="number" min="1" max="3650" value="` + fmt.Sprintf("%d", stats.TokenRetentionDays) + `" required></label>
		<button type="submit">` + iconLabel("trash", "Prune") + `</button>
	</form></aside></div>`)
	return template.HTML(b.String())
}

func compactAuditDetail(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" {
		return ""
	}
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return raw
	}
	compact, err := json.Marshal(v)
	if err != nil {
		return raw
	}
	return string(compact)
}

func userEditPage(u *User, msg string) template.HTML {
	var b strings.Builder
	b.WriteString(`<section class="panel narrow"><p class="eyebrow">Admin</p><h1>Edit ` + html.EscapeString(u.Username) + `</h1>`)
	if msg != "" {
		b.WriteString(`<p class="flash">` + html.EscapeString(msg) + `</p>`)
	}
	b.WriteString(`<form method="post">
		<label>Username <input value="` + html.EscapeString(u.Username) + `" disabled></label>
		<label>Email <input name="email" type="email" value="` + html.EscapeString(u.Email) + `"></label>
		<label>New password <input name="password" type="password" autocomplete="new-password"></label>
		<label class="check"><input name="is_admin" type="checkbox"`)
	if u.IsAdmin {
		b.WriteString(` checked`)
	}
	b.WriteString(`> Admin</label><label class="check"><input name="deactivated" type="checkbox"`)
	if u.DeactivatedAt != nil {
		b.WriteString(` checked`)
	}
	b.WriteString(`> Deactivated</label><button type="submit">` + iconLabel("edit", "Save") + `</button></form></section>`)
	return template.HTML(b.String())
}

func tokensPage(tokens []APIToken, msg, newToken string) template.HTML {
	var b strings.Builder
	b.WriteString(`<header class="pagehead"><div><p class="eyebrow">CLI access</p><h1>API Tokens</h1><p>Issue scoped tokens for the local SkillsDex CLI. Tokens are shown once, then stored only as hashes.</p></div><div class="page-art key-art" aria-hidden="true">` + icon("key") + `</div></header><div class="split"><section class="surface">`)
	if msg != "" {
		b.WriteString(`<p class="flash">` + html.EscapeString(msg) + `</p>`)
	}
	if newToken != "" {
		b.WriteString(`<div class="one-time-token"><span>New token</span><code>` + html.EscapeString(newToken) + `</code></div>`)
	}
	b.WriteString(`<table><thead><tr><th>Name</th><th>Preview</th><th>Created</th><th>Last used</th><th>Status</th><th></th></tr></thead><tbody>`)
	if len(tokens) == 0 {
		b.WriteString(`<tr><td colspan="6"><div class="empty">No API tokens yet.</div></td></tr>`)
	}
	for _, t := range tokens {
		status := "active"
		if t.RevokedAt != "" {
			status = "revoked"
		}
		b.WriteString(`<tr><td><div class="cell-title"><strong>` + html.EscapeString(t.Name) + `</strong></div></td><td class="token-cell"><code>` + html.EscapeString(t.TokenPreview) + `</code></td><td>` + t.CreatedAt.Format("2006-01-02 15:04") + `</td><td>` + html.EscapeString(t.LastUsedAt) + `</td><td><span class="pill ` + status + `">` + status + `</span></td><td>`)
		if t.RevokedAt == "" {
			b.WriteString(`<form method="post" action="/tokens/` + html.EscapeString(t.ID) + `/revoke"><button class="link danger" type="submit">Revoke</button></form>`)
		}
		b.WriteString(`</td></tr>`)
	}
	b.WriteString(`</tbody></table></section><aside class="panel sticky"><div class="panel-head"><div class="panel-icon">` + icon("key") + `</div><div><h2>New Token</h2><p>Create credentials for ` + "`skillsdex login`" + `.</p></div></div><form method="post" action="/tokens">
		<label>Name <input name="name" value="local cli"></label><button type="submit">` + iconLabel("plus", "Generate") + `</button>
	</form><div class="command-card"><span>CLI login</span><pre>` + html.EscapeString(tokenStdinLoginCommand()) + `</pre></div></aside></div>`)
	return template.HTML(b.String())
}

func setupPage(status setupStatus) template.HTML {
	var b strings.Builder
	b.WriteString(`<header class="pagehead"><div><p class="eyebrow">First run</p><h1>Setup</h1><p>Walk through the minimum loop: collect useful skills, create CLI credentials, install a set, and verify sync behavior.</p></div><div class="page-art" aria-hidden="true">` + icon("check") + `</div></header>`)
	b.WriteString(`<div class="split wide"><section class="setup-list">`)
	b.WriteString(setupStep("1", "Build a useful library", status.SkillCount > 0, fmt.Sprintf("%d active skill(s)", status.SkillCount), "Create a private skill, import from GitHub, or bring in a public agentskill.sh skill.", "/skills/import", iconLabel("import", "Import Skills")))
	b.WriteString(setupStep("2", "Generate a CLI token", status.TokenCount > 0, fmt.Sprintf("%d active token(s)", status.TokenCount), "Tokens connect the local CLI to this SkillsDex server.", "/tokens", iconLabel("key", "Open Tokens")))
	b.WriteString(setupStep("3", "Choose an install set", status.PinnedCount > 0 || status.RecommendedCount > 0, fmt.Sprintf("%d pinned, %d recommended", status.PinnedCount, status.RecommendedCount), "Mark personal daily-use skills as pinned, use the seeded recommended defaults, or create a named set for a machine or workflow.", "/sets", iconLabel("layers", "Manage Sets")))
	b.WriteString(setupStep("4", "Check local sync", false, "run from a terminal", "Use dry-run before updating a machine, then sync once the plan looks right.", "", ""))
	b.WriteString(`</section><aside class="panel sticky"><div class="panel-head"><div class="panel-icon">` + icon("bolt") + `</div><div><h2>Terminal Flow</h2><p>Run these after creating a token.</p></div></div>`)
	for _, cmd := range []struct {
		label string
		value string
	}{
		{"Login", tokenStdinLoginCommand()},
		{"Configure", "skillsdex config set default_target codex\nskillsdex doctor"},
		{"Inspect", "skillsdex skills\nskillsdex installed --strict"},
		{"Install", "skillsdex install-set laptop\nskillsdex install-pinned\nskillsdex install-defaults"},
		{"Preview updates", "skillsdex sync --dry-run --strict\nskillsdex refresh --all --strict"},
		{"Apply updates", "skillsdex sync --yes"},
		{"Check quality", "skillsdex lint --all --strict"},
		{"Back up", "skillsdex backup export --out ./skillsdex-backup.zip\nskillsdex backup restore --file ./skillsdex-backup.zip --dry-run"},
	} {
		b.WriteString(`<div class="command-card"><span>` + html.EscapeString(cmd.label) + `</span><pre>` + html.EscapeString(cmd.value) + `</pre></div>`)
	}
	b.WriteString(`</aside></div>`)
	return template.HTML(b.String())
}

func setupStep(number, title string, done bool, meta, detail, href, action string) string {
	status := "pending"
	statusLabel := "todo"
	if done {
		status = "active"
		statusLabel = "done"
	}
	var b strings.Builder
	b.WriteString(`<article class="panel setup-step"><div class="setup-step-index">` + html.EscapeString(number) + `</div><div><div class="setup-step-head"><h2>` + html.EscapeString(title) + `</h2><span class="pill ` + status + `">` + statusLabel + `</span></div><p>` + html.EscapeString(detail) + `</p><div class="muted">` + html.EscapeString(meta) + `</div>`)
	if href != "" {
		b.WriteString(`<a class="button secondary" href="` + html.EscapeString(href) + `">` + action + `</a>`)
	}
	b.WriteString(`</div></article>`)
	return b.String()
}

func installSetsPage(sets []InstallSet, msg string) template.HTML {
	var b strings.Builder
	b.WriteString(`<header class="pagehead"><div><p class="eyebrow">Install sets</p><h1>Collections</h1><p>Group skills by workflow, machine, client, or project type and install the group from the CLI.</p></div><div class="page-art" aria-hidden="true">` + icon("layers") + `</div></header><div class="split"><section class="surface data-surface">`)
	if msg != "" {
		b.WriteString(`<p class="flash">` + html.EscapeString(msg) + `</p>`)
	}
	b.WriteString(`<table><thead><tr><th>Set</th><th>Skills</th><th>Updated</th><th></th></tr></thead><tbody>`)
	if len(sets) == 0 {
		b.WriteString(`<tr><td colspan="4"><div class="empty"><strong>No install sets yet.</strong><br>Create one for a machine, workflow, or recurring client.</div></td></tr>`)
	}
	for _, set := range sets {
		b.WriteString(`<tr><td><a class="identifier" href="/sets/` + html.EscapeString(set.Slug) + `">` + html.EscapeString(set.Name) + `</a><div class="muted">` + html.EscapeString(set.Slug) + `</div><div class="muted">` + html.EscapeString(set.Description) + `</div></td><td><span class="pill">` + fmt.Sprintf("%d", set.SkillCount) + `</span></td><td>` + set.UpdatedAt.Format("2006-01-02 15:04") + `</td><td><a class="table-action" href="/sets/` + html.EscapeString(set.Slug) + `">` + iconLabel("edit", "Edit") + `</a></td></tr>`)
	}
	b.WriteString(`</tbody></table></section><aside class="panel sticky"><div class="panel-head"><div class="panel-icon">` + icon("layers") + `</div><div><h2>New Set</h2><p>Create a named install group.</p></div></div><form method="post" action="/sets">
		<label>Slug <input name="slug" required pattern="[a-z0-9][a-z0-9-]{0,62}" placeholder="laptop"></label>
		<label>Name <input name="name" required placeholder="Laptop"></label>
		<label>Description <textarea name="description" rows="3" placeholder="Skills to install on a daily driver."></textarea></label>
		<button type="submit">` + iconLabel("plus", "Create Set") + `</button>
		</form><div class="command-card"><span>Install a set</span><pre>skillsdex config set default_target codex
skillsdex install-set laptop</pre></div></aside></div>`)
	return template.HTML(b.String())
}

func installSetPage(set *InstallSet, skills []Skill, selected map[string]bool, msg string) template.HTML {
	var b strings.Builder
	b.WriteString(`<header class="pagehead"><div><p class="eyebrow">Install set</p><h1>` + html.EscapeString(set.Name) + `</h1><p>` + html.EscapeString(set.Description) + `</p><div class="metrics"><span><strong>` + fmt.Sprintf("%d", set.SkillCount) + `</strong> skills</span><span><strong>` + html.EscapeString(set.Slug) + `</strong> slug</span></div></div><div class="actions"><a class="button secondary" href="/sets">All Sets</a><form method="post" action="/sets/` + html.EscapeString(set.Slug) + `/delete" onsubmit="return confirm('Delete install set ` + html.EscapeString(set.Slug) + `?');"><button class="link danger" type="submit">` + iconLabel("trash", "Delete") + `</button></form></div><div class="page-art" aria-hidden="true">` + icon("layers") + `</div></header>`)
	if msg != "" {
		b.WriteString(`<p class="flash">` + html.EscapeString(msg) + `</p>`)
	}
	b.WriteString(`<div class="split wide"><form class="panel" method="post" action="/sets/` + html.EscapeString(set.Slug) + `">
		<label>Name <input name="name" value="` + html.EscapeString(set.Name) + `" required></label>
		<label>Description <textarea name="description" rows="3">` + html.EscapeString(set.Description) + `</textarea></label>
		<section class="membership-list"><h2>Skills</h2>`)
	if len(skills) == 0 {
		b.WriteString(`<div class="empty compact">No active skills are available.</div>`)
	}
	for _, skill := range skills {
		checked := ""
		if selected[skill.ID] {
			checked = " checked"
		}
		b.WriteString(`<label class="membership-row"><input type="checkbox" name="skill_id" value="` + html.EscapeString(skill.ID) + `"` + checked + `><span><strong>` + html.EscapeString(skill.OwnerSlug+"/"+skill.Slug) + `</strong><em>` + html.EscapeString(skill.ShortDescription) + `</em></span></label>`)
	}
	b.WriteString(`</section><div class="actions"><button type="submit">` + iconLabel("edit", "Save Set") + `</button></div></form><aside class="panel sticky"><div class="panel-head"><div class="panel-icon">` + icon("bolt") + `</div><div><h2>Install Command</h2><p>Use this on any authenticated machine.</p></div></div><div class="command-card"><span>Codex</span><pre>skillsdex config set default_target codex
skillsdex install-set ` + html.EscapeString(set.Slug) + `</pre></div><div class="command-card"><span>Preview managed updates later</span><pre>skillsdex sync --dry-run --strict</pre></div></aside></div>`)
	return template.HTML(b.String())
}

func skillsPage(skills []Skill, archived bool) template.HTML {
	var b strings.Builder
	active := 0
	archivedCount := 0
	for _, s := range skills {
		if s.ArchivedAt != "" {
			archivedCount++
		} else {
			active++
		}
	}
	b.WriteString(`<header class="pagehead hero-head"><div><p class="eyebrow">Library</p><h1>Skills</h1><p>Browse, import, revise, and install reusable agent workflows.</p><div class="metrics"><span><strong>` + fmt.Sprintf("%d", active) + `</strong> active</span><span><strong>` + fmt.Sprintf("%d", archivedCount) + `</strong> archived</span></div></div><div class="actions"><a class="button" href="/skills/new">` + iconLabel("plus", "New Skill") + `</a> `)
	b.WriteString(`<a class="button secondary" href="/skills/import">` + iconLabel("import", "Import") + `</a> `)
	b.WriteString(`<a class="button secondary" href="/skills/top">` + iconLabel("spark", "Top Skills") + `</a> `)
	b.WriteString(`<a class="button secondary" href="/skills/export">` + iconLabel("download", "Export") + `</a> `)
	b.WriteString(`<a class="button secondary" href="/skills/export/github">` + iconLabel("download", "GitHub Export") + `</a> `)
	if archived {
		b.WriteString(`<a class="button ghost" href="/skills">Hide archived</a>`)
	} else {
		b.WriteString(`<a class="button ghost" href="/skills?archived=1">` + iconLabel("archive", "Show archived") + `</a>`)
	}
	b.WriteString(`</div><div class="page-art skills-art" aria-hidden="true"><span class="art-node">SKILL.md</span><span class="art-node">sync</span><span class="art-node">codex</span></div></header><div class="split wide"><section class="surface data-surface"><table><thead><tr><th>Identifier</th><th>Name</th><th>Revision</th><th>Updated</th><th>Status</th></tr></thead><tbody>`)
	if len(skills) == 0 {
		b.WriteString(`<tr><td colspan="5"><div class="empty"><strong>No skills yet.</strong><br><a href="/skills/new">Create the first skill</a></div></td></tr>`)
	}
	for _, s := range skills {
		status := "active"
		if s.ArchivedAt != "" {
			status = "archived"
		}
		id := s.OwnerSlug + "/" + s.Slug
		b.WriteString(`<tr><td><a class="identifier" href="/skills/` + html.EscapeString(id) + `">` + html.EscapeString(id) + `</a></td><td><div class="cell-title"><strong>` + html.EscapeString(s.Name) + `</strong><div class="muted">` + html.EscapeString(s.ShortDescription) + `</div></div>`)
		if s.RecommendedDefault {
			b.WriteString(`<div class="row-tags"><span class="pill active">recommended</span></div>`)
		}
		if s.PinnedInstall {
			b.WriteString(`<div class="row-tags"><span class="pill pinned">pinned</span></div>`)
		}
		if s.SourceProvider != "" {
			b.WriteString(`<div class="row-tags"><span class="pill source">` + html.EscapeString(sourceProviderLabel(s.SourceProvider)) + `</span></div>`)
		}
		b.WriteString(`</td><td><span class="rev">r` + fmt.Sprintf("%d", s.RevisionNumber) + `</span></td><td>` + s.UpdatedAt.Format("2006-01-02 15:04") + `</td><td><span class="pill ` + status + `">` + status + `</span></td></tr>`)
	}
	b.WriteString(`</tbody></table></section><aside class="panel sticky"><div class="panel-head"><div class="panel-icon">` + icon("bolt") + `</div><div><h2>Install Skills</h2><p>Run the CLI from a new machine after login.</p></div></div><div class="command-card"><span>Default target</span><pre>skillsdex config set default_target codex</pre></div><div class="command-card"><span>Named install set</span><pre>skillsdex install-set laptop</pre></div><div class="command-card"><span>Pinned install set</span><pre>skillsdex install-pinned</pre></div><div class="command-card"><span>Recommended defaults</span><pre>skillsdex install-defaults</pre></div><div class="command-card"><span>Single skill</span><pre>skillsdex install admin/five-advisor-review</pre></div><div class="command-card"><span>Sync managed installs</span><pre>skillsdex sync --dry-run --strict</pre></div></aside></div>`)
	return template.HTML(b.String())
}

type skillImportResult struct {
	Identifier string
	Name       string
	Status     string
}

func skillImportPage(source, msg string, results []skillImportResult, searchQuery string, search agentSkillSearchResponse, searchMsg string) template.HTML {
	var b strings.Builder
	b.WriteString(`<header class="pagehead"><div><p class="eyebrow">Import</p><h1>Source</h1><p>Pull a Codex-compatible SKILL.md into your private library.</p></div><div class="page-art import-art" aria-hidden="true">` + icon("import") + `</div></header><div class="split"><section class="panel"><form method="post" action="/skills/import">
		<label>Source <input name="source_url" value="` + html.EscapeString(source) + `" placeholder="https://github.com/DannyMac180/skills or https://agentskill.sh/@openclaw/openclaw-debugging" required autofocus></label>
		<div class="actions"><button type="submit">` + iconLabel("import", "Import") + `</button><a class="button secondary" href="/skills">Cancel</a></div>
	</form>`)
	b.WriteString(`<form method="post" action="/skills/import" enctype="multipart/form-data" class="backup-restore-form">
		<label>Restore Backup <input type="file" name="restore_zip" accept=".zip,application/zip" required></label>
		<div class="actions"><button type="submit">` + iconLabel("import", "Restore") + `</button></div>
	</form>`)
	if msg != "" {
		b.WriteString(`<p class="flash">` + html.EscapeString(msg) + `</p>`)
	}
	if results != nil {
		b.WriteString(`<table class="result-table"><thead><tr><th>Identifier</th><th>Name</th><th>Status</th></tr></thead><tbody>`)
		if len(results) == 0 {
			b.WriteString(`<tr><td colspan="3"><div class="empty">No SKILL.md files found.</div></td></tr>`)
		}
		for _, result := range results {
			id := html.EscapeString(result.Identifier)
			b.WriteString(`<tr><td><a class="identifier" href="/skills/` + id + `">` + id + `</a></td><td><strong>` + html.EscapeString(result.Name) + `</strong></td><td><span class="pill">` + html.EscapeString(result.Status) + `</span></td></tr>`)
		}
		b.WriteString(`</tbody></table>`)
	}
	b.WriteString(`<section class="search-block"><div class="panel-head compact"><div class="panel-icon">` + icon("search") + `</div><div><h2>Search agentskill.sh</h2><p>Find public skills by topic.</p></div></div><form method="get" action="/skills/import" class="inline-search">
		<label>Query <input name="q" value="` + html.EscapeString(searchQuery) + `" placeholder="code review, playwright, seo"></label>
		<button type="submit">` + iconLabel("search", "Search") + `</button>
	</form>`)
	if searchMsg != "" {
		b.WriteString(`<p class="flash">` + html.EscapeString(searchMsg) + `</p>`)
	}
	if searchQuery != "" && searchMsg == "" {
		if len(search.Results) == 0 {
			b.WriteString(`<div class="empty">No agentskill.sh results.</div>`)
		} else {
			b.WriteString(agentSkillResultsTable(search.Results))
		}
	}
	b.WriteString(`</section>`)
	b.WriteString(`</section><aside class="panel sticky"><div class="panel-head"><div class="panel-icon">` + icon("book") + `</div><div><h2>Supported Sources</h2><p>Paste one of these source formats.</p></div></div><div class="command-card"><span>GitHub repository</span><pre>https://github.com/owner/repo</pre></div><div class="command-card"><span>GitHub skill folder</span><pre>https://github.com/owner/repo/tree/main/skill-name</pre></div><div class="command-card"><span>agentskill.sh skill</span><pre>https://agentskill.sh/@owner/skill-name</pre></div><div class="command-card"><span>agentskill.sh /learn</span><pre>npx @agentskill.sh/cli@latest setup</pre></div><p><a href="https://agentskill.sh/" target="_blank" rel="noopener">Browse agentskill.sh</a> · <a href="https://agentskill.sh/install" target="_blank" rel="noopener">Install /learn</a></p></aside></div>`)
	return template.HTML(b.String())
}

func topSkillsPage(search agentSkillSearchResponse, msg string) template.HTML {
	var b strings.Builder
	b.WriteString(`<header class="pagehead"><div><p class="eyebrow">agentskill.sh</p><h1>Top 100 Skills</h1><p>Ranked feed from agentskill.sh. Import saves the selected SKILL.md into your SkillsDex library.</p></div><div class="actions"><a class="button secondary" href="/skills/import">` + iconLabel("import", "Import by URL") + `</a><a class="button secondary" href="https://agentskill.sh/" target="_blank" rel="noopener">Browse agentskill.sh</a></div><div class="page-art top-art" aria-hidden="true">` + icon("spark") + `</div></header><section class="surface data-surface">`)
	if msg != "" {
		b.WriteString(`<p class="flash">` + html.EscapeString(msg) + `</p>`)
	}
	if len(search.Results) == 0 && msg == "" {
		b.WriteString(`<div class="empty">No top skills returned.</div>`)
	} else if len(search.Results) > 0 {
		b.WriteString(agentSkillResultsTable(search.Results))
	}
	b.WriteString(`</section>`)
	return template.HTML(b.String())
}

func helpPage() template.HTML {
	var b strings.Builder
	b.WriteString(`<header class="pagehead"><div><p class="eyebrow">Reference</p><h1>Assistant Formats</h1><p>Choose the right shape for reusable agent guidance: portable skills, repo instructions, or editor-specific rules.</p></div><div class="actions"><a class="button" href="/skills/import">` + iconLabel("import", "Import Skills") + `</a><a class="button secondary" href="/skills/top">` + iconLabel("spark", "Top Skills") + `</a></div><div class="page-art help-art" aria-hidden="true">` + icon("help") + `</div></header>`)
	b.WriteString(`<section class="help-intent-grid" aria-label="Format guidance"><article class="panel help-principle"><div class="panel-icon">` + icon("book") + `</div><span>Most portable</span><strong>Use <code>SKILL.md</code></strong><p>Package repeatable workflows as a skill directory with a clear description and focused instructions.</p></article><article class="panel help-principle"><div class="panel-icon">` + icon("code") + `</div><span>Always visible</span><strong>Use repo guidance</strong><p>Keep broad project policy in files such as <code>AGENTS.md</code> or <code>CLAUDE.md</code>.</p></article><article class="panel help-principle"><div class="panel-icon">` + icon("spark") + `</div><span>File scoped</span><strong>Use editor rules</strong><p>Use Cursor rules when guidance should attach by glob, folder, or explicit mention.</p></article></section>`)
	b.WriteString(`<section class="help-cards" aria-label="Tool format comparison">`)
	rows := []struct {
		tool       string
		summary    string
		paths      string
		format     string
		activation string
		best       string
		sourceURL  string
		source     string
	}{
		{
			tool:       "Claude Code",
			summary:    "Skills handle reusable procedures; `CLAUDE.md` stays useful for broad project memory.",
			paths:      "`~/.claude/skills/<name>/SKILL.md`, `.claude/skills/<name>/SKILL.md`, or plugin skills.",
			format:     "`SKILL.md` with YAML frontmatter and Markdown. `description` is the key discovery field; optional scripts, references, examples, and templates can sit beside it.",
			activation: "Claude can load a skill when the description matches the task, or you can invoke it with `/<skill-name>`. `SKILL.md` edits are detected live in watched skill folders.",
			best:       "Repeatable commands, generated artifacts, and multi-step workflows that should not live permanently in every prompt.",
			sourceURL:  "https://code.claude.com/docs/en/skills",
			source:     "Claude docs",
		},
		{
			tool:       "Codex",
			summary:    "Agent Skills are Codex's reusable workflow format; plugins are the distribution unit.",
			paths:      "Native Codex locations include repo `.agents/skills`, user `$HOME/.agents/skills`, and admin `/etc/codex/skills`. SkillsDex currently writes managed Codex installs to `~/.codex/skills/<owner>--<slug>/SKILL.md`.",
			format:     "A skill directory with required `SKILL.md`, `name`, and `description`, plus optional `scripts/`, `references/`, `assets/`, and `agents/`.",
			activation: "Codex can activate skills by explicit mention or by matching the task to the skill description. If an installed update does not appear, restart Codex.",
			best:       "Portable workflows for Codex CLI, IDE, and app usage, especially when SkillsDex should install or sync them across machines.",
			sourceURL:  "https://developers.openai.com/codex/skills",
			source:     "Codex docs",
		},
		{
			tool:       "Cursor",
			summary:    "Rules provide persistent context for Cursor Agent and Inline Edit; `AGENTS.md` is the simpler Markdown alternative.",
			paths:      "Project rules live in `.cursor/rules`. Nested `.cursor/rules` folders scope rules to subdirectories.",
			format:     "Project rules are `.mdc` files with metadata and Markdown. Common fields include `description`, `globs`, and `alwaysApply`.",
			activation: "Rules can be Always included, Auto Attached by glob, Agent Requested by description, or Manual through `@ruleName`.",
			best:       "Codebase conventions, architecture decisions, and file-scoped guidance that should stay versioned with a repo.",
			sourceURL:  "https://docs.cursor.com/en/context",
			source:     "Cursor docs",
		},
	}
	for _, row := range rows {
		b.WriteString(`<article class="panel help-card"><div class="help-card-head"><h2>` + html.EscapeString(row.tool) + `</h2><a href="` + html.EscapeString(row.sourceURL) + `" target="_blank" rel="noopener">` + html.EscapeString(row.source) + `</a></div><p class="help-summary">` + formatHelpText(row.summary) + `</p><dl class="help-facts"><div><dt>Where it lives</dt><dd>` + formatHelpText(row.paths) + `</dd></div><div><dt>Format</dt><dd>` + formatHelpText(row.format) + `</dd></div><div><dt>Activation</dt><dd>` + formatHelpText(row.activation) + `</dd></div><div><dt>Best fit</dt><dd>` + formatHelpText(row.best) + `</dd></div></dl></article>`)
	}
	b.WriteString(`</section>`)
	b.WriteString(`<div class="split help-grid"><section class="panel"><h2>Portability checklist</h2><div class="note-list"><p><strong>Lead with trigger language.</strong> Put the task and boundaries in the skill or rule description so the agent can select it reliably.</p><p><strong>Keep the loaded file short.</strong> Link to supporting files for long references, examples, scripts, and templates.</p><p><strong>Separate policy from procedure.</strong> Use skills for actions; use repo guidance or rules for conventions that should always be visible.</p></div></section>`)
	b.WriteString(`<aside class="panel"><h2>Quick paths</h2><div class="path-list"><div class="command-card"><span>Claude project skill</span><pre>.claude/skills/name/SKILL.md</pre></div><div class="command-card"><span>Codex repo skill</span><pre>.agents/skills/name/SKILL.md</pre></div><div class="command-card"><span>SkillsDex Codex install</span><pre>~/.codex/skills/owner--slug/SKILL.md</pre></div><div class="command-card"><span>Cursor project rule</span><pre>.cursor/rules/name.mdc</pre></div><div class="command-card"><span>Simple repo guidance</span><pre>AGENTS.md</pre></div></div></aside></div>`)
	return template.HTML(b.String())
}

func formatHelpText(s string) string {
	parts := strings.Split(s, "`")
	for i, part := range parts {
		escaped := html.EscapeString(part)
		if i%2 == 1 {
			parts[i] = `<code>` + escaped + `</code>`
		} else {
			parts[i] = escaped
		}
	}
	return strings.Join(parts, "")
}

func agentSkillResultsTable(results []agentSkillSearchResult) string {
	var b strings.Builder
	b.WriteString(`<table class="result-table sortable" data-sort-table><thead><tr><th><button class="sort-head" type="button" data-sort="text">Skill</button></th><th><button class="sort-head" type="button" data-sort="number">Quality</button></th><th><button class="sort-head" type="button" data-sort="number">Security</button></th><th><button class="sort-head" type="button" data-sort="number">GitHub Stars</button></th><th></th></tr></thead><tbody>`)
	for _, result := range results {
		slug := html.EscapeString(result.Slug)
		b.WriteString(`<tr><td data-sort-value="` + html.EscapeString(result.Name) + `"><a class="identifier" href="https://agentskill.sh/@` + slug + `" target="_blank" rel="noopener">` + slug + `</a><div class="cell-title"><strong>` + html.EscapeString(result.Name) + `</strong><div class="muted">` + html.EscapeString(result.Description) + `</div></div></td><td data-sort-value="` + scoreSortValue(result.ContentQualityScore) + `">` + scoreLabel(result.ContentQualityScore) + `</td><td data-sort-value="` + scoreSortValue(result.SecurityScore) + `">` + scoreLabel(result.SecurityScore) + `</td><td data-sort-value="` + fmt.Sprintf("%d", result.GitHubStars) + `">` + fmt.Sprintf("%d", result.GitHubStars) + `</td><td><form method="post" action="/skills/import"><input type="hidden" name="source_url" value="@` + slug + `"><button type="submit">` + iconLabel("import", "Import") + `</button></form></td></tr>`)
	}
	b.WriteString(`</tbody></table>`)
	return b.String()
}

func scoreLabel(score *int) string {
	if score == nil {
		return `<span class="muted">n/a</span>`
	}
	status := ""
	if *score >= 70 {
		status = " active"
	} else if *score < 30 {
		status = " revoked"
	}
	return `<span class="pill` + status + `">` + fmt.Sprintf("%d", *score) + `/100</span>`
}

func scoreSortValue(score *int) string {
	if score == nil {
		return "-1"
	}
	return fmt.Sprintf("%d", *score)
}

func skillFormPage(action string, create bool, in SkillInput, msg string) template.HTML {
	var b strings.Builder
	title := "Edit Skill"
	if create {
		title = "New Skill"
	}
	preview := renderSkillMarkdown(in)
	quality := skillQuality(in)
	b.WriteString(`<header class="pagehead"><div><p class="eyebrow">Editor</p><h1>` + title + `</h1><p>Shape the stored SKILL.md frontmatter and body.</p></div><div class="page-art editor-art" aria-hidden="true">` + icon("edit") + `</div></header>`)
	if msg != "" {
		b.WriteString(`<p class="flash">` + html.EscapeString(msg) + `</p>`)
	}
	b.WriteString(`<form method="post" action="` + html.EscapeString(action) + `" class="editor" data-skill-editor><section class="panel">
		<label>Owner <input name="owner" value="` + html.EscapeString(in.Owner) + `" required data-skill-owner`)
	if !create {
		b.WriteString(` readonly`)
	}
	b.WriteString(`></label><label>Slug <input name="slug" value="` + html.EscapeString(in.Slug) + `" required pattern="[a-z0-9][a-z0-9-]{0,62}" data-skill-slug`)
	if !create {
		b.WriteString(` readonly`)
	}
	b.WriteString(`></label><label>Name <input name="name" value="` + html.EscapeString(in.Name) + `" required data-skill-name></label>
		<label>Description <textarea name="description" rows="3" required data-skill-description>` + html.EscapeString(in.Description) + `</textarea></label>
		<label>Short description <input name="short_description" value="` + html.EscapeString(in.ShortDescription) + `" data-skill-short-description></label>
		<div class="form-grid"><label>Source provider <input name="source_provider" value="` + html.EscapeString(in.SourceProvider) + `" placeholder="github or agentskill.sh"></label>
		<label>Source URL <input name="source_url" type="url" value="` + html.EscapeString(in.SourceURL) + `" placeholder="https://github.com/owner/repo/tree/main/skill"></label></div>
		<label class="check"><input name="recommended_default" type="checkbox"`)
	if in.RecommendedDefault {
		b.WriteString(` checked`)
	}
	b.WriteString(`> Recommended default</label>
		<label class="check"><input name="pinned_install" type="checkbox"`)
	if in.PinnedInstall {
		b.WriteString(` checked`)
	}
	b.WriteString(`> Pinned install</label>`)
	if create {
		b.WriteString(`<div class="template-picks"><span>Templates</span>`)
		for _, option := range skillTemplateOptions(in.Owner) {
			b.WriteString(`<a href="/skills/new?template=` + html.EscapeString(option.Key) + `" title="` + html.EscapeString(option.Description) + `">` + html.EscapeString(option.Label) + `</a>`)
		}
		b.WriteString(`</div>`)
	}
	b.WriteString(`
			<label>Markdown body <textarea name="body" rows="20" required data-skill-body>` + html.EscapeString(in.Body) + `</textarea></label><div class="editor-assist" aria-live="polite"><span data-body-count>` + html.EscapeString(editorBodyCount(in.Body)) + `</span><span data-description-count>` + html.EscapeString(editorDescriptionCount(in.Description)) + `</span></div>` + skillFilesEditorHTML(in.Files) + `
			<div class="actions"><button name="action" value="save" type="submit">` + iconLabel("edit", "Save Revision") + `</button><button class="secondary" name="action" value="preview" type="submit">Preview</button></div></section><aside class="panel preview-pane">` + qualityPanelHTML(quality) + `<div class="panel-head"><div class="panel-icon">` + icon("code") + `</div><div><h2>Generated SKILL.md</h2><p>What will be stored for this revision.</p></div></div><pre class="raw" data-skill-preview-raw>` + html.EscapeString(preview) + `</pre><h2>Rendered Preview</h2><div class="rendered mini" data-skill-preview-rendered>` + simpleMarkdownHTML(in.Body) + `</div></aside></form>`)
	return template.HTML(b.String())
}

func editorBodyCount(body string) string {
	return fmt.Sprintf("Body: %d words, %d characters", wordCount(body), len([]rune(body)))
}

func editorDescriptionCount(description string) string {
	return fmt.Sprintf("Description: %d words", wordCount(description))
}

func skillFilesEditorHTML(files []SkillFile) string {
	const blankSlots = 3
	total := len(files) + blankSlots
	var b strings.Builder
	b.WriteString(`<section class="support-editor"><div class="panel-head compact"><div class="panel-icon">` + icon("layers") + `</div><div><h2>Supporting Files</h2><p>Add references, scripts, assets, or templates beside <code>SKILL.md</code>.</p></div></div>`)
	b.WriteString(`<input type="hidden" name="file_count" value="` + fmt.Sprintf("%d", total) + `">`)
	for i := 0; i < total; i++ {
		var file SkillFile
		existing := i < len(files)
		if existing {
			file = files[i]
		}
		title := "New file"
		if existing {
			title = file.Path
		}
		b.WriteString(`<article class="support-file-editor"><div class="support-file-head"><strong>` + html.EscapeString(title) + `</strong>`)
		if existing {
			b.WriteString(`<label class="check compact"><input name="file_delete_` + fmt.Sprintf("%d", i) + `" type="checkbox"> Remove</label>`)
		}
		b.WriteString(`</div><label>Path <input name="file_path_` + fmt.Sprintf("%d", i) + `" value="` + html.EscapeString(file.Path) + `" placeholder="references/guide.md"></label><label>Content <textarea name="file_content_` + fmt.Sprintf("%d", i) + `" rows="5">` + html.EscapeString(file.Content) + `</textarea></label><label class="check"><input name="file_executable_` + fmt.Sprintf("%d", i) + `" type="checkbox"`)
		if file.Executable {
			b.WriteString(` checked`)
		}
		b.WriteString(`> Executable</label></article>`)
	}
	b.WriteString(`</section>`)
	return b.String()
}

func skillDetailPage(s *Skill, raw string, files []SkillFile, activeSkills []Skill) template.HTML {
	id := s.OwnerSlug + "/" + s.Slug
	var b strings.Builder
	b.WriteString(`<header class="pagehead"><div><p class="eyebrow">Skill</p><h1>` + html.EscapeString(id) + `</h1><p>` + html.EscapeString(s.Description) + `</p><div class="metrics"><span><strong>r` + fmt.Sprintf("%d", s.RevisionNumber) + `</strong> latest</span><span><strong>` + s.UpdatedAt.Format("Jan 2") + `</strong> updated</span></div></div><div class="actions">
			<a class="button" href="/skills/` + html.EscapeString(id) + `/edit">` + iconLabel("edit", "Edit") + `</a>
			<a class="button secondary" href="/skills/` + html.EscapeString(id) + `/raw">` + iconLabel("code", "Raw") + `</a>
			<a class="button secondary" href="/skills/` + html.EscapeString(id) + `/revisions">Revisions</a>`)
	if s.SourceURL != "" {
		b.WriteString(`<a class="button secondary" href="/skills/` + html.EscapeString(id) + `/refresh">` + iconLabel("import", "Refresh Source") + `</a>`)
	}
	b.WriteString(`</div><div class="page-art skill-art" aria-hidden="true">` + icon("book") + `</div></header>`)
	if s.ArchivedAt != "" {
		b.WriteString(`<p class="flash">This skill is archived.</p>`)
	} else {
		b.WriteString(`<form class="inline-danger" method="post" action="/skills/` + html.EscapeString(id) + `/archive"><button class="link danger" type="submit">` + iconLabel("archive", "Archive skill") + `</button></form>`)
	}
	b.WriteString(`<form class="inline-danger" method="post" action="/skills/` + html.EscapeString(id) + `/delete" onsubmit="return confirm('Delete ` + html.EscapeString(id) + ` permanently? This cannot be undone.');"><button class="link danger" type="submit">` + iconLabel("trash", "Delete skill") + `</button></form>`)
	recommended := "no"
	if s.RecommendedDefault {
		recommended = "yes"
	}
	pinned := "no"
	if s.PinnedInstall {
		pinned = "yes"
	}
	source := sourceMetaHTML(s)
	body := bodyFromSkillMarkdown(raw)
	quality := skillQualityWithContext(SkillInput{Name: s.Name, Description: s.Description, ShortDescription: s.ShortDescription, Body: body}, SkillQualityContext{
		CurrentIdentifier: s.OwnerSlug + "/" + s.Slug,
		SourceProvider:    s.SourceProvider,
		SourceURL:         s.SourceURL,
		UpdatedAt:         s.UpdatedAt,
		Skills:            activeSkills,
	})
	b.WriteString(`<section class="panel skill-install-panel"><div class="panel-head"><div class="panel-icon">` + icon("bolt") + `</div><div><h2>Install</h2><p>Copy this skill into the managed Codex target.</p></div></div><div class="command-card"><span>Codex</span><pre>skillsdex config set default_target codex
skillsdex install ` + html.EscapeString(id) + `</pre></div><div class="meta-list detail-meta"><div><span>Name</span><strong>` + html.EscapeString(s.Name) + `</strong></div><div><span>Short description</span><strong>` + html.EscapeString(s.ShortDescription) + `</strong></div>` + source + `<div><span>Pinned install</span><strong>` + pinned + `</strong></div><div><span>Recommended default</span><strong>` + recommended + `</strong></div></div></section><section class="panel quality-section">` + qualityPanelHTML(quality) + `</section><section class="rendered skill-body">` + simpleMarkdownHTML(body) + `</section>`)
	if len(files) > 0 {
		b.WriteString(`<section class="panel"><div class="panel-head compact"><div class="panel-icon">` + icon("layers") + `</div><div><h2>Supporting Files</h2><p>Installed beside <code>SKILL.md</code>.</p></div></div><div class="file-list">`)
		for _, file := range files {
			b.WriteString(filePreviewHTML(file))
		}
		b.WriteString(`</div></section>`)
	}
	return template.HTML(b.String())
}

func skillRefreshPage(s *Skill, status string, changes []SourceRefreshChange) template.HTML {
	id := s.OwnerSlug + "/" + s.Slug
	var b strings.Builder
	b.WriteString(`<header class="pagehead"><div><p class="eyebrow">Source refresh</p><h1>` + html.EscapeString(id) + `</h1><p>Review upstream package changes before saving a new SkillsDex revision.</p></div><div class="page-art skill-art" aria-hidden="true">` + icon("import") + `</div></header>`)
	b.WriteString(`<div class="split"><section class="surface"><div class="metrics"><span><strong>` + html.EscapeString(status) + `</strong> status</span><span><strong>` + fmt.Sprintf("%d", len(changes)) + `</strong> file change(s)</span></div>`)
	if len(changes) == 0 {
		b.WriteString(`<div class="empty compact">No upstream package file changes found.</div>`)
	} else {
		b.WriteString(`<table><thead><tr><th>File</th><th>Status</th></tr></thead><tbody>`)
		for _, change := range changes {
			b.WriteString(`<tr><td><code>` + html.EscapeString(change.Path) + `</code></td><td><span class="pill">` + html.EscapeString(change.Status) + `</span></td></tr>`)
		}
		b.WriteString(`</tbody></table>`)
		b.WriteString(`<div class="package-diffs">`)
		for _, change := range changes {
			if strings.TrimSpace(change.Diff) == "" {
				continue
			}
			b.WriteString(`<article class="package-diff"><div class="file-row"><code>` + html.EscapeString(change.Path) + `</code><span>` + html.EscapeString(change.Status) + `</span></div>` + diffHTML(change.Diff) + `</article>`)
		}
		b.WriteString(`</div>`)
	}
	b.WriteString(`</section><aside class="panel sticky"><div class="panel-head"><div class="panel-icon">` + icon("import") + `</div><div><h2>Apply Refresh</h2><p>Saving creates a normal revision and preserves pinned/default install flags.</p></div></div>`)
	if status == "update available" {
		b.WriteString(`<form method="post" action="/skills/` + html.EscapeString(id) + `/refresh"><button type="submit">` + iconLabel("import", "Apply Source Update") + `</button></form><div class="command-card"><span>After applying</span><pre>skillsdex sync --dry-run --strict</pre></div>`)
	} else {
		b.WriteString(`<div class="empty compact">Nothing to apply.</div>`)
	}
	b.WriteString(`<p><a href="/skills/` + html.EscapeString(id) + `">Back to skill</a></p></aside></div>`)
	return template.HTML(b.String())
}

func filePreviewHTML(file SkillFile) string {
	mode := ""
	if file.Executable {
		mode = " executable"
	}
	size := file.SizeBytes
	if size == 0 {
		size = int64(len([]byte(file.Content)))
	}
	var b strings.Builder
	b.WriteString(`<article class="file-item"><div class="file-row"><code>` + html.EscapeString(file.Path) + `</code><span>` + fmt.Sprintf("%d bytes", size) + html.EscapeString(mode) + `</span></div>`)
	if preview, ok := skillFilePreview(file); ok {
		b.WriteString(`<pre class="file-preview">` + html.EscapeString(preview) + `</pre>`)
	} else {
		b.WriteString(`<p class="muted">Preview unavailable for this file type.</p>`)
	}
	b.WriteString(`</article>`)
	return b.String()
}

func skillFilePreview(file SkillFile) (string, bool) {
	if !isTextSkillFile(file) {
		return "", false
	}
	content := strings.ReplaceAll(file.Content, "\r\n", "\n")
	const limit = 4000
	if len([]byte(content)) > limit {
		return string([]byte(content)[:limit]) + "\n... truncated ...", true
	}
	return content, true
}

func isTextSkillFile(file SkillFile) bool {
	contentType := strings.ToLower(strings.TrimSpace(file.ContentType))
	if strings.HasPrefix(contentType, "text/") || strings.Contains(contentType, "json") || strings.Contains(contentType, "yaml") {
		return true
	}
	switch strings.ToLower(path.Ext(file.Path)) {
	case ".md", ".markdown", ".txt", ".json", ".yaml", ".yml", ".toml", ".sh", ".py", ".js", ".ts", ".css", ".html":
		return true
	default:
		return false
	}
}

func qualityPanelHTML(issues []SkillQualityIssue) string {
	var b strings.Builder
	summary := qualitySummary(issues)
	b.WriteString(`<div class="panel-head quality-head" data-quality-panel><div class="panel-icon">` + icon("check") + `</div><div><h2>Quality Signals</h2><p><span class="pill quality-` + html.EscapeString(summaryClass(summary)) + `" data-quality-summary>` + html.EscapeString(summary) + `</span></p></div></div>`)
	if len(issues) == 0 {
		b.WriteString(`<div class="empty compact" data-quality-empty>No obvious quality issues found.</div><div class="quality-list" data-quality-list hidden></div>`)
		return b.String()
	}
	b.WriteString(`<div class="empty compact" data-quality-empty hidden>No obvious quality issues found.</div><div class="quality-list" data-quality-list>`)
	for _, issue := range issues {
		severity := "info"
		if issue.Severity == "warn" {
			severity = "warn"
		}
		b.WriteString(`<article class="quality-item ` + severity + `"><strong>` + html.EscapeString(issue.Title) + `</strong><p>` + html.EscapeString(issue.Detail) + `</p></article>`)
	}
	b.WriteString(`</div>`)
	return b.String()
}

func summaryClass(summary string) string {
	return strings.ReplaceAll(summary, " ", "-")
}

func sourceMetaHTML(s *Skill) string {
	if s.SourceProvider == "" && s.SourceURL == "" {
		return `<div><span>Source</span><strong>Manual</strong></div>`
	}
	label := sourceProviderLabel(s.SourceProvider)
	if s.SourceURL == "" || !safeHTTPURL(s.SourceURL) {
		return `<div><span>Source</span><strong>` + html.EscapeString(label) + `</strong></div>`
	}
	return `<div><span>Source</span><strong><a href="` + html.EscapeString(s.SourceURL) + `" target="_blank" rel="noopener">` + html.EscapeString(label) + `</a></strong></div>`
}

func sourceProviderLabel(provider string) string {
	switch strings.TrimSpace(provider) {
	case "github":
		return "GitHub"
	case "agentskill.sh":
		return "agentskill.sh"
	case "":
		return "Manual"
	default:
		return provider
	}
}

func safeHTTPURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return u.Scheme == "https" || u.Scheme == "http"
}

func revisionsPage(owner, slug string, revs []SkillRevision) template.HTML {
	id := owner + "/" + slug
	var b strings.Builder
	b.WriteString(`<header class="pagehead"><div><p class="eyebrow">History</p><h1>Revisions for ` + html.EscapeString(id) + `</h1></div><div class="page-art history-art" aria-hidden="true">` + icon("archive") + `</div></header><section class="surface data-surface"><table><thead><tr><th>Revision</th><th>Created</th><th>Hash</th><th></th></tr></thead><tbody>`)
	for _, r := range revs {
		b.WriteString(`<tr><td><span class="rev">r` + fmt.Sprintf("%d", r.RevisionNumber) + `</span></td><td>` + r.CreatedAt.Format("2006-01-02 15:04") + `</td><td><code>` + html.EscapeString(r.TreeHash) + `</code></td><td><a href="/skills/` + html.EscapeString(id) + `/revisions/` + html.EscapeString(r.ID) + `">View diff</a></td></tr>`)
	}
	b.WriteString(`</tbody></table></section>`)
	return template.HTML(b.String())
}

type packageFileDiff struct {
	Path   string
	Status string
	Diff   string
}

func packageDiffs(prev, current []SkillFile) []packageFileDiff {
	prevByPath := map[string]SkillFile{}
	currentByPath := map[string]SkillFile{}
	seen := map[string]bool{}
	var paths []string
	for _, file := range prev {
		prevByPath[file.Path] = file
		if !seen[file.Path] {
			seen[file.Path] = true
			paths = append(paths, file.Path)
		}
	}
	for _, file := range current {
		currentByPath[file.Path] = file
		if !seen[file.Path] {
			seen[file.Path] = true
			paths = append(paths, file.Path)
		}
	}
	sort.Slice(paths, func(i, j int) bool {
		if paths[i] == "SKILL.md" {
			return true
		}
		if paths[j] == "SKILL.md" {
			return false
		}
		return paths[i] < paths[j]
	})
	var diffs []packageFileDiff
	for _, filePath := range paths {
		oldFile, hadOld := prevByPath[filePath]
		newFile, hasNew := currentByPath[filePath]
		status := "modified"
		oldContent := oldFile.Content
		newContent := newFile.Content
		switch {
		case !hadOld && hasNew:
			status = "added"
		case hadOld && !hasNew:
			status = "removed"
		case oldContent == newContent && oldFile.Executable == newFile.Executable:
			continue
		}
		if hadOld && !hasNew {
			newContent = ""
		}
		if !hadOld && hasNew {
			oldContent = ""
		}
		diff := unifiedDiff(oldContent, newContent)
		if hadOld && hasNew && oldContent == newContent && oldFile.Executable != newFile.Executable {
			mode := "non-executable"
			if newFile.Executable {
				mode = "executable"
			}
			diff = "+ mode: " + mode + "\n"
		}
		diffs = append(diffs, packageFileDiff{Path: filePath, Status: status, Diff: diff})
	}
	return diffs
}

func revisionPage(s *Skill, rev SkillRevision, files []SkillFile, diffs []packageFileDiff) template.HTML {
	id := s.OwnerSlug + "/" + s.Slug
	var b strings.Builder
	b.WriteString(`<header class="pagehead"><div><p class="eyebrow">Revision</p><h1>` + html.EscapeString(id) + ` r` + fmt.Sprintf("%d", rev.RevisionNumber) + `</h1></div></header>`)
	b.WriteString(`<section class="panel"><h2>Package Diff</h2>`)
	if len(diffs) == 0 {
		b.WriteString(`<div class="empty compact">No package file changes in this revision.</div>`)
	} else {
		b.WriteString(`<div class="package-diffs">`)
		for _, d := range diffs {
			b.WriteString(`<article class="package-diff"><div class="file-row"><code>` + html.EscapeString(d.Path) + `</code><span>` + html.EscapeString(d.Status) + `</span></div>` + diffHTML(d.Diff) + `</article>`)
		}
		b.WriteString(`</div>`)
	}
	b.WriteString(`</section><section class="panel"><h2>Files at this revision</h2><div class="file-list">`)
	for _, file := range files {
		b.WriteString(filePreviewHTML(file))
	}
	b.WriteString(`</div></section>`)
	return template.HTML(b.String())
}

var layoutTmpl = `<!doctype html>
<html lang="en">
<head>
	<meta charset="utf-8">
	<meta name="viewport" content="width=device-width, initial-scale=1">
	<title>{{.Title}} · SkillsDex</title>
	<script>
	(function() {
		try {
			var saved = localStorage.getItem("skillsdex-theme");
			var theme = saved || (window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light");
			document.documentElement.dataset.theme = theme;
		} catch (e) {}
	})();
	</script>
	<link rel="stylesheet" href="/static/style.css">
</head>
<body>
	<div class="app-shell">
		<nav>
			<a class="brand" href="/skills"><span class="brand-mark">SD</span><span>SkillsDex</span></a>
			{{if .User}}
				<div class="nav-links">
					<a href="/skills" data-nav="/skills">` + iconLabel("book", "Skills") + `</a>
					<a href="/setup" data-nav="/setup">` + iconLabel("check", "Setup") + `</a>
					<a href="/sets" data-nav="/sets">` + iconLabel("layers", "Sets") + `</a>
					<a href="/skills/top" data-nav="/skills/top">` + iconLabel("spark", "Top Skills") + `</a>
					<a href="/help" data-nav="/help">` + iconLabel("help", "Help") + `</a>
				<a href="/tokens" data-nav="/tokens">` + iconLabel("key", "API Tokens") + `</a>
				{{if .User.IsAdmin}}<a href="/admin/users" data-nav="/admin/users">` + iconLabel("users", "Users") + `</a><a href="/admin/audit" data-nav="/admin/audit">` + iconLabel("archive", "Audit") + `</a><a href="/admin/maintenance" data-nav="/admin/maintenance">` + iconLabel("trash", "Maintenance") + `</a>{{end}}
			</div>
			<button class="theme-toggle icon-button" type="button" aria-label="Toggle color theme" data-theme-toggle>` + icon("sun") + `<span class="button-label">Theme</span></button>
			<form method="post" action="/logout">{{.CSRFField}}<button class="link nav-logout" type="submit">` + icon("logout") + `<span class="button-label">Logout {{.User.Username}}</span></button></form>
			{{end}}
		</nav>
		<main>{{.Body}}</main>
	</div>
	<script>
	(function() {
		var button = document.querySelector("[data-theme-toggle]");
		if (!button) return;
		function apply(theme) {
			document.documentElement.dataset.theme = theme;
			var label = button.querySelector(".button-label");
			if (label) label.textContent = theme === "dark" ? "Light" : "Dark";
			button.setAttribute("aria-label", "Switch to " + (theme === "dark" ? "light" : "dark") + " theme");
		}
		var current = document.documentElement.dataset.theme || "light";
		apply(current);
		button.addEventListener("click", function() {
			var next = document.documentElement.dataset.theme === "dark" ? "light" : "dark";
			try { localStorage.setItem("skillsdex-theme", next); } catch (e) {}
			apply(next);
		});
	})();
	(function() {
		var path = window.location.pathname;
		document.querySelectorAll("[data-nav]").forEach(function(link) {
			var target = link.getAttribute("data-nav");
			if (target === "/skills" && path.indexOf("/skills/top") === 0) return;
			if (path === target || (target !== "/skills" && path.indexOf(target + "/") === 0) || (target === "/skills" && path.indexOf("/skills") === 0)) {
				link.setAttribute("aria-current", "page");
			}
		});
	})();
	(function() {
		document.querySelectorAll("[data-sort-table]").forEach(function(table) {
			var tbody = table.querySelector("tbody");
			if (!tbody) return;
			table.querySelectorAll("[data-sort]").forEach(function(button, index) {
				button.addEventListener("click", function() {
					var type = button.getAttribute("data-sort");
					var current = button.getAttribute("aria-sort") || "none";
					var direction = current === "ascending" ? "descending" : "ascending";
					table.querySelectorAll("[data-sort]").forEach(function(other) {
						other.removeAttribute("aria-sort");
					});
					button.setAttribute("aria-sort", direction);
					var rows = Array.prototype.slice.call(tbody.querySelectorAll("tr"));
					rows.sort(function(a, b) {
						var av = sortValue(a.children[index], type);
						var bv = sortValue(b.children[index], type);
						if (type === "number") return direction === "ascending" ? av - bv : bv - av;
						return direction === "ascending" ? av.localeCompare(bv) : bv.localeCompare(av);
					});
					rows.forEach(function(row) { tbody.appendChild(row); });
				});
			});
		});
		function sortValue(cell, type) {
			if (!cell) return type === "number" ? -1 : "";
			var raw = cell.getAttribute("data-sort-value") || cell.textContent || "";
			if (type === "number") return parseFloat(raw) || 0;
			return raw.trim().toLowerCase();
		}
	})();
	(function() {
		document.querySelectorAll("[data-skill-editor]").forEach(function(form) {
			var name = form.querySelector("[data-skill-name]");
			var description = form.querySelector("[data-skill-description]");
			var shortDescription = form.querySelector("[data-skill-short-description]");
			var body = form.querySelector("[data-skill-body]");
			var rawPreview = form.querySelector("[data-skill-preview-raw]");
			var renderedPreview = form.querySelector("[data-skill-preview-rendered]");
			var qualitySummary = form.querySelector("[data-quality-summary]");
			var qualityEmpty = form.querySelector("[data-quality-empty]");
			var qualityList = form.querySelector("[data-quality-list]");
			var bodyCount = form.querySelector("[data-body-count]");
			var descriptionCount = form.querySelector("[data-description-count]");
			if (!body) return;
			function value(el) {
				return el ? el.value || "" : "";
			}
			function wordCount(text) {
				var trimmed = (text || "").trim();
				return trimmed ? trimmed.split(/\s+/).length : 0;
			}
			function yamlQuote(text) {
				return '"' + (text || "").replace(/\\/g, "\\\\").replace(/"/g, "\\\"").replace(/\n/g, "\\n") + '"';
			}
			function renderSkillMarkdown() {
				var out = "---\n";
				out += "name: " + yamlQuote(value(name)) + "\n";
				out += "description: " + yamlQuote(value(description)) + "\n";
				if (value(shortDescription).trim() !== "") {
					out += "metadata:\n";
					out += "  short-description: " + yamlQuote(value(shortDescription)) + "\n";
				}
				out += "---\n\n";
				out += value(body).replace(/\r\n/g, "\n");
				return out;
			}
			function hasTriggerPrefix(text) {
				var lower = (text || "").trim().toLowerCase();
				return ["use when ", "use to ", "use for ", "use before ", "use after ", "use if "].some(function(prefix) {
					return lower.indexOf(prefix) === 0;
				});
			}
			function hasMarkdownH1(text) {
				return (text || "").split("\n").some(function(line) {
					return line.trim().indexOf("# ") === 0;
				});
			}
			function mentionsSensitiveTerm(text) {
				var lower = (text || "").toLowerCase();
				return ["api key", "apikey", "password", "secret", "access token", "bearer token", "private key"].some(function(term) {
					return lower.indexOf(term) >= 0;
				});
			}
			function addIssue(issues, severity, title, detail) {
				issues.push({ severity: severity, title: title, detail: detail });
			}
			function firstWord(text) {
				return (text || "").trim().split(/\s+/)[0] || "";
			}
			function suggestedTriggerDescription() {
				var skillName = value(name).trim() || "this skill";
				var shortDesc = value(shortDescription).trim();
				if (shortDesc !== "") {
					return "Use when " + firstWord(shortDesc).toLowerCase() + shortDesc.slice(firstWord(shortDesc).length) + ".";
				}
				var desc = value(description).trim();
				if (desc !== "" && desc.slice(-1) !== ".") {
					desc += ".";
				}
				if (desc === "") {
					desc = "the task needs " + skillName.toLowerCase() + ".";
				}
				return "Use when " + desc.toLowerCase().replace(/\.$/, "") + ".";
			}
			function localQualityIssues() {
				var issues = [];
				var desc = value(description).trim();
				var shortDesc = value(shortDescription).trim();
				var markdown = value(body).trim();
				if (wordCount(desc) < 6) {
					addIssue(issues, "warn", "Description is too short", "Codex relies heavily on the description to decide when to load a skill.");
				}
				if (desc !== "" && !hasTriggerPrefix(desc)) {
					addIssue(issues, "warn", "Trigger language is unclear", 'Start with a phrase like "Use when..." or "Use to..." so the agent can select it reliably.');
					addIssue(issues, "info", "Suggested trigger rewrite", 'Try: "' + suggestedTriggerDescription() + '"');
				}
				if (shortDesc === "") {
					addIssue(issues, "info", "Short description is missing", "A compact summary makes browse and install decisions easier.");
				}
				if (markdown === "" || wordCount(markdown) < 20) {
					addIssue(issues, "warn", "Instructions are sparse", "Add concrete workflow steps, boundaries, and expected outputs.");
				}
				if (markdown !== "" && !hasMarkdownH1(markdown)) {
					addIssue(issues, "info", "Top-level heading is missing", "A single H1 makes the skill easier to scan in previews and exports.");
				}
				if (markdown.length > 14000) {
					addIssue(issues, "warn", "Loaded context is very large", "Move long references, examples, and templates into supporting files when full skill folders are available.");
				} else if (markdown.length > 8000) {
					addIssue(issues, "info", "Loaded context is getting large", "Keep the always-loaded SKILL.md focused so the agent has room for task context.");
				}
				if (mentionsSensitiveTerm(markdown)) {
					addIssue(issues, "warn", "Possible sensitive material", "Check that the skill does not contain real credentials, tokens, passwords, or private operational secrets.");
				}
				if (markdown.toLowerCase().indexOf("todo") >= 0) {
					addIssue(issues, "info", "TODO text found", "Replace placeholders before installing this as a daily-use skill.");
				}
				return issues;
			}
			function summaryForIssues(issues) {
				if (!issues.length) return "ready";
				return issues.some(function(issue) { return issue.severity === "warn"; }) ? "needs attention" : "review";
			}
			function refreshQuality() {
				if (!qualitySummary || !qualityEmpty || !qualityList) return;
				var issues = localQualityIssues();
				var summary = summaryForIssues(issues);
				qualitySummary.textContent = summary;
				qualitySummary.className = "pill quality-" + summary.replace(/\s+/g, "-");
				qualityList.replaceChildren();
				if (!issues.length) {
					qualityEmpty.hidden = false;
					qualityList.hidden = true;
					return;
				}
				qualityEmpty.hidden = true;
				qualityList.hidden = false;
				issues.forEach(function(issue) {
					var item = document.createElement("article");
					item.className = "quality-item " + (issue.severity === "warn" ? "warn" : "info");
					appendText(item, "strong", issue.title);
					appendText(item, "p", issue.detail);
					qualityList.appendChild(item);
				});
			}
			function appendText(parent, tag, text) {
				var node = document.createElement(tag);
				node.textContent = text;
				parent.appendChild(node);
				return node;
			}
			function refreshRenderedPreview() {
				if (!renderedPreview) return;
				renderedPreview.replaceChildren();
				var list = null;
				var lines = value(body).replace(/\r\n/g, "\n").split("\n");
				lines.forEach(function(line) {
					var trimmed = line.trim();
					if (trimmed === "" || trimmed.indexOf("---") === 0) {
						list = null;
						return;
					}
					if (trimmed.indexOf("### ") === 0) {
						list = null;
						appendText(renderedPreview, "h3", trimmed.slice(4).trim());
						return;
					}
					if (trimmed.indexOf("## ") === 0) {
						list = null;
						appendText(renderedPreview, "h2", trimmed.slice(3).trim());
						return;
					}
					if (trimmed.indexOf("# ") === 0) {
						list = null;
						appendText(renderedPreview, "h1", trimmed.slice(2).trim());
						return;
					}
					if (trimmed.indexOf("- ") === 0) {
						if (!list) {
							list = document.createElement("ul");
							renderedPreview.appendChild(list);
						}
						appendText(list, "li", trimmed.slice(2).trim());
						return;
					}
					list = null;
					appendText(renderedPreview, "p", trimmed);
				});
				if (!renderedPreview.childNodes.length) {
					var empty = document.createElement("p");
					empty.className = "muted";
					empty.textContent = "No Markdown body yet.";
					renderedPreview.appendChild(empty);
				}
			}
			function refresh() {
				if (rawPreview) rawPreview.textContent = renderSkillMarkdown();
				refreshRenderedPreview();
				refreshQuality();
				if (bodyCount) bodyCount.textContent = "Body: " + wordCount(value(body)) + " words, " + Array.from(value(body)).length + " characters";
				if (descriptionCount) descriptionCount.textContent = "Description: " + wordCount(value(description)) + " words";
			}
			[name, description, shortDescription, body].forEach(function(el) {
				if (el) el.addEventListener("input", refresh);
			});
			body.addEventListener("keydown", function(event) {
				if (event.key !== "Tab") return;
				event.preventDefault();
				var start = body.selectionStart || 0;
				var end = body.selectionEnd || 0;
				var text = body.value;
				body.value = text.slice(0, start) + "  " + text.slice(end);
				body.selectionStart = body.selectionEnd = start + 2;
				refresh();
			});
			refresh();
		});
	})();
	</script>
</body>
</html>`

const css = `
:root { color-scheme: light; font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; color: #1f252c; background: #eceff1; --bg: #eceff1; --bg-grid: rgba(44,56,65,.08); --surface: #ffffff; --surface-2: #f7f8f7; --surface-3: #eef3f2; --line: #d6dde2; --text: #1f252c; --muted: #66717a; --accent: #1f6f78; --accent-strong: #174f56; --accent-ink: #0f3439; --clay: #a05a3c; --clay-soft: #fff1ea; --gold: #a06f10; --danger: #a33838; --ok: #237a4d; --warn: #9a6a16; --nav: rgba(255,255,255,.88); --nav-link: #334c57; --nav-hover: #e9f3f4; --row-hover: #f8fbfb; --field: #ffffff; --field-border: #c4cdd4; --soft: #f1f5f4; --soft-border: #dce5e4; --code-bg: #101820; --code-text: #f6f8fa; --shadow: rgba(30,39,46,.10); --shadow-soft: rgba(30,39,46,.05); --focus: rgba(31,111,120,.24); --warning-bg: #fff7e6; --warning-line: #ecd39a; --warning-text: #6f4c0b; --ok-bg: #eef8f2; --ok-line: #cce8d7; }
:root[data-theme="dark"] { color-scheme: dark; color: #e8edf1; background: #101418; --bg: #101418; --bg-grid: rgba(209,220,224,.07); --surface: #171d23; --surface-2: #1e252c; --surface-3: #202c30; --line: #303a43; --text: #e8edf1; --muted: #9da9b3; --accent: #44a7ad; --accent-strong: #86d4d9; --accent-ink: #c9f2f3; --clay: #e49a7b; --clay-soft: #2c211d; --gold: #e0bd62; --danger: #ff8b8b; --ok: #79d49c; --warn: #e6bd6a; --nav: rgba(20,26,31,.90); --nav-link: #c5d7da; --nav-hover: #213137; --row-hover: #1c252b; --field: #10161b; --field-border: #3a4650; --soft: #202a31; --soft-border: #35424b; --code-bg: #090e13; --code-text: #e8eef5; --shadow: rgba(0,0,0,.30); --shadow-soft: rgba(0,0,0,.18); --focus: rgba(68,167,173,.32); --warning-bg: #332716; --warning-line: #6b5224; --warning-text: #f0cf86; --ok-bg: #173025; --ok-line: #2f6048; }
* { box-sizing: border-box; }
body { margin: 0; min-height: 100vh; background: var(--bg); background-image: linear-gradient(var(--bg-grid) 1px, transparent 1px), linear-gradient(90deg, var(--bg-grid) 1px, transparent 1px); background-size: 28px 28px; }
.app-shell { min-height: 100vh; }
nav { min-height: 66px; display: flex; align-items: center; gap: 18px; padding: 0 28px; background: var(--nav); border-bottom: 1px solid var(--line); position: sticky; top: 0; z-index: 10; backdrop-filter: blur(14px); box-shadow: 0 1px 0 rgba(255,255,255,.35); }
nav a { color: var(--nav-link); text-decoration: none; font-weight: 700; }
nav a:hover { color: var(--accent-strong); }
nav form { margin-left: auto; }
.nav-links { display: flex; align-items: center; gap: 6px; }
.nav-links a { display: inline-flex; align-items: center; gap: 7px; min-height: 36px; padding: 8px 10px; border-radius: 7px; }
.nav-links a:hover { background: var(--nav-hover); }
.nav-links a[aria-current="page"] { background: var(--accent-ink); color: #fff; }
:root[data-theme="dark"] .nav-links a[aria-current="page"] { background: var(--accent); color: #071114; }
.brand { color: var(--text); font-size: 18px; display: inline-flex; align-items: center; gap: 10px; margin-right: 6px; }
.brand-mark, .mark { display: inline-grid; place-items: center; width: 36px; height: 36px; border-radius: 8px; background: var(--accent-ink); color: #fff; font-weight: 850; letter-spacing: 0; box-shadow: inset 0 0 0 1px rgba(255,255,255,.16), 0 8px 18px var(--shadow-soft); }
main { max-width: 1260px; margin: 0 auto; padding: 32px; }
h1 { font-size: 32px; line-height: 1.12; margin: 0 0 10px; letter-spacing: 0; }
h2 { font-size: 18px; margin: 0 0 14px; letter-spacing: 0; }
p { line-height: 1.55; }
a { color: var(--accent-strong); }
table { width: 100%; border-collapse: collapse; background: var(--surface); }
th, td { text-align: left; padding: 15px 16px; border-bottom: 1px solid var(--line); vertical-align: middle; }
th { font-size: 11px; text-transform: uppercase; color: var(--muted); background: var(--surface-2); letter-spacing: .04em; }
tr:hover td { background: var(--row-hover); }
tbody tr:last-child td { border-bottom: 0; }
code, pre { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }
pre { white-space: pre-wrap; overflow: auto; }
.surface, .panel, .rendered { background: var(--surface); border: 1px solid var(--line); border-radius: 8px; box-shadow: 0 12px 32px var(--shadow-soft), 0 1px 2px var(--shadow); overflow: hidden; }
.panel { padding: 22px; overflow: visible; }
.data-surface { border-top: 3px solid var(--accent); }
.narrow { max-width: 480px; margin: 42px auto; }
.split { display: grid; grid-template-columns: minmax(0, 1fr) 360px; gap: 22px; align-items: start; }
.split.wide { grid-template-columns: minmax(0, 1fr) 380px; }
.sticky { position: sticky; top: 84px; }
.pagehead { display: flex; justify-content: space-between; gap: 18px; align-items: flex-start; margin-bottom: 24px; padding: 24px; background: color-mix(in srgb, var(--surface) 86%, transparent); border: 1px solid var(--line); border-radius: 8px; box-shadow: 0 10px 28px var(--shadow-soft); overflow: hidden; position: relative; }
.pagehead p { margin: 0; color: var(--muted); max-width: 720px; }
.eyebrow { font-size: 12px; font-weight: 800; text-transform: uppercase; color: var(--accent-strong) !important; letter-spacing: .08em; margin-bottom: 6px !important; }
.metrics { display: flex; gap: 8px; flex-wrap: wrap; margin-top: 12px; }
.metrics span { display: inline-flex; align-items: center; gap: 6px; min-height: 32px; padding: 5px 10px; border-radius: 7px; background: var(--soft); border: 1px solid var(--soft-border); color: var(--muted); font-size: 13px; }
.metrics strong { color: var(--text); }
form { margin: 0; }
label { display: grid; gap: 7px; margin-bottom: 15px; font-weight: 700; color: var(--text); }
.form-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
input, textarea { width: 100%; border: 1px solid var(--field-border); border-radius: 7px; padding: 11px 12px; font: inherit; background: var(--field); color: var(--text); box-shadow: inset 0 1px 0 var(--shadow-soft); }
input:focus, textarea:focus, button:focus-visible, a:focus-visible { outline: 3px solid var(--focus); outline-offset: 2px; border-color: var(--accent); }
textarea { resize: vertical; }
button, .button { display: inline-flex; align-items: center; justify-content: center; gap: 8px; min-height: 38px; border: 1px solid var(--accent-strong); border-radius: 7px; background: var(--accent); color: #fff; padding: 8px 14px; font: inherit; font-weight: 800; text-decoration: none; cursor: pointer; box-shadow: 0 1px 0 rgba(255,255,255,.16) inset; }
button:hover, .button:hover { background: var(--accent-strong); color: #fff; }
.secondary { background: var(--surface); color: var(--accent-strong); }
.secondary:hover { background: var(--nav-hover); color: var(--accent-strong); }
.ghost { background: transparent; color: var(--muted); border-color: var(--line); box-shadow: none; }
.ghost:hover { background: var(--soft); color: var(--text); }
.link { border: 0; background: transparent; color: var(--nav-link); padding: 0; min-height: 0; font-weight: 750; box-shadow: none; }
.link:hover { background: transparent; color: var(--accent-strong); }
.danger { color: var(--danger); }
.check { display: flex; align-items: center; gap: 8px; }
.check input { width: auto; }
.actions { display: flex; gap: 10px; flex-wrap: wrap; }
.theme-toggle { margin-left: auto; min-height: 34px; padding: 6px 10px; background: var(--surface); color: var(--text); border-color: var(--line); }
.theme-toggle:hover { background: var(--nav-hover); color: var(--text); }
.theme-toggle + form { margin-left: 0; }
.nav-logout { display: inline-flex; align-items: center; gap: 7px; }
.flash { background: var(--warning-bg); border: 1px solid var(--warning-line); padding: 11px 13px; border-radius: 6px; color: var(--warning-text); }
.setup-list { display: grid; gap: 14px; }
.setup-step { display: grid; grid-template-columns: 42px minmax(0, 1fr); gap: 14px; }
.setup-step-index { width: 34px; height: 34px; border-radius: 999px; display: inline-grid; place-items: center; background: var(--accent-ink); color: #fff; font-weight: 850; }
.setup-step-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 6px; }
	.setup-step h2, .setup-step p { margin: 0; }
	.setup-step p { color: var(--muted); }
	.setup-step .button { margin-top: 12px; }
	.membership-list { display: grid; gap: 8px; margin: 18px 0; }
	.membership-row { display: grid; grid-template-columns: auto minmax(0, 1fr); align-items: start; gap: 10px; margin: 0; padding: 10px; border: 1px solid var(--soft-border); border-radius: 7px; background: var(--soft); }
	.membership-row input { width: auto; margin-top: 3px; }
	.membership-row span { display: grid; gap: 3px; min-width: 0; }
	.membership-row em { color: var(--muted); font-style: normal; font-size: 13px; overflow-wrap: anywhere; }
	.search-block { margin-top: 24px; padding-top: 20px; border-top: 1px solid var(--line); }
.inline-search { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 10px; align-items: end; margin-bottom: 14px; }
.inline-search label { margin-bottom: 0; }
.help-intent-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 14px; margin-bottom: 22px; }
.help-principle { display: grid; gap: 8px; }
.help-principle span, .help-facts dt { color: var(--muted); font-size: 11px; font-weight: 800; letter-spacing: .06em; text-transform: uppercase; }
.help-principle strong { font-size: 17px; }
.help-principle p { margin: 0; color: var(--muted); }
.help-cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(300px, 1fr)); gap: 16px; }
.help-card { display: grid; gap: 14px; }
.help-card-head { display: flex; align-items: baseline; justify-content: space-between; gap: 12px; }
.help-card-head h2 { margin: 0; }
.help-card-head a { white-space: nowrap; font-size: 13px; font-weight: 800; }
.help-summary { margin: 0; color: var(--muted); }
.help-facts { display: grid; gap: 12px; margin: 0; }
.help-facts div { display: grid; gap: 5px; padding-top: 12px; border-top: 1px solid var(--line); }
.help-facts dd { margin: 0; line-height: 1.45; }
.help-grid { margin-top: 22px; }
.note-list { display: grid; gap: 12px; }
.note-list p { margin: 0; }
.path-list { display: grid; gap: 2px; }
code { background: var(--soft); border: 1px solid var(--soft-border); border-radius: 4px; padding: 1px 4px; }
.sort-head { min-height: 0; padding: 0; border: 0; background: transparent; color: inherit; font: inherit; font-weight: inherit; text-transform: inherit; letter-spacing: inherit; }
.sort-head:hover { background: transparent; color: var(--accent-strong); }
.sort-head::after { content: "sort"; margin-left: 5px; color: var(--muted); font-size: 9px; text-transform: none; }
.sort-head[aria-sort="ascending"]::after { content: "asc"; color: var(--accent-strong); }
.sort-head[aria-sort="descending"]::after { content: "desc"; color: var(--accent-strong); }
.editor { display: grid; grid-template-columns: minmax(0, 1fr) minmax(360px, 500px); gap: 22px; align-items: start; }
.preview-pane { max-height: calc(100vh - 112px); overflow: auto; }
.editor-assist { display: flex; flex-wrap: wrap; gap: 8px; margin: -6px 0 12px; color: var(--muted); font-size: 12px; font-weight: 800; }
.editor-assist span { display: inline-flex; min-height: 26px; align-items: center; padding: 3px 8px; border: 1px solid var(--soft-border); border-radius: 999px; background: var(--soft); }
.template-picks { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; margin: 0 0 15px; padding: 10px; border: 1px solid var(--soft-border); border-radius: 7px; background: var(--soft); }
.template-picks span { color: var(--muted); font-size: 12px; font-weight: 800; text-transform: uppercase; letter-spacing: .06em; }
.template-picks a { font-weight: 800; text-decoration: none; }
.support-editor { display: grid; gap: 12px; margin: 18px 0; padding-top: 18px; border-top: 1px solid var(--line); }
.support-file-editor { display: grid; gap: 10px; padding: 12px; border: 1px solid var(--soft-border); border-radius: 7px; background: var(--soft); }
.support-file-editor label { margin-bottom: 0; }
.support-file-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.support-file-head strong { min-width: 0; overflow-wrap: anywhere; }
.check.compact { margin-bottom: 0; font-size: 13px; color: var(--muted); }
.quality-section { margin-bottom: 18px; }
.quality-head { margin-bottom: 12px; }
.quality-list { display: grid; gap: 10px; margin-bottom: 18px; }
.quality-item { border: 1px solid var(--soft-border); border-left-width: 4px; border-radius: 7px; padding: 11px 12px; background: var(--soft); }
.quality-item.warn { border-left-color: var(--warn); background: var(--warning-bg); }
.quality-item.info { border-left-color: var(--accent); }
.quality-item strong { display: block; margin-bottom: 4px; }
.quality-item p { margin: 0; color: var(--muted); font-size: 13px; }
.quality-ready { color: var(--ok); background: var(--ok-bg); border-color: var(--ok-line); }
.quality-needs-attention { color: var(--warn); background: var(--warning-bg); border-color: var(--warning-line); }
.raw { background: var(--code-bg); color: var(--code-text); border-radius: 6px; padding: 14px; border: 1px solid #263241; }
.rendered { padding: 28px; overflow: visible; line-height: 1.62; }
.rendered h1:first-child { margin-top: 0; }
.mini { padding: 14px; }
.muted { color: var(--muted); font-size: 13px; margin-top: 4px; }
.row-tags { display: flex; gap: 6px; flex-wrap: wrap; margin-top: 8px; }
.identifier { font-weight: 800; text-decoration: none; }
.pill, .rev { display: inline-flex; align-items: center; min-height: 24px; padding: 3px 8px; border-radius: 999px; border: 1px solid var(--soft-border); background: var(--soft); color: var(--text); font-size: 12px; font-weight: 800; }
.pill.active { color: var(--ok); background: var(--ok-bg); border-color: var(--ok-line); }
.pill.pinned { color: var(--gold); background: var(--warning-bg); border-color: var(--warning-line); }
	.pill.source { color: var(--accent-strong); background: var(--surface-3); border-color: var(--soft-border); }
	.pill.archived, .pill.revoked, .pill.deactivated { color: var(--warn); background: var(--warning-bg); border-color: var(--warning-line); }
	.token-cell code { display: block; max-width: 360px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
	.one-time-token { display: grid; gap: 8px; margin: 16px; padding: 14px; border-radius: 7px; border: 1px solid var(--ok-line); background: var(--ok-bg); }
	.one-time-token span { color: var(--ok); font-size: 12px; font-weight: 800; text-transform: uppercase; letter-spacing: .06em; }
	.one-time-token code { display: block; overflow-wrap: anywhere; background: var(--surface); }
	.empty { padding: 32px; text-align: center; color: var(--muted); }
.empty.compact { padding: 12px; text-align: left; border: 1px solid var(--soft-border); border-radius: 7px; background: var(--soft); margin-bottom: 18px; }
.command-card { display: grid; gap: 8px; margin-top: 14px; }
.command-card span { color: var(--muted); font-size: 12px; font-weight: 800; text-transform: uppercase; letter-spacing: .06em; }
.command-card pre { margin: 0; background: var(--code-bg); color: var(--code-text); border-radius: 7px; padding: 13px; border: 1px solid #263241; }
.meta-list { display: grid; gap: 12px; margin-top: 18px; }
.meta-list div { display: grid; gap: 4px; padding-top: 12px; border-top: 1px solid var(--line); }
.meta-list span { color: var(--muted); font-size: 12px; text-transform: uppercase; font-weight: 800; letter-spacing: .06em; }
.skill-install-panel { margin-bottom: 18px; }
.skill-install-panel .command-card { max-width: 620px; }
.detail-meta { grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: 14px; }
.detail-meta div { min-width: 0; }
.detail-meta strong { overflow-wrap: anywhere; }
.skill-body { margin-top: 18px; }
.file-list, .package-diffs { display: grid; gap: 10px; }
.file-item, .package-diff { border: 1px solid var(--soft-border); border-radius: 7px; background: var(--soft); overflow: hidden; }
.file-row { display: flex; align-items: center; justify-content: space-between; gap: 12px; min-height: 38px; padding: 9px 10px; }
.file-row code { min-width: 0; overflow-wrap: anywhere; }
.file-row span { flex: 0 0 auto; color: var(--muted); font-size: 12px; font-weight: 800; text-transform: uppercase; letter-spacing: .04em; }
.file-preview { margin: 0; max-height: 240px; overflow: auto; border-top: 1px solid var(--soft-border); border-radius: 0; padding: 10px; background: var(--surface); color: var(--text); }
.package-diff .diff { border: 0; border-top: 1px solid var(--soft-border); border-radius: 0; margin: 0; }
.icon { display: inline-flex; align-items: center; justify-content: center; width: 17px; height: 17px; flex: 0 0 auto; }
.icon svg { width: 17px; height: 17px; fill: none; stroke: currentColor; stroke-width: 2; stroke-linecap: round; stroke-linejoin: round; }
.panel-head { display: flex; gap: 12px; align-items: flex-start; margin-bottom: 18px; }
.panel-head.compact { margin-bottom: 12px; }
.panel-head h2, .panel-head p { margin: 0; }
.panel-head p { color: var(--muted); font-size: 13px; }
.panel-icon { width: 38px; height: 38px; border-radius: 8px; display: inline-grid; place-items: center; color: var(--accent-strong); background: var(--surface-3); border: 1px solid var(--soft-border); }
.panel-icon .icon, .panel-icon svg { width: 19px; height: 19px; }
.page-art { min-width: 118px; min-height: 86px; border: 1px solid var(--soft-border); border-radius: 8px; background: var(--surface-3); color: var(--accent-strong); display: grid; place-items: center; position: relative; }
.page-art > .icon, .page-art svg { width: 42px; height: 42px; }
.skills-art { display: grid; align-content: center; gap: 8px; padding: 14px; }
.art-node { display: block; min-width: 92px; padding: 7px 9px; border-radius: 6px; background: var(--surface); border: 1px solid var(--line); color: var(--text); font-size: 12px; font-weight: 800; box-shadow: 0 8px 18px var(--shadow-soft); }
.art-node:nth-child(2) { transform: translateX(18px); color: var(--accent-strong); }
.art-node:nth-child(3) { transform: translateX(4px); color: var(--clay); }
.cell-title { display: grid; gap: 3px; }
.table-action { display: inline-flex; align-items: center; gap: 6px; text-decoration: none; font-weight: 800; }
.inline-danger { margin: -8px 0 18px; }
.login-shell { min-height: calc(100vh - 126px); display: grid; grid-template-columns: minmax(0, 1fr) 420px; gap: 34px; align-items: center; max-width: 1040px; margin: 0 auto; }
.product-lockup { display: flex; gap: 14px; align-items: center; margin-bottom: 18px; }
.login-brand h1 { font-size: 48px; margin: 0; }
.login-brand p { color: var(--muted); font-size: 18px; max-width: 520px; }
.login-panel { padding: 26px; border-top: 3px solid var(--accent); }
.mark { width: 52px; height: 52px; border-radius: 10px; font-size: 18px; }
.login-graphic { margin-top: 28px; width: min(460px, 100%); display: grid; gap: 10px; }
.stack-card { display: flex; align-items: center; justify-content: space-between; min-height: 54px; padding: 12px 14px; border-radius: 8px; border: 1px solid var(--line); background: var(--surface); box-shadow: 0 10px 22px var(--shadow-soft); font-weight: 800; }
.stack-card span { color: var(--muted); }
.stack-card strong { color: var(--accent-strong); }
.stack-card.primary { border-left: 4px solid var(--accent); transform: translateX(18px); }
.stack-card.muted-card { border-left: 4px solid var(--clay); transform: translateX(34px); }
.diff { background: var(--surface); border: 1px solid var(--line); border-radius: 6px; padding: 0; }
.diff span { display: block; padding: 2px 8px; }
.diff .add { background: #e7f6e7; color: #14532d; }
.diff .del { background: #fde8e8; color: #842029; }
.diff .ctx { color: #4b5563; }
@media (max-width: 820px) {
	main { padding: 18px; }
	.split, .split.wide, .editor, .login-shell { grid-template-columns: 1fr; }
	.help-intent-grid, .help-cards { grid-template-columns: 1fr; }
	.form-grid { grid-template-columns: 1fr; gap: 0; }
	.inline-search { grid-template-columns: 1fr; }
	.pagehead { display: block; padding: 20px; }
	.page-art { display: none; }
	.sticky, .preview-pane { position: static; max-height: none; }
	nav { padding: 0 16px; gap: 10px; overflow-x: auto; align-items: center; }
	.nav-links { gap: 2px; }
	.brand span:last-child { display: none; }
	.nav-links a span:not(.icon), .theme-toggle .button-label, .nav-logout .button-label { display: none; }
	.surface { overflow: visible; }
	table, thead, tbody, tr, th, td { display: block; width: 100%; }
	thead { display: none; }
	tr { padding: 14px 16px; border-bottom: 1px solid var(--line); }
	tr:last-child { border-bottom: 0; }
	td { padding: 4px 0; border-bottom: 0; }
	tr:hover td { background: transparent; }
	td:empty { display: none; }
	td form { margin-top: 8px; }
	.token-cell code { max-width: 220px; }
	.detail-meta { grid-template-columns: 1fr; }
	h1 { font-size: 26px; }
	.login-brand h1 { font-size: 38px; }
	.stack-card.primary, .stack-card.muted-card { transform: none; }
}
`
