package skillsdex

import (
	"net/http"
	"strings"
)

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
