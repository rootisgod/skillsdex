package skillsdex

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type InstructionManifest struct {
	Revision int    `json:"revision"`
	Body     string `json:"body"`
	Hash     string `json:"hash"`
}

type AgentSyncReport struct {
	Agent    string `json:"agent"`
	Revision int    `json:"revision"`
	State    string `json:"state"`
}

func (a *app) handleDeviceSync(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-cache")
	authorization := r.Header.Get("Authorization")
	if !strings.HasPrefix(authorization, "Bearer ") {
		writeJSONError(w, http.StatusUnauthorized, "device authentication required")
		return
	}
	d, err := deviceForCredential(r.Context(), a.db, strings.TrimPrefix(authorization, "Bearer "))
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "invalid device credential")
		return
	}
	var manifest InstructionManifest
	err = a.db.QueryRowContext(r.Context(), `SELECT revision,body FROM instruction_revisions WHERE user_id=? ORDER BY revision DESC LIMIT 1`, d.UserID).Scan(&manifest.Revision, &manifest.Body)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		writeJSONError(w, http.StatusInternalServerError, "could not load configuration")
		return
	}
	manifest.Hash = sha256String([]byte(manifest.Body))
	if r.URL.Path == "/api/v1/sync/manifest" {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if _, err := a.db.ExecContext(r.Context(), `UPDATE devices SET last_seen_at=? WHERE id=? AND revoked_at IS NULL`, time.Now().UTC().Format(time.RFC3339), d.ID); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "could not record connection")
			return
		}
		etag := fmt.Sprintf(`"%d-%s"`, manifest.Revision, manifest.Hash)
		w.Header().Set("ETag", etag)
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		writeJSON(w, manifest)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var report AgentSyncReport
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&report); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid sync report")
		return
	}
	if report.Agent != "codex" && report.Agent != "claude" && report.Agent != "gemini" {
		writeJSONError(w, http.StatusBadRequest, "unsupported agent")
		return
	}
	if report.State != "synced" && report.State != "conflict" && report.State != "error" && report.State != "local" {
		writeJSONError(w, http.StatusBadRequest, "invalid sync state")
		return
	}
	if report.Revision < 0 || report.Revision > manifest.Revision || (report.State == "synced" && report.Revision == 0) {
		writeJSONError(w, http.StatusBadRequest, "invalid applied revision")
		return
	}
	_, err = a.db.ExecContext(r.Context(), `INSERT INTO device_agent_status(device_id,agent,applied_revision,state,reported_at) VALUES(?,?,?,?,?) ON CONFLICT(device_id,agent) DO UPDATE SET applied_revision=excluded.applied_revision,state=excluded.state,reported_at=excluded.reported_at`, d.ID, report.Agent, report.Revision, report.State, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not save sync status")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
