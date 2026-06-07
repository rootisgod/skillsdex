package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

type app struct {
	db *sql.DB
}

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
	return http.ListenAndServe(addr, mux)
}

func (a *app) routes(mux *http.ServeMux) {
	mux.HandleFunc("/static/style.css", a.handleCSS)
	mux.HandleFunc("/login", a.handleLogin)
	mux.HandleFunc("/logout", a.requireWeb(a.handleLogout))
	mux.HandleFunc("/admin/users", a.requireAdmin(a.handleAdminUsers))
	mux.HandleFunc("/admin/users/", a.requireAdmin(a.handleAdminUserEdit))
	mux.HandleFunc("/tokens", a.requireWeb(a.handleTokens))
	mux.HandleFunc("/tokens/", a.requireWeb(a.handleTokenAction))
	mux.HandleFunc("/skills", a.requireWeb(a.handleSkills))
	mux.HandleFunc("/skills/", a.requireWeb(a.handleSkillRoute))
	mux.HandleFunc("/help", a.requireWeb(a.handleHelp))
	mux.HandleFunc("/v1/", a.handleAPI)
	mux.HandleFunc("/", a.requireWeb(a.handleHome))
}

func (a *app) handleCSS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	_, _ = io.WriteString(w, css)
}

func (a *app) requireWeb(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := a.currentUser(r)
		if u == nil {
			http.Redirect(w, r, "/login", http.StatusFound)
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

func (a *app) currentUser(r *http.Request) *User {
	c, err := r.Cookie("skillsdex_session")
	if err != nil || c.Value == "" {
		return nil
	}
	u, err := userForSession(r.Context(), a.db, c.Value)
	if err != nil {
		return nil
	}
	return u
}

func webUser(r *http.Request) *User {
	u, _ := r.Context().Value(userKey).(*User)
	return u
}

func (a *app) handleHome(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/skills", http.StatusFound)
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
		a.render(w, r, nil, "Login", loginForm("Invalid username or password."))
		return
	}
	token, err := createWebSession(r.Context(), a.db, u.ID)
	if err != nil {
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "skillsdex_session", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, "/skills", http.StatusFound)
}

func (a *app) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("skillsdex_session"); err == nil {
		_ = deleteWebSession(r.Context(), a.db, c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: "skillsdex_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, "/login", http.StatusFound)
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
		a.render(w, r, u, "API Tokens", tokensPage(tokens, ""))
	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		name := strings.TrimSpace(r.FormValue("name"))
		if name == "" {
			name = "default"
		}
		_, err := createAPIToken(r.Context(), a.db, u.ID, name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/tokens", http.StatusFound)
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
	http.Redirect(w, r, "/tokens", http.StatusFound)
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
			search, err = searchAgentSkills(r.Context(), http.DefaultClient, query, 10)
			if err != nil {
				searchMsg = err.Error()
			}
		}
		a.render(w, r, u, "Import Skills", skillImportPage("", "", nil, query, search, searchMsg))
	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		source := strings.TrimSpace(firstNonEmpty(r.FormValue("source_url"), r.FormValue("github_url")))
		inputs, err := importSkillInputs(r.Context(), http.DefaultClient, source, u.Username)
		if err != nil {
			a.render(w, r, u, "Import Skills", skillImportPage(source, err.Error(), nil, "", agentSkillSearchResponse{}, ""))
			return
		}
		var results []skillImportResult
		for _, in := range inputs {
			s, status, err := upsertImportedSkill(r.Context(), a.db, u, in)
			if err != nil {
				results = append(results, skillImportResult{Identifier: in.Owner + "/" + in.Slug, Name: in.Name, Status: err.Error()})
				continue
			}
			results = append(results, skillImportResult{Identifier: s.OwnerSlug + "/" + s.Slug, Name: s.Name, Status: status})
		}
		a.render(w, r, u, "Import Skills", skillImportPage(source, "", results, "", agentSkillSearchResponse{}, ""))
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *app) handleSkillTop(w http.ResponseWriter, r *http.Request, u *User) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	skills, err := topAgentSkills(r.Context(), http.DefaultClient, 100)
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	a.render(w, r, u, "Top Skills", topSkillsPage(skills, msg))
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
	in := SkillInput{Owner: owner, Slug: slug, Name: s.Name, Description: s.Description, ShortDescription: s.ShortDescription, RecommendedDefault: s.RecommendedDefault, Body: bodyFromSkillMarkdown(string(raw))}
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
	if r.FormValue("action") == "preview" {
		a.render(w, r, u, "Edit Skill", skillFormPage("/skills/"+owner+"/"+slug+"/edit", false, in, ""))
		return
	}
	_, err = updateSkill(r.Context(), a.db, u, owner, slug, in)
	if err != nil {
		a.render(w, r, u, "Edit Skill", skillFormPage("/skills/"+owner+"/"+slug+"/edit", false, in, err.Error()))
		return
	}
	http.Redirect(w, r, "/skills/"+owner+"/"+slug, http.StatusFound)
}

func (a *app) handleSkillDetail(w http.ResponseWriter, r *http.Request, u *User, owner, slug string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	raw, s, err := rawSkill(r.Context(), a.db, u.ID, owner, slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	a.render(w, r, u, s.OwnerSlug+"/"+s.Slug, skillDetailPage(s, string(raw)))
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
	http.Redirect(w, r, "/skills?archived=1", http.StatusFound)
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
	content, rev, s, err := getRevisionContent(r.Context(), a.db, u.ID, owner, slug, revID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	prev, err := previousRevisionContent(r.Context(), a.db, rev)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	diff := unifiedDiff(string(prev), string(content))
	a.render(w, r, u, "Revision", revisionPage(s, rev, string(content), diff))
}

func skillInputFromForm(r *http.Request, defaultOwner string) SkillInput {
	_ = r.ParseForm()
	owner := strings.TrimSpace(r.FormValue("owner"))
	if owner == "" {
		owner = defaultOwner
	}
	return SkillInput{
		Owner:              owner,
		Slug:               strings.TrimSpace(r.FormValue("slug")),
		Name:               strings.TrimSpace(r.FormValue("name")),
		Description:        strings.TrimSpace(r.FormValue("description")),
		ShortDescription:   strings.TrimSpace(r.FormValue("short_description")),
		RecommendedDefault: r.FormValue("recommended_default") == "on",
		Body:               strings.TrimRight(r.FormValue("body"), "\r\n"),
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
		writeJSON(w, map[string]any{"skill": s})
		return
	}
	if strings.HasPrefix(path, "skills/") {
		a.handleSkillAPI(w, r, u, strings.TrimPrefix(path, "skills/"))
		return
	}
	writeJSONError(w, http.StatusNotFound, "not found")
}

func (a *app) apiUser(r *http.Request) (*User, error) {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return nil, errors.New("missing bearer token")
	}
	return userForAPIToken(r.Context(), a.db, strings.TrimSpace(strings.TrimPrefix(auth, "Bearer ")))
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
			raw, s, err := rawSkill(r.Context(), a.db, u.ID, owner, slug)
			if err != nil {
				status := http.StatusInternalServerError
				if isNotFound(err) {
					status = http.StatusNotFound
				}
				writeJSONError(w, status, "not found")
				return
			}
			writeJSON(w, map[string]any{"skill": s, "raw": string(raw)})
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
	if len(parts) == 3 && parts[2] == "archive" && r.Method == http.MethodPost {
		if err := archiveSkill(r.Context(), a.db, u.ID, owner, slug); err != nil {
			writeJSONError(w, http.StatusNotFound, "not found")
			return
		}
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
	data := struct {
		Title string
		User  *User
		Body  template.HTML
	}{Title: title, User: u, Body: body}
	t := template.Must(template.New("layout").Parse(layoutTmpl))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.Execute(w, data); err != nil {
		log.Printf("render: %v", err)
	}
}

func loginForm(msg string) template.HTML {
	var b strings.Builder
	if msg != "" {
		b.WriteString(`<p class="flash">` + html.EscapeString(msg) + `</p>`)
	}
	b.WriteString(`<section class="login-shell"><div class="login-brand"><div class="mark">SD</div><h1>SkillsDex</h1><p>Private Codex skills, versioned and ready to install.</p></div><form class="panel login-panel" method="post" action="/login">
		<label>Username <input name="username" autocomplete="username" required autofocus></label>
		<label>Password <input name="password" type="password" autocomplete="current-password" required></label>
		<button type="submit">Log in</button>
	</form></section>`)
	return template.HTML(b.String())
}

func usersPage(users []User, msg string) template.HTML {
	var b strings.Builder
	b.WriteString(`<header class="pagehead"><div><p class="eyebrow">Admin</p><h1>Users</h1></div></header><div class="split"><section class="surface">`)
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
		b.WriteString(`<tr><td><strong>` + html.EscapeString(u.Username) + `</strong></td><td>` + html.EscapeString(u.Email) + `</td><td><span class="pill">` + role + `</span></td><td><span class="pill ` + status + `">` + status + `</span></td><td><a href="/admin/users/` + html.EscapeString(u.ID) + `">Edit</a></td></tr>`)
	}
	b.WriteString(`</tbody></table></section><section class="panel sticky"><h2>Create User</h2><form method="post" action="/admin/users">
		<label>Username <input name="username" required pattern="[a-z0-9][a-z0-9-]{0,62}"></label>
		<label>Email <input name="email" type="email"></label>
		<label>Password <input name="password" type="password" required></label>
		<label class="check"><input name="is_admin" type="checkbox"> Admin</label>
		<button type="submit">Create</button>
	</form></section></div>`)
	return template.HTML(b.String())
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
	b.WriteString(`> Deactivated</label><button type="submit">Save</button></form></section>`)
	return template.HTML(b.String())
}

func tokensPage(tokens []APIToken, msg string) template.HTML {
	var b strings.Builder
	b.WriteString(`<header class="pagehead"><div><p class="eyebrow">CLI access</p><h1>API Tokens</h1></div></header><div class="split"><section class="surface">`)
	if msg != "" {
		b.WriteString(`<p class="flash">` + html.EscapeString(msg) + `</p>`)
	}
	b.WriteString(`<table><thead><tr><th>Name</th><th>Token</th><th>Created</th><th>Last used</th><th>Status</th><th></th></tr></thead><tbody>`)
	if len(tokens) == 0 {
		b.WriteString(`<tr><td colspan="6"><div class="empty">No API tokens yet.</div></td></tr>`)
	}
	for _, t := range tokens {
		status := "active"
		if t.RevokedAt != "" {
			status = "revoked"
		}
		b.WriteString(`<tr><td><strong>` + html.EscapeString(t.Name) + `</strong></td><td class="token-cell"><code>` + html.EscapeString(t.Token) + `</code></td><td>` + t.CreatedAt.Format("2006-01-02 15:04") + `</td><td>` + html.EscapeString(t.LastUsedAt) + `</td><td><span class="pill ` + status + `">` + status + `</span></td><td>`)
		if t.RevokedAt == "" {
			b.WriteString(`<form method="post" action="/tokens/` + html.EscapeString(t.ID) + `/revoke"><button class="link danger" type="submit">Revoke</button></form>`)
		}
		b.WriteString(`</td></tr>`)
	}
	b.WriteString(`</tbody></table></section><aside class="panel sticky"><h2>New Token</h2><form method="post" action="/tokens">
		<label>Name <input name="name" value="local cli"></label><button type="submit">Generate</button>
	</form><div class="command-card"><span>CLI login</span><pre>skillsdex login --server http://127.0.0.1:8765 --token &lt;token&gt;</pre></div></aside></div>`)
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
	b.WriteString(`<header class="pagehead"><div><p class="eyebrow">Library</p><h1>Skills</h1><div class="metrics"><span><strong>` + fmt.Sprintf("%d", active) + `</strong> active</span><span><strong>` + fmt.Sprintf("%d", archivedCount) + `</strong> archived</span></div></div><div class="actions"><a class="button" href="/skills/new">New Skill</a> `)
	b.WriteString(`<a class="button secondary" href="/skills/import">Import</a> `)
	b.WriteString(`<a class="button secondary" href="/skills/top">Top Skills</a> `)
	if archived {
		b.WriteString(`<a href="/skills">Hide archived</a>`)
	} else {
		b.WriteString(`<a href="/skills?archived=1">Show archived</a>`)
	}
	b.WriteString(`</div></header><div class="split wide"><section class="surface"><table><thead><tr><th>Identifier</th><th>Name</th><th>Revision</th><th>Updated</th><th>Status</th></tr></thead><tbody>`)
	if len(skills) == 0 {
		b.WriteString(`<tr><td colspan="5"><div class="empty"><strong>No skills yet.</strong><br><a href="/skills/new">Create the first skill</a></div></td></tr>`)
	}
	for _, s := range skills {
		status := "active"
		if s.ArchivedAt != "" {
			status = "archived"
		}
		id := s.OwnerSlug + "/" + s.Slug
		b.WriteString(`<tr><td><a class="identifier" href="/skills/` + html.EscapeString(id) + `">` + html.EscapeString(id) + `</a></td><td><strong>` + html.EscapeString(s.Name) + `</strong><div class="muted">` + html.EscapeString(s.ShortDescription) + `</div>`)
		if s.RecommendedDefault {
			b.WriteString(`<div class="row-tags"><span class="pill active">recommended</span></div>`)
		}
		b.WriteString(`</td><td><span class="rev">r` + fmt.Sprintf("%d", s.RevisionNumber) + `</span></td><td>` + s.UpdatedAt.Format("2006-01-02 15:04") + `</td><td><span class="pill ` + status + `">` + status + `</span></td></tr>`)
	}
	b.WriteString(`</tbody></table></section><aside class="panel sticky"><h2>Install Starter Skills</h2><div class="command-card"><span>Recommended defaults</span><pre>skillsdex install-defaults --target codex</pre></div><div class="command-card"><span>Single skill</span><pre>skillsdex install admin/five-advisor-review --target codex</pre></div><div class="command-card"><span>Sync managed installs</span><pre>skillsdex sync</pre></div></aside></div>`)
	return template.HTML(b.String())
}

type skillImportResult struct {
	Identifier string
	Name       string
	Status     string
}

func skillImportPage(source, msg string, results []skillImportResult, searchQuery string, search agentSkillSearchResponse, searchMsg string) template.HTML {
	var b strings.Builder
	b.WriteString(`<header class="pagehead"><div><p class="eyebrow">Import</p><h1>Source</h1></div></header><div class="split"><section class="panel"><form method="post" action="/skills/import">
		<label>Source <input name="source_url" value="` + html.EscapeString(source) + `" placeholder="https://github.com/DannyMac180/skills or https://agentskill.sh/@openclaw/openclaw-debugging" required autofocus></label>
		<div class="actions"><button type="submit">Import</button><a class="button secondary" href="/skills">Cancel</a></div>
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
	b.WriteString(`<section class="search-block"><h2>Search agentskill.sh</h2><form method="get" action="/skills/import" class="inline-search">
		<label>Query <input name="q" value="` + html.EscapeString(searchQuery) + `" placeholder="code review, playwright, seo"></label>
		<button type="submit">Search</button>
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
	b.WriteString(`</section><aside class="panel sticky"><h2>Supported Sources</h2><div class="command-card"><span>GitHub repository</span><pre>https://github.com/owner/repo</pre></div><div class="command-card"><span>GitHub skill folder</span><pre>https://github.com/owner/repo/tree/main/skill-name</pre></div><div class="command-card"><span>agentskill.sh skill</span><pre>https://agentskill.sh/@owner/skill-name</pre></div><div class="command-card"><span>agentskill.sh /learn</span><pre>npx @agentskill.sh/cli@latest setup</pre></div><p><a href="https://agentskill.sh/" target="_blank" rel="noopener">Browse agentskill.sh</a> · <a href="https://agentskill.sh/install" target="_blank" rel="noopener">Install /learn</a></p></aside></div>`)
	return template.HTML(b.String())
}

func topSkillsPage(search agentSkillSearchResponse, msg string) template.HTML {
	var b strings.Builder
	b.WriteString(`<header class="pagehead"><div><p class="eyebrow">agentskill.sh</p><h1>Top 100 Skills</h1><p>Ranked feed from agentskill.sh. Import saves the selected SKILL.md into your SkillsDex library.</p></div><div class="actions"><a class="button secondary" href="/skills/import">Import by URL</a><a class="button secondary" href="https://agentskill.sh/" target="_blank" rel="noopener">Browse agentskill.sh</a></div></header><section class="surface">`)
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
	b.WriteString(`<header class="pagehead"><div><p class="eyebrow">Reference</p><h1>Assistant Formats</h1><p>Choose the right shape for reusable agent guidance: portable skills, repo instructions, or editor-specific rules.</p></div><div class="actions"><a class="button" href="/skills/import">Import Skills</a><a class="button secondary" href="/skills/top">Top Skills</a></div></header>`)
	b.WriteString(`<section class="help-intent-grid" aria-label="Format guidance"><article class="panel help-principle"><span>Most portable</span><strong>Use <code>SKILL.md</code></strong><p>Package repeatable workflows as a skill directory with a clear description and focused instructions.</p></article><article class="panel help-principle"><span>Always visible</span><strong>Use repo guidance</strong><p>Keep broad project policy in files such as <code>AGENTS.md</code> or <code>CLAUDE.md</code>.</p></article><article class="panel help-principle"><span>File scoped</span><strong>Use editor rules</strong><p>Use Cursor rules when guidance should attach by glob, folder, or explicit mention.</p></article></section>`)
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
		b.WriteString(`<tr><td data-sort-value="` + html.EscapeString(result.Name) + `"><a class="identifier" href="https://agentskill.sh/@` + slug + `" target="_blank" rel="noopener">` + slug + `</a><div><strong>` + html.EscapeString(result.Name) + `</strong></div><div class="muted">` + html.EscapeString(result.Description) + `</div></td><td data-sort-value="` + scoreSortValue(result.ContentQualityScore) + `">` + scoreLabel(result.ContentQualityScore) + `</td><td data-sort-value="` + scoreSortValue(result.SecurityScore) + `">` + scoreLabel(result.SecurityScore) + `</td><td data-sort-value="` + fmt.Sprintf("%d", result.GitHubStars) + `">` + fmt.Sprintf("%d", result.GitHubStars) + `</td><td><form method="post" action="/skills/import"><input type="hidden" name="source_url" value="@` + slug + `"><button type="submit">Import</button></form></td></tr>`)
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
	b.WriteString(`<header class="pagehead"><div><p class="eyebrow">Editor</p><h1>` + title + `</h1></div></header>`)
	if msg != "" {
		b.WriteString(`<p class="flash">` + html.EscapeString(msg) + `</p>`)
	}
	b.WriteString(`<form method="post" action="` + html.EscapeString(action) + `" class="editor"><section class="panel">
		<label>Owner <input name="owner" value="` + html.EscapeString(in.Owner) + `" required`)
	if !create {
		b.WriteString(` readonly`)
	}
	b.WriteString(`></label><label>Slug <input name="slug" value="` + html.EscapeString(in.Slug) + `" required pattern="[a-z0-9][a-z0-9-]{0,62}"`)
	if !create {
		b.WriteString(` readonly`)
	}
	b.WriteString(`></label><label>Name <input name="name" value="` + html.EscapeString(in.Name) + `" required></label>
		<label>Description <textarea name="description" rows="3" required>` + html.EscapeString(in.Description) + `</textarea></label>
		<label>Short description <input name="short_description" value="` + html.EscapeString(in.ShortDescription) + `"></label>
		<label class="check"><input name="recommended_default" type="checkbox"`)
	if in.RecommendedDefault {
		b.WriteString(` checked`)
	}
	b.WriteString(`> Recommended default</label>
		<label>Markdown body <textarea name="body" rows="20" required>` + html.EscapeString(in.Body) + `</textarea></label>
		<div class="actions"><button name="action" value="save" type="submit">Save Revision</button><button class="secondary" name="action" value="preview" type="submit">Preview</button></div></section><aside class="panel preview-pane"><h2>Generated SKILL.md</h2><pre class="raw">` + html.EscapeString(preview) + `</pre><h2>Rendered Preview</h2><div class="rendered mini">` + simpleMarkdownHTML(in.Body) + `</div></aside></form>`)
	return template.HTML(b.String())
}

func skillDetailPage(s *Skill, raw string) template.HTML {
	id := s.OwnerSlug + "/" + s.Slug
	var b strings.Builder
	b.WriteString(`<header class="pagehead"><div><p class="eyebrow">Skill</p><h1>` + html.EscapeString(id) + `</h1><p>` + html.EscapeString(s.Description) + `</p><div class="metrics"><span><strong>r` + fmt.Sprintf("%d", s.RevisionNumber) + `</strong> latest</span><span><strong>` + s.UpdatedAt.Format("Jan 2") + `</strong> updated</span></div></div><div class="actions">
		<a class="button" href="/skills/` + html.EscapeString(id) + `/edit">Edit</a>
		<a class="button secondary" href="/skills/` + html.EscapeString(id) + `/raw">Raw</a>
		<a class="button secondary" href="/skills/` + html.EscapeString(id) + `/revisions">Revisions</a></div></header>`)
	if s.ArchivedAt != "" {
		b.WriteString(`<p class="flash">This skill is archived.</p>`)
	} else {
		b.WriteString(`<form method="post" action="/skills/` + html.EscapeString(id) + `/archive"><button class="link danger" type="submit">Archive skill</button></form>`)
	}
	recommended := "no"
	if s.RecommendedDefault {
		recommended = "yes"
	}
	b.WriteString(`<div class="split wide"><section class="rendered">` + simpleMarkdownHTML(bodyFromSkillMarkdown(raw)) + `</section><aside class="panel sticky"><h2>Install</h2><div class="command-card"><span>Codex</span><pre>skillsdex install ` + html.EscapeString(id) + ` --target codex</pre></div><div class="meta-list"><div><span>Name</span><strong>` + html.EscapeString(s.Name) + `</strong></div><div><span>Short description</span><strong>` + html.EscapeString(s.ShortDescription) + `</strong></div><div><span>Recommended default</span><strong>` + recommended + `</strong></div></div></aside></div>`)
	return template.HTML(b.String())
}

func revisionsPage(owner, slug string, revs []SkillRevision) template.HTML {
	id := owner + "/" + slug
	var b strings.Builder
	b.WriteString(`<header class="pagehead"><div><p class="eyebrow">History</p><h1>Revisions for ` + html.EscapeString(id) + `</h1></div></header><section class="surface"><table><thead><tr><th>Revision</th><th>Created</th><th>Hash</th><th></th></tr></thead><tbody>`)
	for _, r := range revs {
		b.WriteString(`<tr><td><span class="rev">r` + fmt.Sprintf("%d", r.RevisionNumber) + `</span></td><td>` + r.CreatedAt.Format("2006-01-02 15:04") + `</td><td><code>` + html.EscapeString(r.TreeHash) + `</code></td><td><a href="/skills/` + html.EscapeString(id) + `/revisions/` + html.EscapeString(r.ID) + `">View diff</a></td></tr>`)
	}
	b.WriteString(`</tbody></table></section>`)
	return template.HTML(b.String())
}

func revisionPage(s *Skill, rev SkillRevision, raw, diff string) template.HTML {
	id := s.OwnerSlug + "/" + s.Slug
	var b strings.Builder
	b.WriteString(`<header class="pagehead"><div><p class="eyebrow">Revision</p><h1>` + html.EscapeString(id) + ` r` + fmt.Sprintf("%d", rev.RevisionNumber) + `</h1></div></header>`)
	b.WriteString(`<section class="panel"><h2>Diff from previous revision</h2>` + diffHTML(diff) + `</section>`)
	b.WriteString(`<section class="panel"><h2>SKILL.md at this revision</h2><pre class="raw">` + html.EscapeString(raw) + `</pre></section>`)
	return template.HTML(b.String())
}

const layoutTmpl = `<!doctype html>
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
				<a href="/skills">Skills</a>
				<a href="/skills/top">Top Skills</a>
				<a href="/help">Help</a>
				<a href="/tokens">API Tokens</a>
				{{if .User.IsAdmin}}<a href="/admin/users">Users</a>{{end}}
			</div>
			<button class="theme-toggle" type="button" aria-label="Toggle color theme" data-theme-toggle>Theme</button>
			<form method="post" action="/logout"><button class="link" type="submit">Logout {{.User.Username}}</button></form>
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
			button.textContent = theme === "dark" ? "Light" : "Dark";
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
	</script>
</body>
</html>`

const css = `
:root { color-scheme: light; font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; color: #1d232b; background: #eef1f4; --bg: #eef1f4; --surface: #ffffff; --surface-2: #f8fafb; --line: #d8dee5; --text: #1d232b; --muted: #66717d; --accent: #1f6f78; --accent-strong: #15545b; --danger: #a33838; --ok: #237a4d; --warn: #9a6a16; --nav: rgba(255,255,255,.92); --nav-link: #315365; --nav-hover: #edf7f8; --row-hover: #fbfcfd; --field: #ffffff; --field-border: #c7d0d8; --soft: #f3f6f7; --soft-border: #dfe6ea; --code-bg: #111820; --code-text: #f8fafc; --shadow: rgba(22,30,38,.04); --focus: rgba(31,111,120,.22); --warning-bg: #fff7e6; --warning-line: #ecd39a; --warning-text: #6f4c0b; --ok-bg: #eef8f2; --ok-line: #cce8d7; }
:root[data-theme="dark"] { color-scheme: dark; color: #e6edf3; background: #11161c; --bg: #11161c; --surface: #171e26; --surface-2: #1d2630; --line: #303b47; --text: #e6edf3; --muted: #9aa7b4; --accent: #2f9aa6; --accent-strong: #6bc8d1; --danger: #ff8b8b; --ok: #79d49c; --warn: #e6bd6a; --nav: rgba(23,30,38,.92); --nav-link: #bdd7dc; --nav-hover: #20323a; --row-hover: #1c2530; --field: #10161d; --field-border: #3a4652; --soft: #202a33; --soft-border: #35414d; --code-bg: #0b1016; --code-text: #e8eef5; --shadow: rgba(0,0,0,.22); --focus: rgba(47,154,166,.34); --warning-bg: #332716; --warning-line: #6b5224; --warning-text: #f0cf86; --ok-bg: #173025; --ok-line: #2f6048; }
* { box-sizing: border-box; }
body { margin: 0; min-height: 100vh; background: var(--bg); }
.app-shell { min-height: 100vh; }
nav { min-height: 64px; display: flex; align-items: center; gap: 22px; padding: 0 28px; background: var(--nav); border-bottom: 1px solid var(--line); position: sticky; top: 0; z-index: 10; backdrop-filter: blur(10px); }
nav a { color: var(--nav-link); text-decoration: none; font-weight: 700; }
nav a:hover { color: var(--accent-strong); }
nav form { margin-left: auto; }
.nav-links { display: flex; align-items: center; gap: 6px; }
.nav-links a { padding: 8px 10px; border-radius: 6px; }
.nav-links a:hover { background: var(--nav-hover); }
.brand { color: var(--text); font-size: 18px; display: inline-flex; align-items: center; gap: 10px; margin-right: 6px; }
.brand-mark, .mark { display: inline-grid; place-items: center; width: 34px; height: 34px; border-radius: 8px; background: #173f46; color: #fff; font-weight: 800; letter-spacing: 0; }
main { max-width: 1240px; margin: 0 auto; padding: 30px; }
h1 { font-size: 30px; line-height: 1.15; margin: 0 0 10px; letter-spacing: 0; }
h2 { font-size: 18px; margin: 0 0 14px; letter-spacing: 0; }
p { line-height: 1.55; }
a { color: var(--accent-strong); }
table { width: 100%; border-collapse: collapse; background: var(--surface); }
th, td { text-align: left; padding: 14px 14px; border-bottom: 1px solid var(--line); vertical-align: middle; }
th { font-size: 11px; text-transform: uppercase; color: var(--muted); background: var(--surface-2); letter-spacing: .04em; }
tr:hover td { background: var(--row-hover); }
tbody tr:last-child td { border-bottom: 0; }
code, pre { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }
pre { white-space: pre-wrap; overflow: auto; }
.surface, .panel, .rendered { background: var(--surface); border: 1px solid var(--line); border-radius: 8px; box-shadow: 0 1px 2px var(--shadow); overflow: hidden; }
.panel { padding: 20px; overflow: visible; }
.narrow { max-width: 480px; margin: 42px auto; }
.split { display: grid; grid-template-columns: minmax(0, 1fr) 360px; gap: 22px; align-items: start; }
.split.wide { grid-template-columns: minmax(0, 1fr) 380px; }
.sticky { position: sticky; top: 84px; }
.pagehead { display: flex; justify-content: space-between; gap: 18px; align-items: flex-start; margin-bottom: 22px; }
.pagehead p { margin: 0; color: var(--muted); max-width: 720px; }
.eyebrow { font-size: 12px; font-weight: 800; text-transform: uppercase; color: var(--accent-strong) !important; letter-spacing: .08em; margin-bottom: 6px !important; }
.metrics { display: flex; gap: 8px; flex-wrap: wrap; margin-top: 12px; }
.metrics span { display: inline-flex; align-items: center; gap: 6px; min-height: 30px; padding: 5px 9px; border-radius: 6px; background: var(--soft); border: 1px solid var(--soft-border); color: var(--muted); font-size: 13px; }
.metrics strong { color: var(--text); }
form { margin: 0; }
label { display: grid; gap: 7px; margin-bottom: 15px; font-weight: 700; color: var(--text); }
input, textarea { width: 100%; border: 1px solid var(--field-border); border-radius: 6px; padding: 10px 11px; font: inherit; background: var(--field); color: var(--text); }
input:focus, textarea:focus, button:focus-visible, a:focus-visible { outline: 3px solid var(--focus); outline-offset: 2px; border-color: var(--accent); }
textarea { resize: vertical; }
button, .button { display: inline-flex; align-items: center; justify-content: center; min-height: 38px; border: 1px solid var(--accent-strong); border-radius: 6px; background: var(--accent); color: #fff; padding: 8px 14px; font: inherit; font-weight: 800; text-decoration: none; cursor: pointer; }
button:hover, .button:hover { background: var(--accent-strong); color: #fff; }
.secondary { background: var(--surface); color: var(--accent-strong); }
.secondary:hover { background: var(--nav-hover); color: var(--accent-strong); }
.link { border: 0; background: transparent; color: var(--nav-link); padding: 0; min-height: 0; font-weight: 750; }
.link:hover { background: transparent; color: var(--accent-strong); }
.danger { color: var(--danger); }
.check { display: flex; align-items: center; gap: 8px; }
.check input { width: auto; }
.actions { display: flex; gap: 10px; flex-wrap: wrap; }
.theme-toggle { margin-left: auto; min-height: 34px; padding: 6px 10px; background: var(--surface); color: var(--text); border-color: var(--line); }
.theme-toggle:hover { background: var(--nav-hover); color: var(--text); }
.theme-toggle + form { margin-left: 0; }
.flash { background: var(--warning-bg); border: 1px solid var(--warning-line); padding: 11px 13px; border-radius: 6px; color: var(--warning-text); }
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
.sort-head::after { content: "↕"; margin-left: 5px; color: var(--muted); font-size: 10px; }
.sort-head[aria-sort="ascending"]::after { content: "↑"; color: var(--accent-strong); }
.sort-head[aria-sort="descending"]::after { content: "↓"; color: var(--accent-strong); }
.editor { display: grid; grid-template-columns: minmax(0, 1fr) minmax(360px, 500px); gap: 22px; align-items: start; }
.preview-pane { max-height: calc(100vh - 112px); overflow: auto; }
.raw { background: var(--code-bg); color: var(--code-text); border-radius: 6px; padding: 14px; border: 1px solid #263241; }
.rendered { padding: 26px; overflow: visible; }
.rendered h1:first-child { margin-top: 0; }
.mini { padding: 14px; }
.muted { color: var(--muted); font-size: 13px; margin-top: 4px; }
.row-tags { display: flex; gap: 6px; flex-wrap: wrap; margin-top: 8px; }
.identifier { font-weight: 800; text-decoration: none; }
.pill, .rev { display: inline-flex; align-items: center; min-height: 24px; padding: 3px 8px; border-radius: 999px; border: 1px solid var(--soft-border); background: var(--soft); color: var(--text); font-size: 12px; font-weight: 800; }
.pill.active { color: var(--ok); background: var(--ok-bg); border-color: var(--ok-line); }
.pill.archived, .pill.revoked, .pill.deactivated { color: var(--warn); background: var(--warning-bg); border-color: var(--warning-line); }
.token-cell code { display: block; max-width: 360px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.empty { padding: 28px; text-align: center; color: var(--muted); }
.command-card { display: grid; gap: 8px; margin-top: 14px; }
.command-card span { color: var(--muted); font-size: 12px; font-weight: 800; text-transform: uppercase; letter-spacing: .06em; }
.command-card pre { margin: 0; background: var(--code-bg); color: var(--code-text); border-radius: 6px; padding: 13px; border: 1px solid #263241; }
.meta-list { display: grid; gap: 12px; margin-top: 18px; }
.meta-list div { display: grid; gap: 4px; padding-top: 12px; border-top: 1px solid var(--line); }
.meta-list span { color: var(--muted); font-size: 12px; text-transform: uppercase; font-weight: 800; letter-spacing: .06em; }
.login-shell { min-height: calc(100vh - 120px); display: grid; grid-template-columns: minmax(0, 1fr) 420px; gap: 34px; align-items: center; max-width: 960px; margin: 0 auto; }
.login-brand h1 { font-size: 44px; margin-top: 18px; }
.login-brand p { color: var(--muted); font-size: 18px; max-width: 460px; }
.login-panel { padding: 24px; }
.mark { width: 48px; height: 48px; border-radius: 10px; }
.diff { background: var(--surface); border: 1px solid var(--line); border-radius: 6px; padding: 0; }
.diff span { display: block; padding: 2px 8px; }
.diff .add { background: #e7f6e7; color: #14532d; }
.diff .del { background: #fde8e8; color: #842029; }
.diff .ctx { color: #4b5563; }
@media (max-width: 820px) {
	main { padding: 18px; }
	.split, .split.wide, .editor, .login-shell { grid-template-columns: 1fr; }
	.help-intent-grid, .help-cards { grid-template-columns: 1fr; }
	.inline-search { grid-template-columns: 1fr; }
	.pagehead { display: block; }
	.sticky, .preview-pane { position: static; max-height: none; }
	nav { padding: 0 16px; gap: 10px; overflow-x: auto; align-items: center; }
	.nav-links { gap: 2px; }
	.brand span:last-child { display: none; }
	.token-cell code { max-width: 220px; }
	h1 { font-size: 26px; }
}
`
