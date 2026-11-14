package skillsdex

import (
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"net/http"
	"strings"
	"time"
)

func (a *app) handleDeviceRegistration(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var input struct {
		Enrolment string `json:"enrolment"`
		Name      string `json:"name"`
		Platform  string `json:"platform"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid registration")
		return
	}
	device, credential, err := registerDevice(r.Context(), a.db, input.Enrolment, input.Name, input.Platform)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "registration failed: check the code, machine name and platform")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, map[string]any{"device": device, "credential": credential})
}

func (a *app) handleMachines(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/machines" {
		http.NotFound(w, r)
		return
	}
	u := webUser(r)
	w.Header().Set("Cache-Control", "no-store")
	code := ""
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		switch r.FormValue("action") {
		case "enrol":
			var err error
			code, err = createDeviceEnrolment(r.Context(), a.db, u.ID)
			if err != nil {
				http.Error(w, "could not create connection code", http.StatusInternalServerError)
				return
			}
			a.audit(r, u, "device_enrolment_created", "device", "", "", nil)
		case "revoke":
			id := r.FormValue("device_id")
			if err := revokeDevice(r.Context(), a.db, u.ID, id); err != nil {
				http.Error(w, "machine not found", http.StatusNotFound)
				return
			}
			a.audit(r, u, "device_revoked", "device", id, "", nil)
			http.Redirect(w, r, "/machines", http.StatusSeeOther)
			return
		default:
			http.Error(w, "unknown action", http.StatusBadRequest)
			return
		}
	} else if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	devices, err := listDevices(r.Context(), a.db, u.ID)
	if err != nil {
		http.Error(w, "could not load machines", http.StatusInternalServerError)
		return
	}
	reports, err := loadDeviceReports(r.Context(), a.db, u.ID)
	if err != nil {
		http.Error(w, "could not load agent status", 500)
		return
	}
	var desired int
	if err := a.db.QueryRowContext(r.Context(), `SELECT COALESCE(MAX(revision),0) FROM instruction_revisions WHERE user_id=?`, u.ID).Scan(&desired); err != nil {
		http.Error(w, "could not load desired revision", 500)
		return
	}
	for i := range devices {
		devices[i].Reports = reports[devices[i].ID]
	}
	a.render(w, r, u, "Machines", machinesPage(devices, code, desired))
}

func machinesPage(devices []Device, code string, desiredRevision ...int) template.HTML {
	desired := 0
	if len(desiredRevision) > 0 {
		desired = desiredRevision[0]
	}
	var b strings.Builder
	b.WriteString(`<header class="pagehead"><div><p class="eyebrow">Your workspace</p><h1>Machines</h1><p>Manage the computers connected to your configuration.</p></div><form method="post" action="/machines"><input type="hidden" name="action" value="enrol"><button type="submit">Connect a machine</button></form></header>`)
	if code != "" {
		fmt.Fprintf(&b, `<section class="panel" style="margin-bottom:24px"><h2>Connect this computer</h2><ol><li>Install the SkillsDex CLI on the computer you want to connect.</li><li>Run <code>skillsdex agent setup --server https://YOUR_SERVER</code>, using this dashboard's server address.</li><li>Paste this code when asked, then review your detected agents and instructions.</li></ol><pre>%s</pre><p>Setup can keep your instructions up to date automatically whenever you sign in.</p><p class="muted">This single-use code expires in 10 minutes. Existing local changes are preserved for you to review.</p></section>`, html.EscapeString(code))
	}
	b.WriteString(`<section class="panel"><h2>Connected machines</h2>`)
	if len(devices) == 0 {
		b.WriteString(`<div class="empty"><h2>No machines connected</h2><p>Your computers will appear here after registration.</p></div>`)
	}
	for _, d := range devices {
		status := "Registered · awaiting first sync"
		if len(d.Reports) > 0 {
			status = "Agent status below"
		}
		if d.RevokedAt != "" {
			status = "Access revoked"
		}
		fmt.Fprintf(&b, `<div class="machine-row"><div><h3>%s</h3><p class="muted">%s · %s</p></div>`, html.EscapeString(d.Name), html.EscapeString(d.Platform), status)
		if d.RevokedAt == "" {
			fmt.Fprintf(&b, `<form method="post" action="/machines"><input type="hidden" name="action" value="revoke"><input type="hidden" name="device_id" value="%s"><button class="ghost" type="submit">Revoke access</button></form>`, html.EscapeString(d.ID))
		}
		b.WriteString(`</div>`)
		if d.RevokedAt == "" && len(d.Reports) > 0 {
			b.WriteString(`<div class="agent-status-list">`)
			for _, report := range d.Reports {
				fmt.Fprintf(&b, `<span>%s <strong>%s</strong> <small>v%d</small></span>`, html.EscapeString(report.Agent), agentStatusLabel(report, desired, time.Now().UTC()), report.Revision)
				if report.State == "conflict" {
					fmt.Fprintf(&b, `<details><summary>Review local changes</summary><p>On this machine, run <code>skillsdex agent resolve %s</code> to compare your instructions. Add <code>--keep-mine</code> to keep using local instructions or <code>--use-shared</code> to back them up and use the shared version.</p></details>`, html.EscapeString(report.Agent))
				}
			}
			b.WriteString(`</div>`)
		}
	}
	b.WriteString(`</section>`)
	return template.HTML(b.String())
}
