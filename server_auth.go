package skillsdex

import (
	"context"
	"database/sql"
	"errors"
	"golang.org/x/crypto/bcrypt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	loginFailureLimit  = 5
	loginFailureWindow = 15 * time.Minute
	loginLockout       = 15 * time.Minute
)

func (a *app) signupURL(r *http.Request, token string) string {
	scheme := "http"
	if a.isSecureRequest(r) {
		scheme = "https"
	}
	u := url.URL{Scheme: scheme, Host: r.Host, Path: "/signup"}
	q := u.Query()
	q.Set("invite", token)
	u.RawQuery = q.Encode()
	return u.String()
}

func (a *app) finishSignupInviteCreate(ctx context.Context, r *http.Request, result *SignupInviteCreateResult, emailTo string) error {
	result.URL = a.signupURL(r, result.Token)
	emailTo = strings.TrimSpace(emailTo)
	if emailTo == "" {
		return nil
	}
	if err := validateInviteEmailAddress(emailTo); err != nil {
		return err
	}
	result.EmailTo = emailTo
	if a.inviteEmailSender == nil {
		result.EmailError = "invite email is not configured"
		return nil
	}
	emailID, err := a.inviteEmailSender.SendInvite(ctx, emailTo, result.URL, result.Invite.ExpiresAt)
	if err != nil {
		result.EmailError = err.Error()
		return nil
	}
	result.EmailID = emailID
	return nil
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
		msg := ""
		if r.URL.Query().Get("signed_up") == "1" {
			msg = "Account created. Sign in to continue."
		}
		a.render(w, r, nil, "Login", loginForm(msg))
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
	username := strings.TrimSpace(r.FormValue("username"))
	blockedUntil, blocked, err := a.loginBlocked(r, username)
	if err != nil {
		http.Error(w, "login throttle error", http.StatusInternalServerError)
		return
	}
	if blocked {
		a.audit(r, nil, "login_rate_limited", "user", "", username, map[string]any{"blocked_until": blockedUntil.Format(time.RFC3339)})
		w.WriteHeader(http.StatusTooManyRequests)
		a.render(w, r, nil, "Login", loginForm("Too many failed login attempts. Try again later."))
		return
	}
	u, err := getUserByUsername(r.Context(), a.db, username)
	if err != nil || u.DeactivatedAt != nil || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(r.FormValue("password"))) != nil {
		if err := a.recordLoginFailure(r, username); err != nil {
			http.Error(w, "login throttle error", http.StatusInternalServerError)
			return
		}
		a.audit(r, nil, "login_failed", "user", "", username, nil)
		a.render(w, r, nil, "Login", loginForm("Invalid username or password."))
		return
	}
	if err := a.recordLoginSuccess(r, username); err != nil {
		http.Error(w, "login throttle error", http.StatusInternalServerError)
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
	http.SetCookie(w, a.sessionCookie(r, token, 0))
	a.audit(r, u, "login", "user", u.ID, u.Username, map[string]any{"password_change_required": redirectTo == "/setup/password"})
	http.Redirect(w, r, redirectTo, http.StatusFound)
}

