package skillsdex

import (
	"context"
	"crypto/subtle"
	"net"
	"net/http"
	"strings"
)

type ctxKey string

const userKey ctxKey = "user"

func (a *app) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; base-uri 'self'; frame-ancestors 'none'; form-action 'self'; object-src 'none'")
		if a.isSecureRequest(r) {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
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
		if r.Method == http.MethodPost {
			limit := int64(maxSkillsDexExportBytes)
			if r.URL.Path == "/instructions" {
				limit = 128 * 1024
			}
			r.Body = http.MaxBytesReader(w, r.Body, limit)
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
	http.Redirect(w, r, "/overview", http.StatusFound)
}

func (a *app) remoteAddr(r *http.Request) string {
	if a.isTrustedProxyRequest(r) {
		for _, forwarded := range strings.Split(r.Header.Get("X-Forwarded-For"), ",") {
			forwarded = strings.TrimSpace(forwarded)
			if forwarded == "" {
				continue
			}
			if ip := net.ParseIP(forwarded); ip != nil {
				return ip.String()
			}
		}
	}
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
		Secure:   r.TLS != nil,
	}
}

func (a *app) sessionCookie(r *http.Request, value string, maxAge int) *http.Cookie {
	c := sessionCookie(r, value, maxAge)
	c.Secure = a.isSecureRequest(r)
	return c
}

func (a *app) isSecureRequest(r *http.Request) bool {
	return r.TLS != nil || (a.isTrustedProxyRequest(r) && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"))
}

func (a *app) isTrustedProxyRequest(r *http.Request) bool {
	if len(a.trustedProxies) == 0 {
		return false
	}
	host := strings.TrimSpace(r.RemoteAddr)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, network := range a.trustedProxies {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}
