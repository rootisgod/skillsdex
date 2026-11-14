package skillsdex

import (
	"context"
	"database/sql"
	"fmt"
	"html"
	"html/template"
	"net/http"
	"strings"
	"time"
)

type deviceAgentStatus struct {
	Agent      string
	Revision   int
	State      string
	ReportedAt string
}

func loadDeviceReports(ctx context.Context, db *sql.DB, userID string) (map[string][]deviceAgentStatus, error) {
	rows, err := db.QueryContext(ctx, `SELECT s.device_id,s.agent,s.applied_revision,s.state,s.reported_at FROM device_agent_status s JOIN devices d ON d.id=s.device_id WHERE d.user_id=? ORDER BY s.agent`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	reports := make(map[string][]deviceAgentStatus)
	for rows.Next() {
		var id string
		var status deviceAgentStatus
		if err := rows.Scan(&id, &status.Agent, &status.Revision, &status.State, &status.ReportedAt); err != nil {
			return nil, err
		}
		reports[id] = append(reports[id], status)
	}
	return reports, rows.Err()
}

func reportedAgentState(status deviceAgentStatus, desired int, now time.Time) string {
	stamp, err := time.Parse(time.RFC3339, status.ReportedAt)
	if err != nil || now.Sub(stamp) > 3*time.Minute {
		return "Offline"
	}
	if status.State == "conflict" {
		return "Local changes"
	}
	if status.State == "error" {
		return "Needs attention"
	}
	if status.State == "local" {
		return "Using local instructions"
	}
	if desired == 0 || status.Revision != desired {
		return "Update pending"
	}
	return "Synced"
}

func (a *app) handleOverview(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/overview" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	u := webUser(r)
	devices, err := listDevices(r.Context(), a.db, u.ID)
	if err != nil {
		http.Error(w, "could not load machines", 500)
		return
	}
	history, err := instructionHistory(r.Context(), a.db, u.ID)
	if err != nil {
		http.Error(w, "could not load instructions", 500)
		return
	}
	reports, err := loadDeviceReports(r.Context(), a.db, u.ID)
	if err != nil {
		http.Error(w, "could not load sync status", 500)
		return
	}
	a.render(w, r, u, "Overview", overviewPage(devices, history, reports, time.Now().UTC()))
}

func overviewPage(devices []Device, history []instructionRevision, reports map[string][]deviceAgentStatus, now time.Time) template.HTML {
	revision := 0
	if len(history) > 0 {
		revision = history[0].Revision
	}
	active := 0
	agents := map[string]bool{}
	attention := 0
	offline := 0
	pending := 0
	for _, d := range devices {
		if d.RevokedAt != "" {
			continue
		}
		active++
		if len(reports[d.ID]) == 0 {
			pending++
		}
		machineOffline := false
		for _, s := range reports[d.ID] {
			agents[s.Agent] = true
			switch reportedAgentState(s, revision, now) {
			case "Local changes", "Needs attention":
				attention++
			case "Offline":
				machineOffline = true
			case "Update pending":
				pending++
			}
		}
		if machineOffline {
			offline++
		}
	}
	status := "Ready to connect"
	if active > 0 {
		status = "Everything is up to date"
		if offline > 0 {
			status = fmt.Sprintf("%d machine offline", offline)
			if offline > 1 {
				status = fmt.Sprintf("%d machines offline", offline)
			}
		}
		if pending > 0 {
			status = "Waiting for updates"
		}
		if attention > 0 {
			status = "Needs your attention"
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<header class="pagehead"><div><h1>Overview</h1><p>Your instructions, across your machines.</p></div><span class="overview-status">%s</span></header><div class="overview-metrics"><div><strong>%d</strong><span>Machines</span></div><div><strong>%d</strong><span>Agents reporting</span></div><div><strong>v%d</strong><span>Instructions</span></div></div>`, status, active, len(agents), revision)
	b.WriteString(`<section class="overview-section"><div class="overview-section-head"><h2>Global instructions</h2><a href="/instructions">Edit instructions →</a></div>`)
	if revision == 0 {
		b.WriteString(`<p class="muted">Tell your agents how you like to work.</p><a class="button" href="/instructions">Create instructions</a>`)
	} else {
		fmt.Fprintf(&b, `<div class="machine-row"><div><h3>Shared working instructions</h3><p class="muted">All connected machines · Version %d</p></div><a href="/instructions">View history →</a></div>`, revision)
	}
	b.WriteString(`</section><section class="overview-section"><div class="overview-section-head"><h2>Connected machines</h2><a href="/machines">Manage machines →</a></div>`)
	if active == 0 {
		b.WriteString(`<p class="muted">Connect your first machine to receive instructions automatically.</p><a class="button secondary" href="/machines">Connect a machine</a>`)
	}
	for _, d := range devices {
		if d.RevokedAt != "" {
			continue
		}
		fmt.Fprintf(&b, `<div class="machine-row"><div><h3>%s</h3><p class="muted">%s</p></div><div class="agent-status-list">`, html.EscapeString(d.Name), html.EscapeString(d.Platform))
		if len(reports[d.ID]) == 0 {
			b.WriteString(`<span class="muted">Awaiting first report</span>`)
		}
		for _, s := range reports[d.ID] {
			fmt.Fprintf(&b, `<span>%s <strong>%s</strong> <small>v%d</small></span>`, html.EscapeString(s.Agent), agentStatusLabel(s, revision, now), s.Revision)
		}
		b.WriteString(`</div></div>`)
	}
	b.WriteString(`<p class="muted sync-explanation">Up to date means your instruction files have arrived. Start a new agent session to use them. Offline machines catch up when they reconnect.</p></section>`)
	return template.HTML(b.String())
}

func agentStatusLabel(status deviceAgentStatus, desired int, now time.Time) string {
	switch state := reportedAgentState(status, desired, now); state {
	case "Synced":
		return "Files up to date"
	case "Offline":
		return "Offline · updates on reconnect"
	default:
		return state
	}
}
