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

func (a *app) handleSkillEdit(w http.ResponseWriter, r *http.Request, u *User, owner, slug string) {
	raw, s, err := rawSkill(r.Context(), a.db, u.ID, owner, slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	in := SkillInput{Owner: owner, Slug: slug, Name: s.Name, Description: s.Description, ShortDescription: s.ShortDescription, Body: bodyFromSkillMarkdown(string(raw))}
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
		Owner:            owner,
		Slug:             strings.TrimSpace(r.FormValue("slug")),
		Name:             strings.TrimSpace(r.FormValue("name")),
		Description:      strings.TrimSpace(r.FormValue("description")),
		ShortDescription: strings.TrimSpace(r.FormValue("short_description")),
		Body:             strings.TrimRight(r.FormValue("body"), "\r\n"),
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
	b.WriteString(`<section class="panel narrow"><h1>SkillsDex</h1><form method="post" action="/login">
		<label>Username <input name="username" autocomplete="username" required autofocus></label>
		<label>Password <input name="password" type="password" autocomplete="current-password" required></label>
		<button type="submit">Log in</button>
	</form></section>`)
	return template.HTML(b.String())
}

func usersPage(users []User, msg string) template.HTML {
	var b strings.Builder
	b.WriteString(`<div class="split"><section><h1>Users</h1>`)
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
		b.WriteString(`<tr><td>` + html.EscapeString(u.Username) + `</td><td>` + html.EscapeString(u.Email) + `</td><td>` + role + `</td><td>` + status + `</td><td><a href="/admin/users/` + html.EscapeString(u.ID) + `">Edit</a></td></tr>`)
	}
	b.WriteString(`</tbody></table></section><section class="panel"><h2>Create User</h2><form method="post" action="/admin/users">
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
	b.WriteString(`<section class="panel narrow"><h1>Edit ` + html.EscapeString(u.Username) + `</h1>`)
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
	b.WriteString(`<div class="split"><section><h1>API Tokens</h1>`)
	if msg != "" {
		b.WriteString(`<p class="flash">` + html.EscapeString(msg) + `</p>`)
	}
	b.WriteString(`<table><thead><tr><th>Name</th><th>Token</th><th>Created</th><th>Last used</th><th>Status</th><th></th></tr></thead><tbody>`)
	for _, t := range tokens {
		status := "active"
		if t.RevokedAt != "" {
			status = "revoked"
		}
		b.WriteString(`<tr><td>` + html.EscapeString(t.Name) + `</td><td><code>` + html.EscapeString(t.Token) + `</code></td><td>` + t.CreatedAt.Format("2006-01-02 15:04") + `</td><td>` + html.EscapeString(t.LastUsedAt) + `</td><td>` + status + `</td><td>`)
		if t.RevokedAt == "" {
			b.WriteString(`<form method="post" action="/tokens/` + html.EscapeString(t.ID) + `/revoke"><button class="link danger" type="submit">Revoke</button></form>`)
		}
		b.WriteString(`</td></tr>`)
	}
	b.WriteString(`</tbody></table></section><section class="panel"><h2>New Token</h2><form method="post" action="/tokens">
		<label>Name <input name="name" value="local cli"></label><button type="submit">Generate</button>
	</form><p class="muted">Use this with <code>skillsdex login</code>.</p></section></div>`)
	return template.HTML(b.String())
}

func skillsPage(skills []Skill, archived bool) template.HTML {
	var b strings.Builder
	b.WriteString(`<header class="pagehead"><h1>Skills</h1><div><a class="button" href="/skills/new">New Skill</a> `)
	if archived {
		b.WriteString(`<a href="/skills">Hide archived</a>`)
	} else {
		b.WriteString(`<a href="/skills?archived=1">Show archived</a>`)
	}
	b.WriteString(`</div></header><table><thead><tr><th>Identifier</th><th>Name</th><th>Revision</th><th>Updated</th><th>Status</th></tr></thead><tbody>`)
	for _, s := range skills {
		status := "active"
		if s.ArchivedAt != "" {
			status = "archived"
		}
		id := s.OwnerSlug + "/" + s.Slug
		b.WriteString(`<tr><td><a href="/skills/` + html.EscapeString(id) + `">` + html.EscapeString(id) + `</a></td><td>` + html.EscapeString(s.Name) + `</td><td>` + fmt.Sprintf("%d", s.RevisionNumber) + `</td><td>` + s.UpdatedAt.Format("2006-01-02 15:04") + `</td><td>` + status + `</td></tr>`)
	}
	b.WriteString(`</tbody></table>`)
	return template.HTML(b.String())
}

func skillFormPage(action string, create bool, in SkillInput, msg string) template.HTML {
	var b strings.Builder
	title := "Edit Skill"
	if create {
		title = "New Skill"
	}
	preview := renderSkillMarkdown(in)
	b.WriteString(`<h1>` + title + `</h1>`)
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
		<label>Markdown body <textarea name="body" rows="20" required>` + html.EscapeString(in.Body) + `</textarea></label>
		<div class="actions"><button name="action" value="save" type="submit">Save Revision</button><button class="secondary" name="action" value="preview" type="submit">Preview</button></div></section><section class="panel"><h2>Generated SKILL.md</h2><pre class="raw">` + html.EscapeString(preview) + `</pre><h2>Rendered Preview</h2><div class="rendered mini">` + simpleMarkdownHTML(in.Body) + `</div></section></form>`)
	return template.HTML(b.String())
}

