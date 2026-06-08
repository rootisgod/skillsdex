package main

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func csrfForm(t *testing.T, db *sql.DB, session, body string) string {
	t.Helper()
	token, err := csrfTokenForSession(context.Background(), db, session)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		body += "&"
	}
	return body + "csrf_token=" + url.QueryEscape(token)
}

func csrfMultipart(t *testing.T, db *sql.DB, session string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	token, err := csrfTokenForSession(context.Background(), db, session)
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField("csrf_token", token); err != nil {
		t.Fatal(err)
	}
	fw, err := mw.CreateFormFile("restore_zip", "skillsdex-backup.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return &body, mw.FormDataContentType()
}

func TestHTTPServerUsesBoundedTimeouts(t *testing.T) {
	mux := http.NewServeMux()
	server := newHTTPServer("127.0.0.1:0", mux)
	if server.Addr != "127.0.0.1:0" {
		t.Fatalf("server addr = %q", server.Addr)
	}
	if server.Handler != mux {
		t.Fatal("server handler was not installed")
	}
	if server.ReadHeaderTimeout <= 0 || server.ReadTimeout <= 0 || server.WriteTimeout <= 0 || server.IdleTimeout <= 0 {
		t.Fatalf("server timeouts must be bounded: readHeader=%s read=%s write=%s idle=%s", server.ReadHeaderTimeout, server.ReadTimeout, server.WriteTimeout, server.IdleTimeout)
	}
}

func TestSecurityHeaders(t *testing.T) {
	proxies, err := parseTrustedProxies([]string{"127.0.0.1/32"})
	if err != nil {
		t.Fatal(err)
	}
	a := &app{trustedProxies: proxies}
	handler := a.securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:45678"
	req.Header.Set("X-Forwarded-Proto", "https")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	headers := w.Result().Header
	for name, want := range map[string]string{
		"X-Content-Type-Options":    "nosniff",
		"X-Frame-Options":           "DENY",
		"Referrer-Policy":           "no-referrer",
		"Strict-Transport-Security": "max-age=31536000",
	} {
		if got := headers.Get(name); got != want {
			t.Fatalf("%s = %q, want %q", name, got, want)
		}
	}
	if got := headers.Get("Content-Security-Policy"); !strings.Contains(got, "default-src 'self'") || !strings.Contains(got, "frame-ancestors 'none'") {
		t.Fatalf("unexpected CSP %q", got)
	}

	httpReq := httptest.NewRequest(http.MethodGet, "/", nil)
	httpW := httptest.NewRecorder()
	handler.ServeHTTP(httpW, httpReq)
	if got := httpW.Result().Header.Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("plain HTTP HSTS = %q, want empty", got)
	}

	spoofedReq := httptest.NewRequest(http.MethodGet, "/", nil)
	spoofedReq.RemoteAddr = "198.51.100.10:45678"
	spoofedReq.Header.Set("X-Forwarded-Proto", "https")
	spoofedW := httptest.NewRecorder()
	handler.ServeHTTP(spoofedW, spoofedReq)
	if got := spoofedW.Result().Header.Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("untrusted forwarded HTTPS HSTS = %q, want empty", got)
	}
}

