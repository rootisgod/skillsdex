package skillsdex

import (
	"net/http"
	"strings"
)

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