func skillDetailPage(s *Skill, raw string) template.HTML {
	id := s.OwnerSlug + "/" + s.Slug
	var b strings.Builder
	b.WriteString(`<header class="pagehead"><div><h1>` + html.EscapeString(id) + `</h1><p>` + html.EscapeString(s.Description) + `</p></div><div>
		<a class="button" href="/skills/` + html.EscapeString(id) + `/edit">Edit</a>
		<a class="button secondary" href="/skills/` + html.EscapeString(id) + `/raw">Raw</a>
		<a class="button secondary" href="/skills/` + html.EscapeString(id) + `/revisions">Revisions</a></div></header>`)
	if s.ArchivedAt != "" {
		b.WriteString(`<p class="flash">This skill is archived.</p>`)
	} else {
		b.WriteString(`<form method="post" action="/skills/` + html.EscapeString(id) + `/archive"><button class="link danger" type="submit">Archive skill</button></form>`)
	}
	b.WriteString(`<section class="panel"><h2>Install</h2><pre>skillsdex install ` + html.EscapeString(id) + ` --target codex</pre></section>`)
	b.WriteString(`<section class="rendered">` + simpleMarkdownHTML(bodyFromSkillMarkdown(raw)) + `</section>`)
	return template.HTML(b.String())
}

func revisionsPage(owner, slug string, revs []SkillRevision) template.HTML {
	id := owner + "/" + slug
	var b strings.Builder
	b.WriteString(`<h1>Revisions for ` + html.EscapeString(id) + `</h1><table><thead><tr><th>Revision</th><th>Created</th><th>Hash</th><th></th></tr></thead><tbody>`)
	for _, r := range revs {
		b.WriteString(`<tr><td>` + fmt.Sprintf("%d", r.RevisionNumber) + `</td><td>` + r.CreatedAt.Format("2006-01-02 15:04") + `</td><td><code>` + html.EscapeString(r.TreeHash) + `</code></td><td><a href="/skills/` + html.EscapeString(id) + `/revisions/` + html.EscapeString(r.ID) + `">View diff</a></td></tr>`)
	}
	b.WriteString(`</tbody></table>`)
	return template.HTML(b.String())
}