func TestSafeBootstrapRejectsPublicBindWithDefaultAdminPassword(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := requireSafeBootstrap(ctx, db, "127.0.0.1:8765"); err != nil {
		t.Fatalf("loopback bind should be allowed during first setup: %v", err)
	}
	if err := requireSafeBootstrap(ctx, db, "localhost:8765"); err != nil {
		t.Fatalf("localhost bind should be allowed during first setup: %v", err)
	}
	if err := requireSafeBootstrap(ctx, db, "0.0.0.0:8765"); err == nil || !strings.Contains(err.Error(), "default admin/admin password") {
		t.Fatalf("public bind err = %v, want default-password refusal", err)
	}
	if err := requireSafeBootstrap(ctx, db, ":8765"); err == nil || !strings.Contains(err.Error(), "default admin/admin password") {
		t.Fatalf("wildcard bind err = %v, want default-password refusal", err)
	}

	admin, err := getUserByUsername(ctx, db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := setUserPassword(ctx, db, admin.ID, "changed-password-123"); err != nil {
		t.Fatal(err)
	}
	if err := requireSafeBootstrap(ctx, db, "0.0.0.0:8765"); err != nil {
		t.Fatalf("public bind after password change should be allowed: %v", err)
	}
}

func TestTrustedProxyControlsForwardedHeaders(t *testing.T) {
	proxies, err := parseTrustedProxies([]string{"127.0.0.1,::1/128"})
	if err != nil {
		t.Fatal(err)
	}
	a := &app{trustedProxies: proxies}

	trustedReq := httptest.NewRequest(http.MethodGet, "/", nil)
	trustedReq.RemoteAddr = "127.0.0.1:45678"
	trustedReq.Header.Set("X-Forwarded-Proto", "https")
	trustedReq.Header.Set("X-Forwarded-For", "203.0.113.25, 127.0.0.1")
	if !a.isSecureRequest(trustedReq) {
		t.Fatal("trusted proxy HTTPS request was not treated as secure")
	}
	if got := a.remoteAddr(trustedReq); got != "203.0.113.25" {
		t.Fatalf("trusted proxy remote addr = %q, want client IP", got)
	}
	if !a.sessionCookie(trustedReq, "token", 0).Secure {
		t.Fatal("trusted proxy HTTPS cookie should be Secure")
	}

	spoofedReq := httptest.NewRequest(http.MethodGet, "/", nil)
	spoofedReq.RemoteAddr = "198.51.100.10:45678"
	spoofedReq.Header.Set("X-Forwarded-Proto", "https")
	spoofedReq.Header.Set("X-Forwarded-For", "203.0.113.25")
	if a.isSecureRequest(spoofedReq) {
		t.Fatal("untrusted forwarded HTTPS request was treated as secure")
	}
	if got := a.remoteAddr(spoofedReq); got != "198.51.100.10" {
		t.Fatalf("untrusted proxy remote addr = %q, want socket IP", got)
	}
	if a.sessionCookie(spoofedReq, "token", 0).Secure {
		t.Fatal("untrusted forwarded HTTPS cookie should not be Secure")
	}
}

func TestHelpPageRequiresAuthAndRendersReference(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	a := &app{db: db}
	mux := http.NewServeMux()
	a.routes(mux)

	unauth := httptest.NewRecorder()
	mux.ServeHTTP(unauth, httptest.NewRequest(http.MethodGet, "/help", nil))
	if unauth.Code != http.StatusFound || unauth.Header().Get("Location") != "/login" {
		t.Fatalf("unauthenticated /help = %d location %q, want redirect to /login", unauth.Code, unauth.Header().Get("Location"))
	}

	admin, err := getUserByUsername(context.Background(), db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	session, err := createWebSession(context.Background(), db, admin.ID)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/help", nil)
	req.AddCookie(&http.Cookie{Name: "skillsdex_session", Value: session})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("authenticated /help = %d, want 200", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		"Assistant Formats",
		"Portability checklist",
		".agents/skills/name/SKILL.md",
		"~/.codex/skills/owner--slug/SKILL.md",
		"Cursor project rule",
		"https://developers.openai.com/codex/skills",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("/help body missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "<table") {
		t.Fatalf("/help should render as responsive cards, found table markup")
	}
}

func TestHelpPageRejectsNonGet(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	admin, err := getUserByUsername(context.Background(), db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	session, err := createWebSession(context.Background(), db, admin.ID)
	if err != nil {
		t.Fatal(err)
	}

	a := &app{db: db}
	mux := http.NewServeMux()
	a.routes(mux)
	req := httptest.NewRequest(http.MethodPost, "/help", strings.NewReader(csrfForm(t, db, session, "")))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "skillsdex_session", Value: session})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /help = %d, want 405", w.Code)
	}
}

func TestWebPostRequiresCSRF(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	admin, err := getUserByUsername(context.Background(), db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	session, err := createWebSession(context.Background(), db, admin.ID)
	if err != nil {
		t.Fatal(err)
	}

	a := &app{db: db}
	mux := http.NewServeMux()
	a.routes(mux)
	req := httptest.NewRequest(http.MethodPost, "/tokens", strings.NewReader("name=local"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "skillsdex_session", Value: session})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("POST without csrf = %d, want 403", w.Code)
	}

	logoutReq := httptest.NewRequest(http.MethodPost, "/logout", strings.NewReader(csrfForm(t, db, session, "")))
	logoutReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	logoutReq.AddCookie(&http.Cookie{Name: "skillsdex_session", Value: session})
	logoutW := httptest.NewRecorder()
	mux.ServeHTTP(logoutW, logoutReq)
	if logoutW.Code != http.StatusFound || logoutW.Header().Get("Location") != "/login" {
		t.Fatalf("logout with csrf = %d location %q", logoutW.Code, logoutW.Header().Get("Location"))
	}
}

func TestValidCSRFBehaviorSpecs(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	admin, err := getUserByUsername(context.Background(), db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	session, err := createWebSession(context.Background(), db, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	token, err := csrfTokenForSession(context.Background(), db, session)
	if err != nil {
		t.Fatal(err)
	}
	a := &app{db: db}

	headerReq := httptest.NewRequest(http.MethodPost, "/tokens", nil)
	headerReq.Header.Set("X-CSRF-Token", token)
	if !a.validCSRF(headerReq, session) {
		t.Fatal("expected header CSRF token to be accepted")
	}

	mismatchReq := httptest.NewRequest(http.MethodPost, "/tokens", nil)
	mismatchReq.Header.Set("X-CSRF-Token", token+"extra")
	if a.validCSRF(mismatchReq, session) {
		t.Fatal("expected length-mismatched CSRF token to be rejected")
	}

	malformedFormReq := httptest.NewRequest(http.MethodPost, "/tokens", strings.NewReader("%zz"))
	malformedFormReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if a.validCSRF(malformedFormReq, session) {
		t.Fatal("expected malformed form body to be rejected")
	}

	malformedMultipartReq := httptest.NewRequest(http.MethodPost, "/import/restore", strings.NewReader("not multipart"))
	malformedMultipartReq.Header.Set("Content-Type", "multipart/form-data")
	if a.validCSRF(malformedMultipartReq, session) {
		t.Fatal("expected malformed multipart body to be rejected")
	}

	validReq := httptest.NewRequest(http.MethodPost, "/tokens", strings.NewReader("csrf_token="+url.QueryEscape(token)))
	validReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if a.validCSRF(validReq, "missing-session") {
		t.Fatal("expected unknown session CSRF token to be rejected")
	}
}

func TestAdminAuditLogRecordsActions(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	admin, err := getUserByUsername(ctx, db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	session, err := createWebSession(ctx, db, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	a := &app{db: db}
	mux := http.NewServeMux()
	a.routes(mux)

	tokenReq := httptest.NewRequest(http.MethodPost, "/tokens", strings.NewReader(csrfForm(t, db, session, "name=audit-cli")))
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tokenReq.AddCookie(&http.Cookie{Name: "skillsdex_session", Value: session})
	tokenW := httptest.NewRecorder()
	mux.ServeHTTP(tokenW, tokenReq)
	if tokenW.Code != http.StatusOK {
		t.Fatalf("create token = %d: %s", tokenW.Code, tokenW.Body.String())
	}
	tokenBody := tokenW.Body.String()
	rawToken := ""
	if start := strings.Index(tokenBody, `class="one-time-token"`); start >= 0 {
		snippet := tokenBody[start:]
		if codeStart := strings.Index(snippet, "<code>"); codeStart >= 0 {
			snippet = snippet[codeStart+len("<code>"):]
			if codeEnd := strings.Index(snippet, "</code>"); codeEnd >= 0 {
				rawToken = snippet[:codeEnd]
			}
		}
	}
	if rawToken == "" || !strings.HasPrefix(rawToken, "sdx_") {
		t.Fatalf("could not find raw token in creation page: %s", tokenBody)
	}

	skillBody := "owner=admin&slug=audit-skill&name=Audit+Skill&description=Use+when+testing+audit+events.&short_description=Audit+fixture&body=%23+Audit+Skill"
	skillReq := httptest.NewRequest(http.MethodPost, "/skills/new", strings.NewReader(csrfForm(t, db, session, skillBody)))
	skillReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	skillReq.AddCookie(&http.Cookie{Name: "skillsdex_session", Value: session})
	skillW := httptest.NewRecorder()
	mux.ServeHTTP(skillW, skillReq)
	if skillW.Code != http.StatusFound || skillW.Header().Get("Location") != "/skills/admin/audit-skill" {
		t.Fatalf("create skill = %d location %q body %s", skillW.Code, skillW.Header().Get("Location"), skillW.Body.String())
	}

	events, err := listAuditEvents(ctx, db, 20)
	if err != nil {
		t.Fatal(err)
	}
	var sawToken, sawSkill bool
	for _, event := range events {
		if event.Action == "api_token_created" && event.TargetLabel == "audit-cli" {
			sawToken = true
		}
		if event.Action == "skill_created" && event.TargetLabel == "admin/audit-skill" {
			sawSkill = true
		}
		if strings.Contains(event.DetailJSON, rawToken) {
			t.Fatalf("audit detail leaked raw token: %#v", event)
		}
	}
	if !sawToken || !sawSkill {
		t.Fatalf("missing audit events token=%v skill=%v events=%#v", sawToken, sawSkill, events)
	}

	auditReq := httptest.NewRequest(http.MethodGet, "/admin/audit", nil)
	auditReq.AddCookie(&http.Cookie{Name: "skillsdex_session", Value: session})
	auditW := httptest.NewRecorder()
	mux.ServeHTTP(auditW, auditReq)
	if auditW.Code != http.StatusOK {
		t.Fatalf("audit page = %d: %s", auditW.Code, auditW.Body.String())
	}
	for _, want := range []string{"Audit Log", "api_token_created", "skill_created", "admin/audit-skill"} {
		if !strings.Contains(auditW.Body.String(), want) {
			t.Fatalf("audit page missing %q:\n%s", want, auditW.Body.String())
		}
	}
	if strings.Contains(auditW.Body.String(), rawToken) {
		t.Fatal("audit page leaked raw token")
	}

	if err := createUser(ctx, db, "auditor", "", "password-1234", false); err != nil {
		t.Fatal(err)
	}
	auditor, err := getUserByUsername(ctx, db, "auditor")
	if err != nil {
		t.Fatal(err)
	}
	userSession, err := createWebSession(ctx, db, auditor.ID)
	if err != nil {
		t.Fatal(err)
	}
	userReq := httptest.NewRequest(http.MethodGet, "/admin/audit", nil)
	userReq.AddCookie(&http.Cookie{Name: "skillsdex_session", Value: userSession})
	userW := httptest.NewRecorder()
	mux.ServeHTTP(userW, userReq)
	if userW.Code != http.StatusForbidden {
		t.Fatalf("non-admin audit page = %d, want 403", userW.Code)
	}
}

func TestAdminMaintenancePrunesRetentionData(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	admin, err := getUserByUsername(ctx, db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	session, err := createWebSession(ctx, db, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	old := nowUTC().AddDate(0, 0, -60).Format(time.RFC3339)
	recent := nowUTC().AddDate(0, 0, -2).Format(time.RFC3339)
	if _, err := db.ExecContext(ctx, `INSERT INTO web_sessions (token, user_id, csrf_token, created_at, expires_at) VALUES (?, ?, ?, ?, ?)`, "expired-session", admin.ID, "expired-csrf", old, old); err != nil {
		t.Fatal(err)
	}

	oldRevoked, err := createAPIToken(ctx, db, admin.ID, "old-revoked")
	if err != nil {
		t.Fatal(err)
	}
	recentRevoked, err := createAPIToken(ctx, db, admin.ID, "recent-revoked")
	if err != nil {
		t.Fatal(err)
	}
	activeToken, err := createAPIToken(ctx, db, admin.ID, "active")
	if err != nil {
		t.Fatal(err)
	}
	for raw, revokedAt := range map[string]string{oldRevoked: old, recentRevoked: recent} {
		preview := tokenPreview(raw)
		if _, err := db.ExecContext(ctx, `UPDATE api_tokens SET revoked_at = ? WHERE token_preview = ?`, revokedAt, preview); err != nil {
			t.Fatal(err)
		}
	}

	if err := createAuditEvent(ctx, db, AuditEvent{ActorUserID: admin.ID, ActorUsername: admin.Username, Action: "old_event", TargetType: "test"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE audit_events SET created_at = ? WHERE action = ?`, old, "old_event"); err != nil {
		t.Fatal(err)
	}
	if err := createAuditEvent(ctx, db, AuditEvent{ActorUserID: admin.ID, ActorUsername: admin.Username, Action: "recent_event", TargetType: "test"}); err != nil {
		t.Fatal(err)
	}

	a := &app{db: db}
	mux := http.NewServeMux()
	a.routes(mux)

	getReq := httptest.NewRequest(http.MethodGet, "/admin/maintenance", nil)
	getReq.AddCookie(&http.Cookie{Name: "skillsdex_session", Value: session})
	getW := httptest.NewRecorder()
	mux.ServeHTTP(getW, getReq)
	if getW.Code != http.StatusOK {
		t.Fatalf("maintenance page = %d: %s", getW.Code, getW.Body.String())
	}
	for _, want := range []string{"Maintenance", "expired sessions", "revoked tokens older than 90 days", "audit events older than 180 days"} {
		if !strings.Contains(getW.Body.String(), want) {
			t.Fatalf("maintenance page missing %q:\n%s", want, getW.Body.String())
		}
	}

	noCSRF := httptest.NewRequest(http.MethodPost, "/admin/maintenance", strings.NewReader("audit_days=30&revoked_token_days=30"))
	noCSRF.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	noCSRF.AddCookie(&http.Cookie{Name: "skillsdex_session", Value: session})
	noCSRFW := httptest.NewRecorder()
	mux.ServeHTTP(noCSRFW, noCSRF)
	if noCSRFW.Code != http.StatusForbidden {
		t.Fatalf("maintenance post without csrf = %d, want 403", noCSRFW.Code)
	}

	postReq := httptest.NewRequest(http.MethodPost, "/admin/maintenance", strings.NewReader(csrfForm(t, db, session, "audit_days=30&revoked_token_days=30")))
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq.AddCookie(&http.Cookie{Name: "skillsdex_session", Value: session})
	postW := httptest.NewRecorder()
	mux.ServeHTTP(postW, postReq)
	if postW.Code != http.StatusOK {
		t.Fatalf("maintenance prune = %d: %s", postW.Code, postW.Body.String())
	}
	if !strings.Contains(postW.Body.String(), "Pruned 1 expired session(s), 1 revoked API token(s), 1 audit event(s), 0 old revision(s), and 0 revision file(s).") {
		t.Fatalf("maintenance prune summary missing:\n%s", postW.Body.String())
	}

	assertCount := func(query string, args ...any) int {
		t.Helper()
		var count int
		if err := db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	if got := assertCount(`SELECT COUNT(*) FROM web_sessions WHERE token = ?`, "expired-session"); got != 0 {
		t.Fatalf("expired session count = %d, want 0", got)
	}
	if _, err := userForSession(ctx, db, session); err != nil {
		t.Fatalf("active admin session should remain: %v", err)
	}
	if got := assertCount(`SELECT COUNT(*) FROM api_tokens WHERE token_preview = ?`, tokenPreview(oldRevoked)); got != 0 {
		t.Fatalf("old revoked token count = %d, want 0", got)
	}
	if got := assertCount(`SELECT COUNT(*) FROM api_tokens WHERE token_preview = ?`, tokenPreview(recentRevoked)); got != 1 {
		t.Fatalf("recent revoked token count = %d, want 1", got)
	}
	if _, err := userForAPIToken(ctx, db, activeToken); err != nil {
		t.Fatalf("active api token should remain usable: %v", err)
	}
	if got := assertCount(`SELECT COUNT(*) FROM audit_events WHERE action = ?`, "old_event"); got != 0 {
		t.Fatalf("old audit event count = %d, want 0", got)
	}
	if got := assertCount(`SELECT COUNT(*) FROM audit_events WHERE action = ?`, "recent_event"); got != 1 {
		t.Fatalf("recent audit event count = %d, want 1", got)
	}
	if got := assertCount(`SELECT COUNT(*) FROM audit_events WHERE action = ?`, "retention_pruned"); got != 1 {
		t.Fatalf("retention audit event count = %d, want 1", got)
	}

	if err := createUser(ctx, db, "maintainer", "", "password-1234", false); err != nil {
		t.Fatal(err)
	}
	user, err := getUserByUsername(ctx, db, "maintainer")
	if err != nil {
		t.Fatal(err)
	}
	userSession, err := createWebSession(ctx, db, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	userReq := httptest.NewRequest(http.MethodGet, "/admin/maintenance", nil)
	userReq.AddCookie(&http.Cookie{Name: "skillsdex_session", Value: userSession})
	userW := httptest.NewRecorder()
	mux.ServeHTTP(userW, userReq)
	if userW.Code != http.StatusForbidden {
		t.Fatalf("non-admin maintenance page = %d, want 403", userW.Code)
	}
}

func TestAPIImportRejectsUnsupportedSource(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	admin, err := getUserByUsername(context.Background(), db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	token, err := createAPIToken(context.Background(), db, admin.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	a := &app{db: db}
	mux := http.NewServeMux()
	a.routes(mux)

	req := httptest.NewRequest(http.MethodPost, "/v1/import", strings.NewReader(`{"source":"http://169.254.169.254/latest/meta-data/"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unsupported import source = %d, want 400: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "latest/meta-data") {
		t.Fatalf("error should not echo arbitrary internal URL: %s", w.Body.String())
	}
}

func TestDefaultAdminLoginRequiresPasswordChange(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	a := &app{db: db}
	mux := http.NewServeMux()
	a.routes(mux)

	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("username=admin&password=admin"))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginW := httptest.NewRecorder()
	mux.ServeHTTP(loginW, loginReq)
	if loginW.Code != http.StatusFound || loginW.Header().Get("Location") != "/setup/password" {
		t.Fatalf("admin login = %d location %q", loginW.Code, loginW.Header().Get("Location"))
	}
	var sessionCookie *http.Cookie
	for _, c := range loginW.Result().Cookies() {
		if c.Name == "skillsdex_session" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("login did not set session cookie")
	}

	blockedReq := httptest.NewRequest(http.MethodGet, "/skills", nil)
	blockedReq.AddCookie(sessionCookie)
	blockedW := httptest.NewRecorder()
	mux.ServeHTTP(blockedW, blockedReq)
	if blockedW.Code != http.StatusFound || blockedW.Header().Get("Location") != "/setup/password" {
		t.Fatalf("/skills before password change = %d location %q", blockedW.Code, blockedW.Header().Get("Location"))
	}

	pageReq := httptest.NewRequest(http.MethodGet, "/setup/password", nil)
	pageReq.AddCookie(sessionCookie)
	pageW := httptest.NewRecorder()
	mux.ServeHTTP(pageW, pageReq)
	if pageW.Code != http.StatusOK || !strings.Contains(pageW.Body.String(), "Change Password") {
		t.Fatalf("password page = %d body %s", pageW.Code, pageW.Body.String())
	}

	body := csrfForm(t, db, sessionCookie.Value, "current_password=admin&new_password=changed-password-123&confirm_password=changed-password-123")
	changeReq := httptest.NewRequest(http.MethodPost, "/setup/password", strings.NewReader(body))
	changeReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	changeReq.AddCookie(sessionCookie)
	changeW := httptest.NewRecorder()
	mux.ServeHTTP(changeW, changeReq)
	if changeW.Code != http.StatusFound || changeW.Header().Get("Location") != "/setup" {
		t.Fatalf("password change = %d location %q body %s", changeW.Code, changeW.Header().Get("Location"), changeW.Body.String())
	}
	admin, err := getUserByUsername(context.Background(), db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if admin.MustChangePassword {
		t.Fatal("admin must_change_password was not cleared")
	}

	skillsReq := httptest.NewRequest(http.MethodGet, "/skills", nil)
	skillsReq.AddCookie(sessionCookie)
	skillsW := httptest.NewRecorder()
	mux.ServeHTTP(skillsW, skillsReq)
	if skillsW.Code != http.StatusOK {
		t.Fatalf("/skills after password change = %d, want 200: %s", skillsW.Code, skillsW.Body.String())
	}
}

func TestLoginRateLimitBlocksPasswordGuessing(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	a := &app{db: db}
	mux := http.NewServeMux()
	a.routes(mux)

	for i := 0; i < loginFailureLimit; i++ {
		req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("username=admin&password=wrong-password"))
		req.RemoteAddr = "198.51.100.10:12345"
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("failed login %d = %d, want 200", i+1, w.Code)
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("username=admin&password=admin"))
	req.RemoteAddr = "198.51.100.10:12345"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("rate-limited login = %d, want 429", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Too many failed login attempts") {
		t.Fatalf("rate-limit page missing message:\n%s", w.Body.String())
	}
}

func TestAdminUserManagementHardening(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	admin, err := getUserByUsername(ctx, db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	adminSession, err := createWebSession(ctx, db, admin.ID)
	if err != nil {
		t.Fatal(err)
	}

	a := &app{db: db}
	mux := http.NewServeMux()
	a.routes(mux)

	lastAdminReq := httptest.NewRequest(http.MethodPost, "/admin/users/"+admin.ID, strings.NewReader(csrfForm(t, db, adminSession, "")))
	lastAdminReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	lastAdminReq.AddCookie(&http.Cookie{Name: "skillsdex_session", Value: adminSession})
	lastAdminW := httptest.NewRecorder()
	mux.ServeHTTP(lastAdminW, lastAdminReq)
	if lastAdminW.Code != http.StatusOK {
		t.Fatalf("last admin update = %d, want 200: %s", lastAdminW.Code, lastAdminW.Body.String())
	}
	if !strings.Contains(lastAdminW.Body.String(), "cannot remove or deactivate the last active admin") {
		t.Fatalf("last admin page missing error:\n%s", lastAdminW.Body.String())
	}

	if err := createUser(ctx, db, "alice", "", "alice-password-123", false); err != nil {
		t.Fatal(err)
	}
	alice, err := getUserByUsername(ctx, db, "alice")
	if err != nil {
		t.Fatal(err)
	}
	aliceSession, err := createWebSession(ctx, db, alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	aliceToken, err := createAPIToken(ctx, db, alice.ID, "web-admin")
	if err != nil {
		t.Fatal(err)
	}

	body := csrfForm(t, db, adminSession, "deactivated=on")
	deactivateReq := httptest.NewRequest(http.MethodPost, "/admin/users/"+alice.ID, strings.NewReader(body))
	deactivateReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	deactivateReq.AddCookie(&http.Cookie{Name: "skillsdex_session", Value: adminSession})
	deactivateW := httptest.NewRecorder()
	mux.ServeHTTP(deactivateW, deactivateReq)
	if deactivateW.Code != http.StatusFound || deactivateW.Header().Get("Location") != "/admin/users" {
		t.Fatalf("deactivate user = %d location %q body %s", deactivateW.Code, deactivateW.Header().Get("Location"), deactivateW.Body.String())
	}
	if _, err := userForSession(ctx, db, aliceSession); err == nil {
		t.Fatal("deactivated user's web session remained usable")
	}
	if _, err := userForAPIToken(ctx, db, aliceToken); err == nil {
		t.Fatal("deactivated user's API token remained usable")
	}

	createBody := csrfForm(t, db, adminSession, "username=deleteme&password=deleteme-password-123")
	createReq := httptest.NewRequest(http.MethodPost, "/admin/users", strings.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	createReq.AddCookie(&http.Cookie{Name: "skillsdex_session", Value: adminSession})
	createW := httptest.NewRecorder()
	mux.ServeHTTP(createW, createReq)
	if createW.Code != http.StatusFound || createW.Header().Get("Location") != "/admin/users" {
		t.Fatalf("create delete-test user = %d location %q body %s", createW.Code, createW.Header().Get("Location"), createW.Body.String())
	}
	deleteUserTarget, err := getUserByUsername(ctx, db, "deleteme")
	if err != nil {
		t.Fatal(err)
	}
	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("username=deleteme&password=deleteme-password-123"))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginW := httptest.NewRecorder()
	mux.ServeHTTP(loginW, loginReq)
	if loginW.Code != http.StatusFound || loginW.Header().Get("Location") != "/skills" {
		t.Fatalf("delete-test login = %d location %q body %s", loginW.Code, loginW.Header().Get("Location"), loginW.Body.String())
	}
	var deleteUserSession string
	for _, cookie := range loginW.Result().Cookies() {
		if cookie.Name == "skillsdex_session" {
			deleteUserSession = cookie.Value
		}
	}
	if deleteUserSession == "" {
		t.Fatal("delete-test login did not set session cookie")
	}
	deleteUserToken, err := createAPIToken(ctx, db, deleteUserTarget.ID, "delete-flow")
	if err != nil {
		t.Fatal(err)
	}
	skillBody := url.Values{
		"owner":       {"deleteme"},
		"slug":        {"temporary"},
		"name":        {"Temporary"},
		"description": {"Use when testing user deletion."},
		"body":        {"# Temporary\n\nDelete this with the user."},
	}.Encode()
	skillReq := httptest.NewRequest(http.MethodPost, "/skills/new", strings.NewReader(csrfForm(t, db, deleteUserSession, skillBody)))
	skillReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	skillReq.AddCookie(&http.Cookie{Name: "skillsdex_session", Value: deleteUserSession})
	skillW := httptest.NewRecorder()
	mux.ServeHTTP(skillW, skillReq)
	if skillW.Code != http.StatusFound || skillW.Header().Get("Location") != "/skills/deleteme/temporary" {
		t.Fatalf("delete-test skill create = %d location %q body %s", skillW.Code, skillW.Header().Get("Location"), skillW.Body.String())
	}
	deleteReq := httptest.NewRequest(http.MethodPost, "/admin/users/"+deleteUserTarget.ID+"/delete", strings.NewReader(csrfForm(t, db, adminSession, "")))
	deleteReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	deleteReq.AddCookie(&http.Cookie{Name: "skillsdex_session", Value: adminSession})
	deleteW := httptest.NewRecorder()
	mux.ServeHTTP(deleteW, deleteReq)
	if deleteW.Code != http.StatusFound || deleteW.Header().Get("Location") != "/admin/users" {
		t.Fatalf("delete user = %d location %q body %s", deleteW.Code, deleteW.Header().Get("Location"), deleteW.Body.String())
	}
	if _, err := getUserByUsername(ctx, db, "deleteme"); !isNotFound(err) {
		t.Fatalf("deleted user lookup err = %v, want not found", err)
	}
	if _, err := userForSession(ctx, db, deleteUserSession); err == nil {
		t.Fatal("deleted user's session remained usable")
	}
	if _, err := userForAPIToken(ctx, db, deleteUserToken); err == nil {
		t.Fatal("deleted user's API token remained usable")
	}
	var ownedSkills int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM skills WHERE owner_id = ?`, deleteUserTarget.ID).Scan(&ownedSkills); err != nil {
		t.Fatal(err)
	}
	if ownedSkills != 0 {
		t.Fatalf("deleted user's skills = %d, want 0", ownedSkills)
	}
}

func TestAPITokenPageShowsNewTokenOnce(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	admin, err := getUserByUsername(context.Background(), db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	session, err := createWebSession(context.Background(), db, admin.ID)
	if err != nil {
		t.Fatal(err)
	}

	a := &app{db: db}
	mux := http.NewServeMux()
	a.routes(mux)
	createReq := httptest.NewRequest(http.MethodPost, "/tokens", strings.NewReader(csrfForm(t, db, session, "name=local")))
	createReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	createReq.AddCookie(&http.Cookie{Name: "skillsdex_session", Value: session})
	createW := httptest.NewRecorder()
	mux.ServeHTTP(createW, createReq)
	if createW.Code != http.StatusOK {
		t.Fatalf("create token page = %d: %s", createW.Code, createW.Body.String())
	}
	body := createW.Body.String()
	if !strings.Contains(body, "New token") || !strings.Contains(body, "Copy the new token now") {
		t.Fatalf("new token page missing one-time notice:\n%s", body)
	}
	var rawToken string
	if blockIdx := strings.Index(body, "one-time-token"); blockIdx >= 0 {
		block := body[blockIdx:]
		if idx := strings.Index(block, "sdx_"); idx >= 0 {
			rawToken = block[idx:]
			if end := strings.Index(rawToken, "<"); end >= 0 {
				rawToken = rawToken[:end]
			}
		}
	}
	if rawToken == "" {
		t.Fatalf("new token page did not include raw token:\n%s", body)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/tokens", nil)
	getReq.AddCookie(&http.Cookie{Name: "skillsdex_session", Value: session})
	getW := httptest.NewRecorder()
	mux.ServeHTTP(getW, getReq)
	if getW.Code != http.StatusOK {
		t.Fatalf("tokens page = %d: %s", getW.Code, getW.Body.String())
	}
	if strings.Contains(getW.Body.String(), rawToken) {
		t.Fatalf("tokens page showed raw token after refresh:\n%s", getW.Body.String())
	}
	if !strings.Contains(getW.Body.String(), tokenPreview(rawToken)) {
		t.Fatalf("tokens page missing token preview %q:\n%s", tokenPreview(rawToken), getW.Body.String())
	}
}

func TestSetupPageRendersFirstRunChecklist(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	admin, err := getUserByUsername(ctx, db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := createAPIToken(ctx, db, admin.ID, "test"); err != nil {
		t.Fatal(err)
	}
	session, err := createWebSession(ctx, db, admin.ID)
	if err != nil {
		t.Fatal(err)
	}

	a := &app{db: db}
	mux := http.NewServeMux()
	a.routes(mux)
	req := httptest.NewRequest(http.MethodGet, "/setup", nil)
	req.AddCookie(&http.Cookie{Name: "skillsdex_session", Value: session})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("/setup = %d, want 200: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{
		"Setup",
		"Build a useful library",
		"Generate a CLI token",
		"SKILLSDEX_TOKEN",
		"token-stdin",
		"skillsdex config set default_target codex",
		"skillsdex sync --dry-run --strict",
		"skillsdex install-pinned",
		"skillsdex installed --strict",
		"skillsdex refresh --all --strict",
		"Check quality",
		"skillsdex lint --all --strict",
		"Back up",
		"skillsdex backup export --out ./skillsdex-backup.zip",
		"skillsdex backup restore --file ./skillsdex-backup.zip --dry-run",
		"active token",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("/setup body missing %q:\n%s", want, body)
		}
	}
}

func TestNewSkillTemplatePrefillsEditor(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	admin, err := getUserByUsername(context.Background(), db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	session, err := createWebSession(context.Background(), db, admin.ID)
	if err != nil {
		t.Fatal(err)
	}

	a := &app{db: db}
	mux := http.NewServeMux()
	a.routes(mux)
	req := httptest.NewRequest(http.MethodGet, "/skills/new?template=review", nil)
	req.AddCookie(&http.Cookie{Name: "skillsdex_session", Value: session})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("template editor = %d, want 200: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{
		"focused-review",
		"Focused Review",
		"Quality Signals",
		"Review changes for concrete risks",
		`data-skill-editor`,
		`data-skill-body`,
		`data-skill-preview-raw`,
		`data-skill-preview-rendered`,
		`data-quality-panel`,
		`data-quality-summary`,
		`data-quality-list`,
		`refreshRenderedPreview`,
		`refreshQuality`,
		`localQualityIssues`,
		`replaceChildren`,
		`Body: `,
		`Description: `,
		`title="Use when a repeatable workflow needs consistent steps and outputs."`,
		`href="/skills/new?template=research"`,
		`name="csrf_token"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("template editor missing %q:\n%s", want, body)
		}
	}
}

func TestInstallSetWebAndAPI(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	admin, err := getUserByUsername(ctx, db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	skill, err := createSkill(ctx, db, admin, SkillInput{
		Owner:       "admin",
		Slug:        "set-web-skill",
		Name:        "Set Web Skill",
		Description: "Use when testing install set web management.",
		Body:        "# Set Web Skill\n\nInstructions.",
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := createWebSession(ctx, db, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	token, err := createAPIToken(ctx, db, admin.ID, "test")
	if err != nil {
		t.Fatal(err)
	}

	a := &app{db: db}
	mux := http.NewServeMux()
	a.routes(mux)

	createReq := httptest.NewRequest(http.MethodPost, "/sets", strings.NewReader(csrfForm(t, db, session, "slug=daily&name=Daily&description=Daily+driver")))
	createReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	createReq.AddCookie(&http.Cookie{Name: "skillsdex_session", Value: session})
	createW := httptest.NewRecorder()
	mux.ServeHTTP(createW, createReq)
	if createW.Code != http.StatusFound || createW.Header().Get("Location") != "/sets/daily" {
		t.Fatalf("create set = %d location %q", createW.Code, createW.Header().Get("Location"))
	}

	editBody := csrfForm(t, db, session, "name=Daily&description=Daily+driver&skill_id="+skill.ID)
	editReq := httptest.NewRequest(http.MethodPost, "/sets/daily", strings.NewReader(editBody))
	editReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	editReq.AddCookie(&http.Cookie{Name: "skillsdex_session", Value: session})
	editW := httptest.NewRecorder()
	mux.ServeHTTP(editW, editReq)
	if editW.Code != http.StatusFound || editW.Header().Get("Location") != "/sets/daily" {
		t.Fatalf("edit set = %d location %q body %s", editW.Code, editW.Header().Get("Location"), editW.Body.String())
	}

	pageReq := httptest.NewRequest(http.MethodGet, "/sets/daily", nil)
	pageReq.AddCookie(&http.Cookie{Name: "skillsdex_session", Value: session})
	pageW := httptest.NewRecorder()
	mux.ServeHTTP(pageW, pageReq)
	if pageW.Code != http.StatusOK {
		t.Fatalf("set page = %d: %s", pageW.Code, pageW.Body.String())
	}
	for _, want := range []string{"Daily", "set-web-skill", "skillsdex config set default_target codex", "skillsdex install-set daily", "checked"} {
		if !strings.Contains(pageW.Body.String(), want) {
			t.Fatalf("set page missing %q:\n%s", want, pageW.Body.String())
		}
	}

	apiReq := httptest.NewRequest(http.MethodGet, "/v1/install-sets/daily", nil)
	apiReq.Header.Set("Authorization", "Bearer "+token)
	apiW := httptest.NewRecorder()
	mux.ServeHTTP(apiW, apiReq)
	if apiW.Code != http.StatusOK {
		t.Fatalf("api set = %d: %s", apiW.Code, apiW.Body.String())
	}
	var payload struct {
		Set    InstallSet `json:"set"`
		Skills []Skill    `json:"skills"`
	}
	if err := json.Unmarshal(apiW.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Set.Slug != "daily" || len(payload.Skills) != 1 || payload.Skills[0].Slug != skill.Slug {
		t.Fatalf("unexpected api payload: %#v", payload)
	}
}

func TestSkillAPIIncludesSupportingFiles(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	admin, err := getUserByUsername(ctx, db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := createSkill(ctx, db, admin, SkillInput{
		Owner:       "admin",
		Slug:        "api-folder-skill",
		Name:        "API Folder Skill",
		Description: "Use when testing API package files.",
		Body:        "# API Folder Skill\n\nInstructions.",
		Files: []SkillFile{
			{Path: "references/guide.md", Content: "# Guide\n"},
			{Path: "scripts/run.sh", Content: "#!/bin/sh\n", Executable: true},
		},
	}); err != nil {
		t.Fatal(err)
	}
	token, err := createAPIToken(ctx, db, admin.ID, "test")
	if err != nil {
		t.Fatal(err)
	}

	a := &app{db: db}
	mux := http.NewServeMux()
	a.routes(mux)
	req := httptest.NewRequest(http.MethodGet, "/v1/skills/admin/api-folder-skill", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("api skill = %d: %s", w.Code, w.Body.String())
	}
	var payload skillResponse
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Skill.Slug != "api-folder-skill" || !strings.Contains(payload.Raw, "# API Folder Skill") {
		t.Fatalf("unexpected skill payload: %#v", payload)
	}
	seen := map[string]SkillFile{}
	for _, file := range payload.Files {
		seen[file.Path] = file
	}
	if seen["SKILL.md"].Content == "" || seen["references/guide.md"].Content != "# Guide\n" || !seen["scripts/run.sh"].Executable {
		t.Fatalf("supporting files missing from API payload: %#v", payload.Files)
	}
}

func TestSkillAPIListsAndFiltersTags(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	admin, err := getUserByUsername(ctx, db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := createSkill(ctx, db, admin, SkillInput{
		Owner:       "admin",
		Slug:        "go-review",
		Name:        "Go Review",
		Description: "Use when reviewing Go code.",
		Tags:        []string{"go", "testing"},
		Body:        "# Go Review\n\nReview Go code.",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := createSkill(ctx, db, admin, SkillInput{
		Owner:       "admin",
		Slug:        "python-review",
		Name:        "Python Review",
		Description: "Use when reviewing Python code.",
		Tags:        []string{"python"},
		Body:        "# Python Review\n\nReview Python code.",
	}); err != nil {
		t.Fatal(err)
	}
	token, err := createAPIToken(ctx, db, admin.ID, "test")
	if err != nil {
		t.Fatal(err)
	}

	a := &app{db: db}
	mux := http.NewServeMux()
	a.routes(mux)
	req := httptest.NewRequest(http.MethodGet, "/v1/skills?tag=go", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("api skills tag filter = %d: %s", w.Code, w.Body.String())
	}
	var payload struct {
		Skills []Skill `json:"skills"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Skills) != 1 || payload.Skills[0].Slug != "go-review" || strings.Join(payload.Skills[0].Tags, ",") != "go,testing" {
		t.Fatalf("unexpected filtered skills payload: %#v", payload)
	}

	badReq := httptest.NewRequest(http.MethodGet, "/v1/skills?tag=bad_tag", nil)
	badReq.Header.Set("Authorization", "Bearer "+token)
	badW := httptest.NewRecorder()
	mux.ServeHTTP(badW, badReq)
	if badW.Code != http.StatusBadRequest {
		t.Fatalf("invalid tag filter status = %d, want 400", badW.Code)
	}
}

func TestSkillExportDumpsOwnedSkills(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	admin, err := getUserByUsername(ctx, db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := createSkill(ctx, db, admin, SkillInput{
		Owner:          "admin",
		Slug:           "exported-skill",
		Name:           "Exported Skill",
		Description:    "Use when testing export.",
		SourceProvider: "github",
		SourceURL:      "https://github.com/acme/skills/tree/main/exported-skill",
		SourceAuthor:   "acme",
		SourceName:     "skills",
		SourcePath:     "exported-skill/SKILL.md",
		SourceRef:      "main",
		SourceExplicit: true,
		Tags:           []string{"go", "testing"},
		Body:           "# Exported Skill\n\nInstructions.",
		Files:          []SkillFile{{Path: "references/guide.md", Content: "# Guide\n"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := archiveSkill(ctx, db, admin.ID, "admin", "exported-skill"); err != nil {
		t.Fatal(err)
	}
	if err := createUser(ctx, db, "alice", "", "secret-123456", false); err != nil {
		t.Fatal(err)
	}
	alice, err := getUserByUsername(ctx, db, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := createSkill(ctx, db, alice, SkillInput{
		Owner:       "alice",
		Slug:        "private",
		Name:        "Private",
		Description: "Use privately.",
		Body:        "# Private",
	}); err != nil {
		t.Fatal(err)
	}
	session, err := createWebSession(ctx, db, admin.ID)
	if err != nil {
		t.Fatal(err)
	}

	a := &app{db: db}
	mux := http.NewServeMux()
	a.routes(mux)
	req := httptest.NewRequest(http.MethodGet, "/skills/export", nil)
	req.AddCookie(&http.Cookie{Name: "skillsdex_session", Value: session})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("export = %d, want 200: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/zip" {
		t.Fatalf("content type = %q, want application/zip", ct)
	}

	zr, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(rc); err != nil {
			rc.Close()
			t.Fatal(err)
		}
		rc.Close()
		entries[f.Name] = buf.String()
	}
	raw, ok := entries["admin--exported-skill/SKILL.md"]
	if !ok || !strings.Contains(raw, "# Exported Skill") {
		t.Fatalf("exported skill missing from zip: %#v", entries)
	}
	if _, ok := entries["alice--private/SKILL.md"]; ok {
		t.Fatal("export included another user's skill")
	}
	if entries["admin--exported-skill/references/guide.md"] != "# Guide\n" {
		t.Fatalf("export missing supporting file: %#v", entries)
	}
	var manifest exportManifest
	if err := json.Unmarshal([]byte(entries["skillsdex-export.json"]), &manifest); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, skill := range manifest.Skills {
		if skill.Identifier == "admin/exported-skill" {
			found = true
			if !skill.Archived || skill.SourceProvider != "github" || skill.SourceURL == "" || skill.SourceAuthor != "acme" || skill.SourceName != "skills" || skill.SourcePath != "exported-skill/SKILL.md" || skill.SourceRef != "main" || skill.Path != "admin--exported-skill/SKILL.md" || strings.Join(skill.Tags, ",") != "go,testing" {
				t.Fatalf("unexpected manifest skill: %#v", skill)
			}
		}
		if strings.HasPrefix(skill.Identifier, "alice/") {
			t.Fatalf("manifest included another user's skill: %#v", skill)
		}
	}
	if !found {
		t.Fatalf("manifest missing exported skill: %#v", manifest.Skills)
	}
}

func TestSkillGitHubExportUsesRepoShape(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	admin, err := getUserByUsername(ctx, db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := createSkill(ctx, db, admin, SkillInput{
		Owner:       "admin",
		Slug:        "repo-skill",
		Name:        "Repo Skill",
		Description: "Use when testing repo-shaped export.",
		Body:        "# Repo Skill\n\nInstructions.",
		Files:       []SkillFile{{Path: "references/guide.md", Content: "# Guide\n"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := createUser(ctx, db, "alice", "", "secret-123456", false); err != nil {
		t.Fatal(err)
	}
	alice, err := getUserByUsername(ctx, db, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := createSkill(ctx, db, alice, SkillInput{
		Owner:       "alice",
		Slug:        "private",
		Name:        "Private",
		Description: "Use privately.",
		Body:        "# Private",
	}); err != nil {
		t.Fatal(err)
	}
	session, err := createWebSession(ctx, db, admin.ID)
	if err != nil {
		t.Fatal(err)
	}

	a := &app{db: db}
	mux := http.NewServeMux()
	a.routes(mux)
	req := httptest.NewRequest(http.MethodGet, "/skills/export/github", nil)
	req.AddCookie(&http.Cookie{Name: "skillsdex_session", Value: session})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("github export = %d, want 200: %s", w.Code, w.Body.String())
	}
	zr, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(rc); err != nil {
			rc.Close()
			t.Fatal(err)
		}
		rc.Close()
		entries[f.Name] = buf.String()
	}
	if raw := entries["skills/admin/repo-skill/SKILL.md"]; !strings.Contains(raw, "# Repo Skill") {
		t.Fatalf("missing repo-shaped skill export: %#v", entries)
	}
	if entries["skills/admin/repo-skill/references/guide.md"] != "# Guide\n" {
		t.Fatalf("missing repo-shaped supporting file: %#v", entries)
	}
	if _, ok := entries["skills/alice/private/SKILL.md"]; ok {
		t.Fatal("github export included another user's skill")
	}
	if readme := entries["README.md"]; !strings.Contains(readme, "admin/repo-skill") || strings.Contains(readme, "alice/private") {
		t.Fatalf("unexpected README export:\n%s", readme)
	}
}

func TestAPIBackupExportAndRestore(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	admin, err := getUserByUsername(ctx, db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := createSkill(ctx, db, admin, SkillInput{
		Owner:              "admin",
		Slug:               "api-backup",
		Name:               "API Backup",
		Description:        "Use when testing API backup export and restore.",
		ShortDescription:   "Exercise API backups",
		RecommendedDefault: true,
		PinnedInstall:      true,
		Body:               "# API Backup\n\nInstructions.",
		Files:              []SkillFile{{Path: "references/guide.md", Content: "# Guide\n"}},
	}); err != nil {
		t.Fatal(err)
	}
	token, err := createAPIToken(ctx, db, admin.ID, "backup-api")
	if err != nil {
		t.Fatal(err)
	}

	a := &app{db: db}
	mux := http.NewServeMux()
	a.routes(mux)
	exportReq := httptest.NewRequest(http.MethodGet, "/v1/backup", nil)
	exportReq.Header.Set("Authorization", "Bearer "+token)
	exportW := httptest.NewRecorder()
	mux.ServeHTTP(exportW, exportReq)
	if exportW.Code != http.StatusOK {
		t.Fatalf("api backup export = %d: %s", exportW.Code, exportW.Body.String())
	}
	if exportW.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("backup export content type = %q", exportW.Header().Get("Content-Type"))
	}

	if err := createUser(ctx, db, "restore-cli", "", "password-1234", false); err != nil {
		t.Fatal(err)
	}
	restoreUser, err := getUserByUsername(ctx, db, "restore-cli")
	if err != nil {
		t.Fatal(err)
	}
	restoreToken, err := createAPIToken(ctx, db, restoreUser.ID, "restore-api")
	if err != nil {
		t.Fatal(err)
	}
	restoreReq := httptest.NewRequest(http.MethodPost, "/v1/backup", bytes.NewReader(exportW.Body.Bytes()))
	restoreReq.Header.Set("Authorization", "Bearer "+restoreToken)
	restoreReq.Header.Set("Content-Type", "application/zip")
	restoreW := httptest.NewRecorder()
	mux.ServeHTTP(restoreW, restoreReq)
	if restoreW.Code != http.StatusOK {
		t.Fatalf("api backup restore = %d: %s", restoreW.Code, restoreW.Body.String())
	}
	var payload importSourceResponse
	if err := json.Unmarshal(restoreW.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Results) == 0 {
		t.Fatalf("restore returned no results: %#v", payload)
	}
	restored, err := getSkill(ctx, db, restoreUser.ID, "restore-cli", "api-backup")
	if err != nil {
		t.Fatal(err)
	}
	if restored.Name != "API Backup" || !restored.RecommendedDefault || !restored.PinnedInstall {
		t.Fatalf("unexpected restored skill: %#v", restored)
	}
	files, _, err := currentSkillFiles(ctx, db, restoreUser.ID, "restore-cli", "api-backup")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]SkillFile{}
	for _, file := range files {
		seen[file.Path] = file
	}
	if seen["references/guide.md"].Content != "# Guide\n" {
		t.Fatalf("restored supporting file missing: %#v", files)
	}
	events, err := listAuditEvents(ctx, db, 20)
	if err != nil {
		t.Fatal(err)
	}
	var sawExport, sawRestore bool
	for _, event := range events {
		if event.Action == "api_backup_exported" {
			sawExport = true
		}
		if event.Action == "api_backup_restored" && event.ActorUsername == "restore-cli" {
			sawRestore = true
		}
	}
	if !sawExport || !sawRestore {
		t.Fatalf("missing backup audit events export=%v restore=%v events=%#v", sawExport, sawRestore, events)
	}
}

func TestAPIUpdateSkillFlagsDoesNotCreateRevision(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	admin, err := getUserByUsername(ctx, db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	created, err := createSkill(ctx, db, admin, SkillInput{
		Owner:            "admin",
		Slug:             "flagged",
		Name:             "Flagged",
		Description:      "Use when testing metadata-only skill flag updates.",
		ShortDescription: "Test skill flags",
		Body:             "# Flagged\n\nInstructions.",
	})
	if err != nil {
		t.Fatal(err)
	}
	token, err := createAPIToken(ctx, db, admin.ID, "flags-api")
	if err != nil {
		t.Fatal(err)
	}

	a := &app{db: db}
	mux := http.NewServeMux()
	a.routes(mux)
	req := httptest.NewRequest(http.MethodPut, "/v1/skills/admin/flagged/flags", strings.NewReader(`{"recommended_default":true,"pinned_install":true}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("flag update = %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Skill Skill `json:"skill"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Skill.RecommendedDefault || !resp.Skill.PinnedInstall {
		t.Fatalf("response flags = recommended %v pinned %v, want true/true", resp.Skill.RecommendedDefault, resp.Skill.PinnedInstall)
	}
	updated, err := getSkill(ctx, db, admin.ID, "admin", "flagged")
	if err != nil {
		t.Fatal(err)
	}
	if !updated.RecommendedDefault || !updated.PinnedInstall {
		t.Fatalf("stored flags = recommended %v pinned %v, want true/true", updated.RecommendedDefault, updated.PinnedInstall)
	}
	if updated.RevisionNumber != created.RevisionNumber || updated.CurrentRevisionID != created.CurrentRevisionID {
		t.Fatalf("flag update changed revision from %d/%s to %d/%s", created.RevisionNumber, created.CurrentRevisionID, updated.RevisionNumber, updated.CurrentRevisionID)
	}
}

func TestSkillImportRestoresSkillsDexExport(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	admin, err := getUserByUsername(ctx, db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := createSkill(ctx, db, admin, SkillInput{
		Owner:              "admin",
		Slug:               "backup-skill",
		Name:               "Backup Skill",
		Description:        "Use when restoring a SkillsDex backup.",
		ShortDescription:   "Restore backup fixtures",
		RecommendedDefault: true,
		PinnedInstall:      true,
		SourceProvider:     "github",
		SourceURL:          "https://github.com/example/skills/tree/main/backup-skill",
		SourceAuthor:       "example",
		SourceName:         "skills",
		SourcePath:         "backup-skill/SKILL.md",
		SourceRef:          "main",
		SourceExplicit:     true,
		Tags:               []string{"backup", "testing"},
		Body:               "# Backup Skill\n\nRestore this from a zip.",
		Files:              []SkillFile{{Path: "references/restore.md", Content: "# Restore\n"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := archiveSkill(ctx, db, admin.ID, "admin", "backup-skill"); err != nil {
		t.Fatal(err)
	}
	backup, err := buildSkillsExport(ctx, db, admin)
	if err != nil {
		t.Fatal(err)
	}
	if err := createUser(ctx, db, "restore", "", "password-1234", false); err != nil {
		t.Fatal(err)
	}
	restoreUser, err := getUserByUsername(ctx, db, "restore")
	if err != nil {
		t.Fatal(err)
	}
	session, err := createWebSession(ctx, db, restoreUser.ID)
	if err != nil {
		t.Fatal(err)
	}

	a := &app{db: db}
	mux := http.NewServeMux()
	a.routes(mux)
	body, contentType := csrfMultipart(t, db, session, backup)
	req := httptest.NewRequest(http.MethodPost, "/skills/import", body)
	req.Header.Set("Content-Type", contentType)
	req.AddCookie(&http.Cookie{Name: "skillsdex_session", Value: session})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("restore import = %d, want 200: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "restore/backup-skill") || !strings.Contains(w.Body.String(), "created and archived") {
		t.Fatalf("restore result missing backup skill: %s", w.Body.String())
	}

	restored, err := getSkill(ctx, db, restoreUser.ID, "restore", "backup-skill")
	if err != nil {
		t.Fatal(err)
	}
	if restored.Name != "Backup Skill" || !restored.RecommendedDefault || !restored.PinnedInstall || restored.SourceProvider != "github" || restored.SourceURL == "" || restored.SourceAuthor != "example" || restored.SourceName != "skills" || restored.SourcePath != "backup-skill/SKILL.md" || restored.SourceRef != "main" || restored.ArchivedAt == "" || strings.Join(restored.Tags, ",") != "backup,testing" {
		t.Fatalf("unexpected restored skill: %#v", restored)
	}
	raw, _, err := rawSkill(ctx, db, restoreUser.ID, "restore", "backup-skill")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "# Backup Skill") {
		t.Fatalf("restored raw skill missing body: %s", string(raw))
	}
	files, _, err := currentSkillFiles(ctx, db, restoreUser.ID, "restore", "backup-skill")
	if err != nil {
		t.Fatal(err)
	}
	foundSupport := false
	for _, file := range files {
		if file.Path == "references/restore.md" && file.Content == "# Restore\n" {
			foundSupport = true
		}
	}
	if !foundSupport {
		t.Fatalf("restored supporting file missing: %#v", files)
	}
}

func TestSkillDetailPreviewsSupportingFiles(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	admin, err := getUserByUsername(ctx, db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := createSkill(ctx, db, admin, SkillInput{
		Owner:       "admin",
		Slug:        "preview-files",
		Name:        "Preview Files",
		Description: "Use when previewing package files.",
		Body:        "# Preview Files\n\nInstructions.",
		Files: []SkillFile{
			{Path: "references/guide.md", Content: "# Guide\n\n<script>alert(1)</script>\n"},
			{Path: "references/example.json", Content: `{"ok":true}`},
		},
	}); err != nil {
		t.Fatal(err)
	}
	session, err := createWebSession(ctx, db, admin.ID)
	if err != nil {
		t.Fatal(err)
	}

	a := &app{db: db}
	mux := http.NewServeMux()
	a.routes(mux)
	req := httptest.NewRequest(http.MethodGet, "/skills/admin/preview-files", nil)
	req.AddCookie(&http.Cookie{Name: "skillsdex_session", Value: session})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("detail page = %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{"Supporting Files", "references/guide.md", "# Guide", "references/example.json", `{&#34;ok&#34;:true}`} {
		if !strings.Contains(body, want) {
			t.Fatalf("detail page missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "<script>alert(1)</script>") || !strings.Contains(body, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Fatalf("supporting file preview was not escaped:\n%s", body)
	}
}

func TestSkillDetailShowsContextualQualitySignals(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	admin, err := getUserByUsername(ctx, db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	body := "# Review\n\nInspect the goal, affected files, tests, and behavior. Report confirmed findings first with file references and severity. Keep questions separate from bugs."
	if _, err := createSkill(ctx, db, admin, SkillInput{
		Owner:            "admin",
		Slug:             "source-review",
		Name:             "Source Review",
		Description:      "Use when reviewing a change for bugs and regressions.",
		ShortDescription: "Review changes for concrete risks",
		SourceProvider:   "github",
		SourceURL:        "https://github.com/example/skills/tree/main/source-review",
		SourceExplicit:   true,
		Body:             body,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := createSkill(ctx, db, admin, SkillInput{
		Owner:            "admin",
		Slug:             "duplicate-review",
		Name:             "Duplicate Review",
		Description:      "Use when reviewing a change for bugs and regressions.",
		ShortDescription: "Review changes for concrete risks",
		Body:             body,
	}); err != nil {
		t.Fatal(err)
	}
	old := nowUTC().AddDate(0, 0, -120).Format(time.RFC3339)
	if _, err := db.Exec(`UPDATE skills SET updated_at = ? WHERE owner_slug = 'admin' AND slug = 'source-review'`, old); err != nil {
		t.Fatal(err)
	}
	session, err := createWebSession(ctx, db, admin.ID)
	if err != nil {
		t.Fatal(err)
	}

	a := &app{db: db}
	mux := http.NewServeMux()
	a.routes(mux)
	req := httptest.NewRequest(http.MethodGet, "/skills/admin/source-review", nil)
	req.AddCookie(&http.Cookie{Name: "skillsdex_session", Value: session})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("detail page = %d: %s", w.Code, w.Body.String())
	}
	bodyHTML := w.Body.String()
	for _, want := range []string{"Possible duplicate skill", "admin/duplicate-review", "Source may be stale", "Refresh Source"} {
		if !strings.Contains(bodyHTML, want) {
			t.Fatalf("detail page missing %q:\n%s", want, bodyHTML)
		}
	}
}

func TestSkillRefreshPreviewRequiresApply(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	admin, err := getUserByUsername(ctx, db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	created, err := createSkill(ctx, db, admin, SkillInput{
		Owner:          "admin",
		Slug:           "source-review",
		Name:           "source-review",
		Description:    "Use when reviewing upstream source refresh.",
		SourceProvider: "agentskill.sh",
		SourceURL:      "https://agentskill.sh/@openclaw/source-review",
		SourceExplicit: true,
		Body:           "# Source Review\n\nOld instructions.",
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := createWebSession(ctx, db, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://agentskill.sh/api/agent/skills/openclaw%2Fsource-review/install" {
			t.Fatalf("unexpected request: %s", r.URL.String())
		}
		return testResponse(http.StatusOK, `{
			"slug": "openclaw/source-review",
			"name": "source-review",
			"description": "Use when reviewing upstream source refresh.",
			"skillMd": "---\nname: source-review\ndescription: Use when reviewing upstream source refresh.\n---\n\n# Source Review\n\nNew instructions.\n"
		}`), nil
	})}
	a := &app{db: db, httpClient: client}
	mux := http.NewServeMux()
	a.routes(mux)

	getReq := httptest.NewRequest(http.MethodGet, "/skills/admin/source-review/refresh", nil)
	getReq.AddCookie(&http.Cookie{Name: "skillsdex_session", Value: session})
	getW := httptest.NewRecorder()
	mux.ServeHTTP(getW, getReq)
	if getW.Code != http.StatusOK {
		t.Fatalf("refresh preview = %d: %s", getW.Code, getW.Body.String())
	}
	previewHTML := getW.Body.String()
	for _, want := range []string{"Source refresh", "update available", "SKILL.md", "modified", "Apply Source Update", "skillsdex sync --dry-run --strict", "- Old instructions.", "+ New instructions."} {
		if !strings.Contains(previewHTML, want) {
			t.Fatalf("preview missing %q:\n%s", want, previewHTML)
		}
	}
	afterPreview, err := getSkill(ctx, db, admin.ID, "admin", "source-review")
	if err != nil {
		t.Fatal(err)
	}
	if afterPreview.RevisionNumber != created.RevisionNumber {
		t.Fatalf("preview changed revision = %d, want %d", afterPreview.RevisionNumber, created.RevisionNumber)
	}

	postReq := httptest.NewRequest(http.MethodPost, "/skills/admin/source-review/refresh", strings.NewReader(csrfForm(t, db, session, "")))
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq.AddCookie(&http.Cookie{Name: "skillsdex_session", Value: session})
	postW := httptest.NewRecorder()
	mux.ServeHTTP(postW, postReq)
	if postW.Code != http.StatusFound || postW.Header().Get("Location") != "/skills/admin/source-review" {
		t.Fatalf("refresh apply = %d location %q body %s", postW.Code, postW.Header().Get("Location"), postW.Body.String())
	}
	afterApply, err := getSkill(ctx, db, admin.ID, "admin", "source-review")
	if err != nil {
		t.Fatal(err)
	}
	if afterApply.RevisionNumber != created.RevisionNumber+1 {
		t.Fatalf("apply revision = %d, want %d", afterApply.RevisionNumber, created.RevisionNumber+1)
	}
}

func TestSkillEditUpdatesSupportingFiles(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	admin, err := getUserByUsername(ctx, db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := createSkill(ctx, db, admin, SkillInput{
		Owner:       "admin",
		Slug:        "edit-files",
		Name:        "Edit Files",
		Description: "Use when editing package files.",
		Body:        "# Edit Files\n\nInstructions.",
		Files: []SkillFile{
			{Path: "references/guide.md", Content: "# Guide\n\nFirst.\n"},
			{Path: "templates/old.txt", Content: "remove me\n"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	session, err := createWebSession(ctx, db, admin.ID)
	if err != nil {
		t.Fatal(err)
	}

	a := &app{db: db}
	mux := http.NewServeMux()
	a.routes(mux)
	getReq := httptest.NewRequest(http.MethodGet, "/skills/admin/edit-files/edit", nil)
	getReq.AddCookie(&http.Cookie{Name: "skillsdex_session", Value: session})
	getW := httptest.NewRecorder()
	mux.ServeHTTP(getW, getReq)
	if getW.Code != http.StatusOK {
		t.Fatalf("edit page = %d: %s", getW.Code, getW.Body.String())
	}
	for _, want := range []string{"Supporting Files", "references/guide.md", "templates/old.txt", "file_count"} {
		if !strings.Contains(getW.Body.String(), want) {
			t.Fatalf("edit page missing %q:\n%s", want, getW.Body.String())
		}
	}

	values := url.Values{}
	token, err := csrfTokenForSession(ctx, db, session)
	if err != nil {
		t.Fatal(err)
	}
	values.Set("csrf_token", token)
	values.Set("owner", "admin")
	values.Set("slug", "edit-files")
	values.Set("name", "Edit Files")
	values.Set("description", "Use when editing package files.")
	values.Set("short_description", "Edit package files")
	values.Set("body", "# Edit Files\n\nUpdated.")
	values.Set("file_count", "3")
	values.Set("file_path_0", "references/guide.md")
	values.Set("file_content_0", "# Guide\n\nSecond.\n")
	values.Set("file_path_1", "templates/old.txt")
	values.Set("file_content_1", "remove me\n")
	values.Set("file_delete_1", "on")
	values.Set("file_path_2", "scripts/run.sh")
	values.Set("file_content_2", "#!/bin/sh\n")
	values.Set("file_executable_2", "on")
	postReq := httptest.NewRequest(http.MethodPost, "/skills/admin/edit-files/edit", strings.NewReader(values.Encode()))
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq.AddCookie(&http.Cookie{Name: "skillsdex_session", Value: session})
	postW := httptest.NewRecorder()
	mux.ServeHTTP(postW, postReq)
	if postW.Code != http.StatusFound || postW.Header().Get("Location") != "/skills/admin/edit-files" {
		t.Fatalf("edit post = %d location %q body %s", postW.Code, postW.Header().Get("Location"), postW.Body.String())
	}
	files, skill, err := currentSkillFiles(ctx, db, admin.ID, "admin", "edit-files")
	if err != nil {
		t.Fatal(err)
	}
	if skill.RevisionNumber != 2 {
		t.Fatalf("revision = %d, want 2", skill.RevisionNumber)
	}
	seen := map[string]SkillFile{}
	for _, file := range files {
		seen[file.Path] = file
	}
	if seen["references/guide.md"].Content != "# Guide\n\nSecond.\n" {
		t.Fatalf("guide was not updated: %#v", seen["references/guide.md"])
	}
	if _, ok := seen["templates/old.txt"]; ok {
		t.Fatalf("deleted supporting file is still present: %#v", files)
	}
	if seen["scripts/run.sh"].Content != "#!/bin/sh\n" || !seen["scripts/run.sh"].Executable {
		t.Fatalf("new executable supporting file missing: %#v", seen["scripts/run.sh"])
	}
}

func TestRevisionPageDiffsSupportingFiles(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	admin, err := getUserByUsername(ctx, db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	skill, err := createSkill(ctx, db, admin, SkillInput{
		Owner:       "admin",
		Slug:        "revision-files",
		Name:        "Revision Files",
		Description: "Use when diffing package files.",
		Body:        "# Revision Files\n\nFirst.",
		Files: []SkillFile{
			{Path: "references/guide.md", Content: "# Guide\n\nFirst.\n"},
			{Path: "templates/prompt.txt", Content: "first\n"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := updateSkill(ctx, db, admin, "admin", skill.Slug, SkillInput{
		Owner:       "admin",
		Slug:        skill.Slug,
		Name:        "Revision Files",
		Description: "Use when diffing package files.",
		Body:        "# Revision Files\n\nSecond.",
		Files: []SkillFile{
			{Path: "references/guide.md", Content: "# Guide\n\nSecond.\n"},
			{Path: "scripts/run.sh", Content: "#!/bin/sh\n", Executable: true},
		},
	}); err != nil {
		t.Fatal(err)
	}
	revs, err := listRevisions(ctx, db, admin.ID, "admin", "revision-files")
	if err != nil {
		t.Fatal(err)
	}
	session, err := createWebSession(ctx, db, admin.ID)
	if err != nil {
		t.Fatal(err)
	}

	a := &app{db: db}
	mux := http.NewServeMux()
	a.routes(mux)
	req := httptest.NewRequest(http.MethodGet, "/skills/admin/revision-files/revisions/"+revs[0].ID, nil)
	req.AddCookie(&http.Cookie{Name: "skillsdex_session", Value: session})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("revision page = %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{
		"Package Diff",
		"SKILL.md",
		"references/guide.md",
		"+ Second.",
		"templates/prompt.txt",
		"- first",
		"scripts/run.sh",
		"added",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("revision page missing %q:\n%s", want, body)
		}
	}
}

func TestSkillDeleteRouteDeletesOwnedSkill(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	admin, err := getUserByUsername(ctx, db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := createSkill(ctx, db, admin, SkillInput{
		Owner:       "admin",
		Slug:        "delete-me",
		Name:        "Delete Me",
		Description: "Use when testing delete.",
		Body:        "# Delete Me",
	}); err != nil {
		t.Fatal(err)
	}
	session, err := createWebSession(ctx, db, admin.ID)
	if err != nil {
		t.Fatal(err)
	}

	a := &app{db: db}
	mux := http.NewServeMux()
	a.routes(mux)
	req := httptest.NewRequest(http.MethodPost, "/skills/admin/delete-me/delete", strings.NewReader(csrfForm(t, db, session, "")))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "skillsdex_session", Value: session})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/skills" {
		t.Fatalf("delete route = %d location %q, want redirect to /skills", w.Code, w.Header().Get("Location"))
	}
	if _, err := getSkill(ctx, db, admin.ID, "admin", "delete-me"); !isNotFound(err) {
		t.Fatalf("deleted skill should not be readable, err=%v", err)
	}
}

func TestSourceMetaHTMLLinksOnlySafeURLs(t *testing.T) {
	good := sourceMetaHTML(&Skill{SourceProvider: "github", SourceURL: "https://github.com/acme/skills", SourceAuthor: "acme", SourceName: "skills", SourcePath: "review/SKILL.md", SourceRef: "main"})
	if !strings.Contains(good, `href="https://github.com/acme/skills"`) || !strings.Contains(good, "GitHub: acme/skills") || !strings.Contains(good, "review/SKILL.md @ main") {
		t.Fatalf("safe source did not render as link: %s", good)
	}

	bad := sourceMetaHTML(&Skill{SourceProvider: "github", SourceURL: "javascript:alert(1)", SourceAuthor: "acme", SourceName: "skills"})
	if strings.Contains(bad, "href=") || !strings.Contains(bad, "GitHub: acme/skills") {
		t.Fatalf("unsafe source rendered incorrectly: %s", bad)
	}
}