func (a *app) handleSignup(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		token := strings.TrimSpace(r.URL.Query().Get("invite"))
		if token == "" {
			a.render(w, r, nil, "Sign Up", signupPage("", "This invite is invalid or expired."))
			return
		}
		if _, err := activeSignupInviteByToken(r.Context(), a.db, token); err != nil {
			a.render(w, r, nil, "Sign Up", signupPage("", "This invite is invalid or expired."))
			return
		}
		a.render(w, r, nil, "Sign Up", signupPage(token, ""))
	case http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, maxImportedSkillBytes)
		if err := r.ParseForm(); err != nil {
			a.render(w, r, nil, "Sign Up", signupPage("", "Invalid form."))
			return
		}
		token := strings.TrimSpace(r.FormValue("invite"))
		if token == "" {
			a.render(w, r, nil, "Sign Up", signupPage("", "This invite is invalid or expired."))
			return
		}
		password := r.FormValue("password")
		if password != r.FormValue("confirm_password") {
			a.render(w, r, nil, "Sign Up", signupPage(token, "passwords do not match"))
			return
		}
		user, invite, err := signupWithInvite(r.Context(), a.db, token, r.FormValue("username"), r.FormValue("email"), password)
		if err != nil {
			msg := err.Error()
			if strings.Contains(msg, "invite") {
				msg = "This invite is invalid or expired."
			}
			a.render(w, r, nil, "Sign Up", signupPage(token, msg))
			return
		}
		a.audit(r, nil, "signup_invite_used", "invite", invite.ID, invite.TokenPreview, map[string]any{"username": user.Username})
		a.audit(r, user, "user_signed_up", "user", user.ID, user.Username, nil)
		http.Redirect(w, r, "/login?signed_up=1", http.StatusFound)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *app) loginBlocked(r *http.Request, username string) (time.Time, bool, error) {
	now := nowUTC()
	if err := pruneLoginFailures(r.Context(), a.db, now); err != nil {
		return time.Time{}, false, err
	}
	for _, key := range a.loginRateKeys(r, username) {
		var lockedRaw string
		err := a.db.QueryRowContext(r.Context(), `SELECT locked_until FROM login_failures WHERE key = ?`, key).Scan(&lockedRaw)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return time.Time{}, false, err
		}
		if lockedRaw == "" {
			continue
		}
		lockedUntil, err := time.Parse(time.RFC3339, lockedRaw)
		if err != nil {
			return time.Time{}, false, err
		}
		if lockedUntil.After(now) {
			return lockedUntil, true, nil
		}
	}
	return time.Time{}, false, nil
}

func (a *app) recordLoginFailure(r *http.Request, username string) error {
	now := nowUTC()
	for _, key := range a.loginRateKeys(r, username) {
		var count int
		var firstRaw, lockedRaw string
		err := a.db.QueryRowContext(r.Context(), `SELECT count, first_at, locked_until FROM login_failures WHERE key = ?`, key).Scan(&count, &firstRaw, &lockedRaw)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		first := now
		if err == nil {
			parsedFirst, parseErr := time.Parse(time.RFC3339, firstRaw)
			if parseErr != nil {
				return parseErr
			}
			first = parsedFirst
			if lockedRaw != "" {
				lockedUntil, parseErr := time.Parse(time.RFC3339, lockedRaw)
				if parseErr != nil {
					return parseErr
				}
				if !lockedUntil.After(now) {
					count = 0
					first = now
				}
			}
			if now.Sub(first) > loginFailureWindow {
				count = 0
				first = now
			}
		}
		count++
		lockedUntil := ""
		if count >= loginFailureLimit {
			lockedUntil = now.Add(loginLockout).Format(time.RFC3339)
		}
		if _, err := a.db.ExecContext(r.Context(), `
			INSERT INTO login_failures (key, count, first_at, locked_until, updated_at)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET
				count = excluded.count,
				first_at = excluded.first_at,
				locked_until = excluded.locked_until,
				updated_at = excluded.updated_at
		`, key, count, first.Format(time.RFC3339), lockedUntil, now.Format(time.RFC3339)); err != nil {
			return err
		}
	}
	return nil
}

func (a *app) recordLoginSuccess(r *http.Request, username string) error {
	for _, key := range a.loginRateKeys(r, username) {
		if _, err := a.db.ExecContext(r.Context(), `DELETE FROM login_failures WHERE key = ?`, key); err != nil {
			return err
		}
	}
	return nil
}

func (a *app) loginRateKeys(r *http.Request, username string) []string {
	ip := a.remoteAddr(r)
	if ip == "" {
		ip = "unknown"
	}
	username = strings.ToLower(strings.TrimSpace(username))
	return []string{"ip:" + ip, "user:" + username + "|ip:" + ip}
}

func pruneLoginFailures(ctx context.Context, db *sql.DB, now time.Time) error {
	_, err := db.ExecContext(ctx, `
		DELETE FROM login_failures
		WHERE (locked_until <> '' AND locked_until <= ?)
			OR (locked_until = '' AND first_at <= ?)
	`, now.Format(time.RFC3339), now.Add(-loginFailureWindow).Format(time.RFC3339))
	return err
}

func (a *app) handleLogout(w http.ResponseWriter, r *http.Request) {
	u := webUser(r)
	if c, err := r.Cookie("skillsdex_session"); err == nil {
		_ = deleteWebSession(r.Context(), a.db, c.Value)
	}
	http.SetCookie(w, a.sessionCookie(r, "", -1))
	a.audit(r, u, "logout", "user", u.ID, u.Username, nil)
	http.Redirect(w, r, "/login", http.StatusFound)
}