func revisionPage(s *Skill, rev SkillRevision, raw, diff string) template.HTML {
	id := s.OwnerSlug + "/" + s.Slug
	var b strings.Builder
	b.WriteString(`<h1>` + html.EscapeString(id) + ` revision ` + fmt.Sprintf("%d", rev.RevisionNumber) + `</h1>`)
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
	<link rel="stylesheet" href="/static/style.css">
</head>
<body>
	<nav>
		<a class="brand" href="/skills">SkillsDex</a>
		{{if .User}}
		<a href="/skills">Skills</a>
		<a href="/tokens">API Tokens</a>
		{{if .User.IsAdmin}}<a href="/admin/users">Users</a>{{end}}
		<form method="post" action="/logout"><button class="link" type="submit">Logout {{.User.Username}}</button></form>
		{{end}}
	</nav>
	<main>{{.Body}}</main>
</body>
</html>`

const css = `
:root { color-scheme: light; font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; color: #202124; background: #f6f7f9; }
* { box-sizing: border-box; }
body { margin: 0; }
nav { height: 56px; display: flex; align-items: center; gap: 18px; padding: 0 28px; background: #fff; border-bottom: 1px solid #dfe3e8; }
nav a { color: #2b4c6f; text-decoration: none; font-weight: 600; }
nav form { margin-left: auto; }
.brand { color: #111; font-size: 18px; }
main { max-width: 1180px; margin: 0 auto; padding: 28px; }
h1 { font-size: 28px; margin: 0 0 18px; }
h2 { font-size: 18px; margin: 0 0 14px; }
p { line-height: 1.55; }
table { width: 100%; border-collapse: collapse; background: #fff; border: 1px solid #dfe3e8; }
th, td { text-align: left; padding: 11px 12px; border-bottom: 1px solid #e8ebef; vertical-align: top; }
th { font-size: 12px; text-transform: uppercase; color: #59636e; background: #fbfbfc; }
code, pre { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }
pre { white-space: pre-wrap; overflow: auto; }
.panel { background: #fff; border: 1px solid #dfe3e8; border-radius: 8px; padding: 18px; }
.narrow { max-width: 440px; margin: 42px auto; }
.split { display: grid; grid-template-columns: minmax(0, 1fr) 340px; gap: 22px; align-items: start; }
.pagehead { display: flex; justify-content: space-between; gap: 18px; align-items: flex-start; margin-bottom: 20px; }
.pagehead p { margin: -8px 0 0; color: #59636e; }
form { margin: 0; }
label { display: grid; gap: 6px; margin-bottom: 14px; font-weight: 650; color: #30363d; }
input, textarea { width: 100%; border: 1px solid #c9d1d9; border-radius: 6px; padding: 9px 10px; font: inherit; background: #fff; }
textarea { resize: vertical; }
button, .button { display: inline-flex; align-items: center; min-height: 36px; border: 1px solid #244f7d; border-radius: 6px; background: #244f7d; color: #fff; padding: 8px 13px; font: inherit; font-weight: 700; text-decoration: none; cursor: pointer; }
.secondary { background: #fff; color: #244f7d; }
.link { border: 0; background: transparent; color: #2b4c6f; padding: 0; min-height: 0; font-weight: 650; }
.danger { color: #a02c2c; }
.check { display: flex; align-items: center; gap: 8px; }
.check input { width: auto; }
.actions { display: flex; gap: 10px; flex-wrap: wrap; }
.flash { background: #fff3cd; border: 1px solid #f0d98c; padding: 10px 12px; border-radius: 6px; }
.editor { display: grid; grid-template-columns: minmax(0, 1fr) minmax(320px, 440px); gap: 22px; align-items: start; }
.raw { background: #0f1720; color: #f8fafc; border-radius: 6px; padding: 14px; }
.rendered { background: #fff; border: 1px solid #dfe3e8; border-radius: 8px; padding: 24px; }
.mini { padding: 14px; }
.muted { color: #68737f; }
.diff { background: #fff; border: 1px solid #dfe3e8; border-radius: 6px; padding: 0; }
.diff span { display: block; padding: 2px 8px; }
.diff .add { background: #e7f6e7; color: #14532d; }
.diff .del { background: #fde8e8; color: #842029; }
.diff .ctx { color: #4b5563; }
@media (max-width: 820px) {
	main { padding: 18px; }
	.split, .editor { grid-template-columns: 1fr; }
	.pagehead { display: block; }
	nav { padding: 0 16px; gap: 10px; overflow-x: auto; }
}
`
