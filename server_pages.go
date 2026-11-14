package skillsdex

import (
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"
)

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

func signupPage(inviteToken, msg string) template.HTML {
	var b strings.Builder
	b.WriteString(`<section class="login-shell"><div class="login-brand"><div class="product-lockup"><div class="mark">SD</div><div><p class="eyebrow">Invite signup</p><h1>SkillsDex</h1></div></div><p>Create a local account from an admin invite. Choose the username you want to use as your SkillsDex namespace.</p><div class="login-graphic" aria-hidden="true"><div class="stack-card primary"><span>invite</span><strong>24h</strong></div><div class="stack-card"><span>username</span><strong>you</strong></div><div class="stack-card muted-card"><span>role</span><strong>user</strong></div></div></div><form class="panel login-panel" method="post" action="/signup"><div class="panel-head"><div class="panel-icon">` + icon("users") + `</div><div><h2>Create Account</h2><p>Use your invite before it expires.</p></div></div>`)
	if msg != "" {
		b.WriteString(`<p class="flash">` + html.EscapeString(msg) + `</p>`)
	}
	if inviteToken != "" {
		b.WriteString(`<input type="hidden" name="invite" value="` + html.EscapeString(inviteToken) + `">
		<label>Username <input name="username" autocomplete="username" required autofocus pattern="[a-z0-9][a-z0-9-]{0,62}"></label>
		<label>Email <input name="email" type="email" autocomplete="email"></label>
		<label>Password <input name="password" type="password" autocomplete="new-password" minlength="12" required></label>
		<label>Confirm password <input name="confirm_password" type="password" autocomplete="new-password" minlength="12" required></label>
		<button type="submit">` + iconLabel("users", "Create Account") + `</button>`)
	} else {
		b.WriteString(`<a class="button secondary" href="/login">` + iconLabel("key", "Back to Login") + `</a>`)
	}
	b.WriteString(`</form></section>`)
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

func invitesPage(invites []SignupInvite, inviteURL, msg string) template.HTML {
	var b strings.Builder
	b.WriteString(`<header class="pagehead"><div><p class="eyebrow">Admin</p><h1>Invites</h1><p>Create 24-hour signup links for new non-admin users.</p></div><div class="page-art users-art" aria-hidden="true">` + icon("users") + `</div></header><div class="split"><section class="surface data-surface">`)
	if msg != "" {
		b.WriteString(`<p class="flash ` + html.EscapeString(inviteFlashClass(msg)) + `">` + html.EscapeString(msg) + `</p>`)
	}
	if inviteURL != "" {
		b.WriteString(`<div class="command-card"><span>One-time signup URL</span><pre>` + html.EscapeString(inviteURL) + `</pre></div>`)
	}
	b.WriteString(`<table class="invite-table"><thead><tr><th>Token</th><th>Status</th><th>Created</th><th>Expires</th><th>Used By</th><th></th></tr></thead><tbody>`)
	if len(invites) == 0 {
		b.WriteString(`<tr><td colspan="6"><div class="empty">No signup invites yet.</div></td></tr>`)
	}
	for _, invite := range invites {
		status := signupInviteStatus(invite)
		action := ""
		if status == "active" {
			action = `<form method="post" action="/admin/invites/` + html.EscapeString(invite.ID) + `/revoke"><button class="link danger" type="submit">` + iconLabel("trash", "Revoke") + `</button></form>`
		}
		usedBy := invite.UsedBy
		if usedBy == "" {
			usedBy = "-"
		}
		b.WriteString(`<tr><td class="token-cell invite-token-cell"><code>` + html.EscapeString(invite.TokenPreview) + `</code><div class="muted">` + html.EscapeString(invite.ID) + `</div></td><td class="invite-status-cell"><span class="pill ` + html.EscapeString(status) + `">` + html.EscapeString(status) + `</span></td><td class="invite-date-cell">` + invite.CreatedAt.Format("2006-01-02 15:04") + `</td><td class="invite-date-cell">` + invite.ExpiresAt.Format("2006-01-02 15:04") + `</td><td class="invite-used-cell">` + html.EscapeString(usedBy) + `</td><td class="invite-action-cell">` + action + `</td></tr>`)
	}
	b.WriteString(`</tbody></table></section><aside class="panel sticky"><div class="panel-head"><div class="panel-icon">` + icon("key") + `</div><div><h2>New Invite</h2><p>Create a single-use signup link that expires in 24 hours.</p></div></div><form method="post" action="/admin/invites">
		<label>Email invite <input name="email" type="email" autocomplete="email" placeholder="optional"></label>
		<button type="submit">` + iconLabel("plus", "Create Invite") + `</button>
	</form><div class="command-card"><span>CLI</span><pre>skillsdex invites create --email user@example.com</pre></div></aside></div>`)
	return template.HTML(b.String())
}

func inviteFlashClass(msg string) string {
	if strings.HasPrefix(msg, "Invite emailed to ") {
		return "success"
	}
	return "warning"
}

func signupInviteCreateMessage(result SignupInviteCreateResult) string {
	if result.EmailTo == "" {
		return ""
	}
	if result.EmailID != "" {
		return "Invite emailed to " + result.EmailTo + "."
	}
	if result.EmailError != "" {
		return "Invite created, but email was not sent: " + result.EmailError
	}
	return ""
}

func signupInviteStatus(invite SignupInvite) string {
	switch {
	case invite.RevokedAt != "":
		return "revoked"
	case invite.UsedAt != "":
		return "used"
	case !invite.ExpiresAt.After(nowUTC()):
		return "expired"
	default:
		return "active"
	}
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
		<label>Password <input name="password" type="password" minlength="12" required></label>
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
	b.WriteString(`<header class="pagehead"><div><p class="eyebrow">Admin</p><h1>Maintenance</h1><p>Prune old operational records and watch storage growth.</p></div><div class="page-art users-art" aria-hidden="true">` + icon("archive") + `</div></header><div class="split"><section class="surface">`)
	if msg != "" {
		b.WriteString(`<p class="flash">` + html.EscapeString(msg) + `</p>`)
	}
	b.WriteString(`<div class="metrics maintenance-metrics">`)
	b.WriteString(`<span><strong>` + fmt.Sprintf("%d", stats.ExpiredSessions) + `</strong> expired sessions</span>`)
	b.WriteString(`<span><strong>` + fmt.Sprintf("%d", stats.RevokedAPITokens) + `</strong> revoked tokens older than ` + fmt.Sprintf("%d", stats.TokenRetentionDays) + ` days</span>`)
	b.WriteString(`<span><strong>` + fmt.Sprintf("%d", stats.OldAuditEvents) + `</strong> audit events older than ` + fmt.Sprintf("%d", stats.AuditRetentionDays) + ` days</span>`)
	b.WriteString(`<span><strong>` + html.EscapeString(formatBytes(stats.DatabaseBytes)) + `</strong> database of ` + html.EscapeString(formatBytes(stats.MaxDatabaseBytes)) + ` max</span>`)
	b.WriteString(`<span><strong>` + html.EscapeString(formatBytes(stats.SkillBytes)) + `</strong> stored skill files</span>`)
	b.WriteString(`</div><table><thead><tr><th>Record</th><th>Retention</th><th>Action</th></tr></thead><tbody>`)
	b.WriteString(`<tr><td>Expired web sessions</td><td>Removed when already expired</td><td>Delete session rows only</td></tr>`)
	b.WriteString(`<tr><td>Revoked API tokens</td><td>` + fmt.Sprintf("%d", stats.TokenRetentionDays) + ` days after revocation</td><td>Delete token metadata for already-revoked tokens</td></tr>`)
	b.WriteString(`<tr><td>Audit events</td><td>` + fmt.Sprintf("%d", stats.AuditRetentionDays) + ` days after event time</td><td>Delete old audit rows, then record this prune</td></tr>`)
	b.WriteString(`<tr><td>Skill revisions</td><td>Keep latest ` + fmt.Sprintf("%d", stats.RevisionKeep) + ` per skill</td><td>Delete older revision files and metadata</td></tr>`)
	b.WriteString(`</tbody></table><h2>Largest Users</h2><table><thead><tr><th>User</th><th>Skills</th><th>Revisions</th><th>Stored</th></tr></thead><tbody>`)
	if len(stats.TopUsers) == 0 {
		b.WriteString(`<tr><td colspan="4"><div class="empty">No user storage yet.</div></td></tr>`)
	}
	for _, row := range stats.TopUsers {
		b.WriteString(`<tr><td>` + html.EscapeString(row.Username) + `</td><td>` + fmt.Sprintf("%d", row.SkillCount) + `</td><td>` + fmt.Sprintf("%d", row.RevisionCount) + `</td><td>` + html.EscapeString(formatBytes(row.StoredBytes)) + ` of ` + html.EscapeString(formatBytes(stats.MaxUserStoredBytes)) + `</td></tr>`)
	}
	b.WriteString(`</tbody></table><h2>Largest Skills</h2><table><thead><tr><th>Skill</th><th>Revisions</th><th>Stored</th></tr></thead><tbody>`)
	if len(stats.TopSkills) == 0 {
		b.WriteString(`<tr><td colspan="3"><div class="empty">No skill storage yet.</div></td></tr>`)
	}
	for _, row := range stats.TopSkills {
		b.WriteString(`<tr><td>` + html.EscapeString(row.OwnerSlug+"/"+row.Slug) + `</td><td>` + fmt.Sprintf("%d", row.RevisionCount) + `</td><td>` + html.EscapeString(formatBytes(row.StoredBytes)) + `</td></tr>`)
	}
	b.WriteString(`</tbody></table></section><aside class="panel sticky"><div class="panel-head"><div class="panel-icon">` + icon("trash") + `</div><div><h2>Prune Data</h2><p>Keep the database small while preserving active access and skill history.</p></div></div><form method="post" action="/admin/maintenance">
		<label>Audit event retention days <input name="audit_days" type="number" min="1" max="3650" value="` + fmt.Sprintf("%d", stats.AuditRetentionDays) + `" required></label>
		<label>Revoked token retention days <input name="revoked_token_days" type="number" min="1" max="3650" value="` + fmt.Sprintf("%d", stats.TokenRetentionDays) + `" required></label>
		<label>Revisions to keep per skill <input name="revision_keep" type="number" min="1" max="` + fmt.Sprintf("%d", maxSkillRevisions) + `" value="` + fmt.Sprintf("%d", stats.RevisionKeep) + `" required></label>
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
		<label>New password <input name="password" type="password" autocomplete="new-password" minlength="12"></label>
		<label class="check"><input name="is_admin" type="checkbox"`)
	if u.IsAdmin {
		b.WriteString(` checked`)
	}
	b.WriteString(`> Admin</label><label class="check"><input name="deactivated" type="checkbox"`)
	if u.DeactivatedAt != nil {
		b.WriteString(` checked`)
	}
	b.WriteString(`> Deactivated</label><button type="submit">` + iconLabel("edit", "Save") + `</button></form>
	<form class="inline-danger" method="post" action="/admin/users/` + html.EscapeString(u.ID) + `/delete" onsubmit="return confirm('Delete user ` + html.EscapeString(u.Username) + ` and all owned skills? This cannot be undone.');">
		<button class="link danger" type="submit">` + iconLabel("trash", "Delete user") + `</button>
	</form></section>`)
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

func skillsPage(skills, allSkills []Skill, archived bool, selectedTag string) template.HTML {
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
	tagOptions := skillTagOptions(allSkills)
	b.WriteString(`<header class="pagehead skills-heading"><div><h1>Skills</h1><p>Reusable instructions for the work you do often.</p></div></header><div class="library-toolbar"><p class="library-count">` + fmt.Sprintf("%d skills", active))
	if archived {
		b.WriteString(fmt.Sprintf(" · %d archived", archivedCount))
	}
	b.WriteString(`</p><div class="library-actions"><details class="library-menu"><summary class="button">` + iconLabel("plus", "Add skill") + `</summary><div class="library-menu-items"><a href="/skills/new">Create a skill</a><a href="/skills/import">Import a skill</a><a href="/skills/top">Discover skills</a></div></details><details class="library-menu"><summary class="button ghost">More</summary><div class="library-menu-items"><a href="/skills/export">Download backup</a><a href="/skills/export/github">Export for GitHub</a>`)
	if archived {
		b.WriteString(`<a href="` + html.EscapeString(skillsURL(false, selectedTag)) + `">Hide archived</a>`)
	} else {
		b.WriteString(`<a href="` + html.EscapeString(skillsURL(true, selectedTag)) + `">Show archived</a>`)
	}
	b.WriteString(`</div></details></div></div>`)
	b.WriteString(`<details class="library-install"><summary>Use these skills on your computer</summary><div><p>Open a skill for its install command. If this is your first time, <a href="/setup">set up the SkillsDex CLI</a>.</p><p>To install your recommended skills:</p><pre>skillsdex install-defaults</pre><p>To preview updates to installed skills:</p><pre>skillsdex sync --dry-run --strict</pre><a href="/help">More installation options</a></div></details>`)
	if len(tagOptions) > 0 {
		b.WriteString(`<nav class="tag-filter" aria-label="Tag filter"><a class="pill`)
		if selectedTag == "" {
			b.WriteString(` active`)
		}
		b.WriteString(`" href="` + html.EscapeString(skillsURL(archived, "")) + `">all</a>`)
		for _, tag := range tagOptions {
			b.WriteString(`<a class="pill`)
			if tag == selectedTag {
				b.WriteString(` active`)
			}
			b.WriteString(`" href="` + html.EscapeString(skillsURL(archived, tag)) + `">` + html.EscapeString(tag) + `</a>`)
		}
		b.WriteString(`</nav>`)
	}
	b.WriteString(`<section class="surface library-list"><table><caption class="sr-only">Your reusable skills</caption><thead><tr><th scope="col">Skill</th><th scope="col">Version</th><th scope="col">Updated</th></tr></thead><tbody>`)
	if len(skills) == 0 {
		msg := `No skills yet.`
		if selectedTag != "" {
			msg = `No skills match tag ` + html.EscapeString(selectedTag) + `.`
		}
		b.WriteString(`<tr><td colspan="3"><div class="empty"><strong>` + msg + `</strong><p>Add instructions you reuse, or import a skill you already have.</p><a href="/skills/new">Create a skill</a></div></td></tr>`)
	}
	for _, s := range skills {
		id := s.OwnerSlug + "/" + s.Slug
		b.WriteString(`<tr><td><a class="library-skill-name" href="/skills/` + html.EscapeString(id) + `">` + html.EscapeString(s.Name) + `</a><p class="library-description">` + html.EscapeString(s.ShortDescription) + `</p><div class="library-skill-meta"><span class="library-identifier">` + html.EscapeString(id) + `</span>`)
		if s.ArchivedAt != "" {
			b.WriteString(`<span class="pill">archived</span>`)
		}
		if s.RecommendedDefault {
			b.WriteString(`<div class="row-tags"><span class="pill active">recommended</span></div>`)
		}
		if s.PinnedInstall {
			b.WriteString(`<div class="row-tags"><span class="pill pinned">pinned</span></div>`)
		}
		if s.SourceProvider != "" {
			b.WriteString(`<div class="row-tags"><span class="pill source">` + html.EscapeString(sourceProviderLabel(s.SourceProvider)) + `</span></div>`)
		}
		if len(s.Tags) > 0 {
			b.WriteString(`<div class="row-tags">`)
			for _, tag := range s.Tags {
				b.WriteString(`<a class="pill" href="` + html.EscapeString(skillsURL(archived, tag)) + `">` + html.EscapeString(tag) + `</a>`)
			}
			b.WriteString(`</div>`)
		}
		b.WriteString(`</div></td><td class="library-version">v` + fmt.Sprintf("%d", s.RevisionNumber) + `</td><td class="library-updated"><time datetime="` + s.UpdatedAt.Format(time.RFC3339) + `" title="` + s.UpdatedAt.Format("2006-01-02 15:04 MST") + `">` + s.UpdatedAt.Format("2 Jan 2006") + `</time></td></tr>`)
	}
	b.WriteString(`</tbody></table></section>`)
	return template.HTML(b.String())
}

func skillTagOptions(skills []Skill) []string {
	seen := map[string]bool{}
	var tags []string
	for _, skill := range skills {
		for _, tag := range skill.Tags {
			if tag == "" || seen[tag] {
				continue
			}
			seen[tag] = true
			tags = append(tags, tag)
		}
	}
	sort.Strings(tags)
	return tags
}

func skillsURL(archived bool, tag string) string {
	values := url.Values{}
	if archived {
		values.Set("archived", "1")
	}
	if tag != "" {
		values.Set("tag", tag)
	}
	if encoded := values.Encode(); encoded != "" {
		return "/skills?" + encoded
	}
	return "/skills"
}

func tagsHTML(tags []string, linked bool, archived bool) string {
	if len(tags) == 0 {
		return "none"
	}
	var b strings.Builder
	b.WriteString(`<span class="row-tags">`)
	for _, tag := range tags {
		if linked {
			b.WriteString(`<a class="pill" href="` + html.EscapeString(skillsURL(archived, tag)) + `">` + html.EscapeString(tag) + `</a>`)
		} else {
			b.WriteString(`<span class="pill">` + html.EscapeString(tag) + `</span>`)
		}
	}
	b.WriteString(`</span>`)
	return b.String()
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
		<p class="muted">Restores skills and appends saved instruction history. The latest restored instructions will sync to connected machines. Machine inventory is included for reference; credentials are excluded, so new machines need to reconnect.</p>
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
			paths:      "Native Codex locations include repo `.agents/skills`, user `$HOME/.agents/skills`, and admin `/etc/codex/skills`. SkillsDex writes managed Codex installs to `$HOME/.agents/skills/<owner>--<slug>/SKILL.md`.",
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
	b.WriteString(`<aside class="panel"><h2>Quick paths</h2><div class="path-list"><div class="command-card"><span>Claude project skill</span><pre>.claude/skills/name/SKILL.md</pre></div><div class="command-card"><span>Codex repo skill</span><pre>.agents/skills/name/SKILL.md</pre></div><div class="command-card"><span>SkillsDex Codex install</span><pre>~/.agents/skills/owner--slug/SKILL.md</pre></div><div class="command-card"><span>Cursor project rule</span><pre>.cursor/rules/name.mdc</pre></div><div class="command-card"><span>Simple repo guidance</span><pre>AGENTS.md</pre></div></div></aside></div>`)
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
		<label>Tags <input name="tags" value="` + html.EscapeString(tagsText(in.Tags)) + `" placeholder="go, testing, security"></label>
		<div class="form-grid"><label>Source provider <input name="source_provider" value="` + html.EscapeString(in.SourceProvider) + `" placeholder="github or agentskill.sh"></label>
		<label>Source URL <input name="source_url" type="url" value="` + html.EscapeString(in.SourceURL) + `" placeholder="https://github.com/owner/repo/tree/main/skill"></label></div>
		<div class="form-grid"><label>Source author <input name="source_author" value="` + html.EscapeString(in.SourceAuthor) + `" placeholder="Jeffallan"></label>
		<label>Source name <input name="source_name" value="` + html.EscapeString(in.SourceName) + `" placeholder="claude-skills"></label>
		<label>Source path <input name="source_path" value="` + html.EscapeString(in.SourcePath) + `" placeholder="skills/golang-pro/SKILL.md"></label>
		<label>Source ref <input name="source_ref" value="` + html.EscapeString(in.SourceRef) + `" placeholder="main"></label></div>
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
	tags := tagsHTML(s.Tags, true, false)
	body := bodyFromSkillMarkdown(raw)
	quality := skillQualityWithContext(SkillInput{Name: s.Name, Description: s.Description, ShortDescription: s.ShortDescription, Body: body}, SkillQualityContext{
		CurrentIdentifier: s.OwnerSlug + "/" + s.Slug,
		SourceProvider:    s.SourceProvider,
		SourceURL:         s.SourceURL,
		UpdatedAt:         s.UpdatedAt,
		Skills:            activeSkills,
	})
	b.WriteString(`<section class="panel skill-install-panel"><div class="panel-head"><div class="panel-icon">` + icon("bolt") + `</div><div><h2>Install</h2><p>Copy this skill into the managed Codex target.</p></div></div><div class="command-card"><span>Codex</span><pre>skillsdex config set default_target codex
skillsdex install ` + html.EscapeString(id) + `</pre></div><div class="meta-list detail-meta"><div><span>Name</span><strong>` + html.EscapeString(s.Name) + `</strong></div><div><span>Short description</span><strong>` + html.EscapeString(s.ShortDescription) + `</strong></div>` + source + `<div><span>Tags</span><strong>` + tags + `</strong></div><div><span>Pinned install</span><strong>` + pinned + `</strong></div><div><span>Recommended default</span><strong>` + recommended + `</strong></div></div></section><section class="panel quality-section">` + qualityPanelHTML(quality) + `</section><section class="rendered skill-body">` + simpleMarkdownHTML(body) + `</section>`)
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
	if s.SourceProvider == "" && s.SourceURL == "" && s.SourceAuthor == "" && s.SourceName == "" && s.SourcePath == "" {
		return `<div><span>Source</span><strong>Manual</strong></div>`
	}
	label := sourceDisplayLabel(s)
	if s.SourceURL == "" || !safeHTTPURL(s.SourceURL) {
		return `<div><span>Source</span><strong>` + html.EscapeString(label) + `</strong></div>`
	}
	return `<div><span>Source</span><strong><a href="` + html.EscapeString(s.SourceURL) + `" target="_blank" rel="noopener">` + html.EscapeString(label) + `</a></strong></div>`
}

func sourceDisplayLabel(s *Skill) string {
	provider := sourceProviderLabel(s.SourceProvider)
	identity := strings.Trim(strings.TrimSpace(s.SourceAuthor)+"/"+strings.TrimSpace(s.SourceName), "/")
	pathLabel := strings.TrimSpace(s.SourcePath)
	ref := strings.TrimSpace(s.SourceRef)
	switch {
	case identity != "" && pathLabel != "" && ref != "":
		return provider + ": " + identity + " · " + pathLabel + " @ " + ref
	case identity != "" && pathLabel != "":
		return provider + ": " + identity + " · " + pathLabel
	case identity != "":
		return provider + ": " + identity
	case pathLabel != "":
		return provider + ": " + pathLabel
	default:
		return provider
	}
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
	<a class="skip-link" href="#main-content">Skip to content</a>
	<div class="app-shell {{if .User}}authenticated{{end}}">
		<nav class="app-nav" aria-label="Main navigation">
			<a class="brand" href="/overview"><span class="brand-mark">SD</span><span>SkillsDex</span></a>
			{{if .User}}
				<div class="nav-links">
					<a href="/overview" data-nav="/overview">` + iconLabel("layers", "Overview") + `</a>
					<a href="/instructions" data-nav="/instructions">` + iconLabel("book", "Instructions") + `</a>
					<a href="/machines" data-nav="/machines">` + iconLabel("layers", "Machines") + `</a>
					<a href="/skills" data-nav="/skills">` + iconLabel("book", "Skills") + `</a>
					<details class="nav-admin nav-more"><summary>More</summary>
					<a href="/setup" data-nav="/setup">` + iconLabel("check", "Setup") + `</a>
					<a href="/sets" data-nav="/sets">` + iconLabel("layers", "Collections") + `</a>
					<a href="/skills/top" data-nav="/skills/top">` + iconLabel("spark", "Discover skills") + `</a>
					<a href="/help" data-nav="/help">` + iconLabel("help", "Help") + `</a>
				<a href="/tokens" data-nav="/tokens">` + iconLabel("key", "API Tokens") + `</a>
				{{if .User.IsAdmin}}<details class="nav-admin"><summary>Administration</summary><a href="/admin/users" data-nav="/admin/users">` + iconLabel("users", "Users") + `</a><a href="/admin/invites" data-nav="/admin/invites">` + iconLabel("key", "Invites") + `</a><a href="/admin/audit" data-nav="/admin/audit">` + iconLabel("archive", "Audit") + `</a><a href="/admin/maintenance" data-nav="/admin/maintenance">` + iconLabel("trash", "Maintenance") + `</a></details>{{end}}
					</details>
			</div>
			<button class="theme-toggle icon-button" type="button" aria-label="Toggle color theme" data-theme-toggle>` + icon("sun") + `<span class="button-label">Theme</span></button>
			<form method="post" action="/logout">{{.CSRFField}}<button class="link nav-logout" type="submit">` + icon("logout") + `<span class="button-label">Logout {{.User.Username}}</span></button></form>
			{{end}}
		</nav>
		<main id="main-content" tabindex="-1">{{.Body}}</main>
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
				var group = link.closest("details");
				while (group) { group.open = true; group = group.parentElement.closest("details"); }
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
