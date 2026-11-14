package skillsdex

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var errInstructionConflict = errors.New("instructions changed since you opened this page; reload and compare before saving")

const starterGlobalInstructions = `# Working preferences

Prefer targeted file reads over loading entire directories. Avoid repeatedly reading unchanged files. Preserve prompt caching where supported. Monitor context growth and recommend compaction or a fresh session when appropriate. Use actual token metrics when available rather than inventing estimates. Prioritise correctness over token savings.
`

type instructionRevision struct {
	Revision  int
	Body      string
	CreatedAt string
}

func instructionHistory(ctx context.Context, db *sql.DB, userID string) ([]instructionRevision, error) {
	rows, err := db.QueryContext(ctx, `SELECT revision, body, created_at FROM instruction_revisions WHERE user_id = ? ORDER BY revision DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var history []instructionRevision
	for rows.Next() {
		var revision instructionRevision
		if err := rows.Scan(&revision.Revision, &revision.Body, &revision.CreatedAt); err != nil {
			return nil, err
		}
		history = append(history, revision)
	}
	return history, rows.Err()
}

func saveInstructions(ctx context.Context, db *sql.DB, userID string, expected int, body string, restore int) error {
	if expected < 0 || restore < 0 || len(body) > 65536 {
		return errors.New("invalid instruction revision or content exceeds 64 KiB")
	}
	if restore > 0 {
		if err := db.QueryRowContext(ctx, `SELECT body FROM instruction_revisions WHERE user_id = ? AND revision = ?`, userID, restore).Scan(&body); err != nil {
			return err
		}
	}
	result, err := db.ExecContext(ctx, `INSERT INTO instruction_revisions (user_id, revision, body, created_at, restored_from)
		SELECT ?, ?, ?, ?, NULLIF(?, 0) WHERE COALESCE((SELECT MAX(revision) FROM instruction_revisions WHERE user_id = ?), 0) = ?`,
		userID, expected+1, body, time.Now().UTC().Format(time.RFC3339), restore, userID, expected)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return errInstructionConflict
	}
	return nil
}

func (a *app) handleInstructions(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/instructions" {
		http.NotFound(w, r)
		return
	}
	u := webUser(r)
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 128*1024)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid instruction form", http.StatusBadRequest)
			return
		}
		expected, err := strconv.Atoi(r.FormValue("revision"))
		if err != nil {
			http.Error(w, "invalid revision", http.StatusBadRequest)
			return
		}
		restore := 0
		if value := r.FormValue("restore"); value != "" {
			restore, err = strconv.Atoi(value)
			if err != nil || restore <= 0 {
				http.Error(w, "invalid restore revision", http.StatusBadRequest)
				return
			}
		}
		err = saveInstructions(r.Context(), a.db, u.ID, expected, r.FormValue("body"), restore)
		if err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, errInstructionConflict) {
				history, loadErr := instructionHistory(r.Context(), a.db, u.ID)
				if loadErr != nil {
					http.Error(w, "could not reload instructions", http.StatusInternalServerError)
					return
				}
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusConflict)
				message := template.HTML(`<div class="warning" role="alert">Another change was saved while you were editing. Your draft is preserved below. Compare it with the latest version in history before saving again.</div>`)
				if restore > 0 {
					message = template.HTML(`<div class="warning" role="alert">Instructions changed before this restore completed. Review the latest history and choose a version again.</div>`)
					a.render(w, r, u, "Instructions", message+instructionsPage(history))
				} else {
					a.render(w, r, u, "Instructions", message+instructionsPage(history, r.FormValue("body")))
				}
				return
			}
			http.Error(w, err.Error(), status)
			return
		}
		a.audit(r, u, "instructions_saved", "instructions", u.ID, "Global instructions", map[string]any{"revision": expected + 1, "restored_from": restore})
		http.Redirect(w, r, "/instructions", http.StatusSeeOther)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	history, err := instructionHistory(r.Context(), a.db, u.ID)
	if err != nil {
		http.Error(w, "could not load instructions", http.StatusInternalServerError)
		return
	}
	a.render(w, r, u, "Instructions", instructionsPage(history))
}

func instructionsPage(history []instructionRevision, draft ...string) template.HTML {
	current := instructionRevision{Body: starterGlobalInstructions}
	if len(history) > 0 {
		current = history[0]
	}
	if len(draft) > 0 {
		current.Body = draft[0]
	}
	var b strings.Builder
	b.WriteString(`<header class="pagehead"><div><h1>Instructions</h1><p>Write once. Apply across your machines.</p></div></header><div class="instructions-workspace"><section class="panel instruction-editor"><form method="post" action="/instructions">`)
	fmt.Fprintf(&b, `<input type="hidden" name="revision" value="%d"><label for="instruction-body">Instructions</label><p class="muted" id="instruction-help">Changes sync to machines running SkillsDex. Start a new agent session to load them.</p><textarea id="instruction-body" name="body" rows="20" aria-describedby="instruction-help" spellcheck="false">%s</textarea><div class="actions" style="margin-top:18px"><button type="submit">Save changes</button><span class="muted">Version %d</span></div></form></section><aside><details class="history-drawer"><summary>Version history</summary>`, current.Revision, html.EscapeString(current.Body), current.Revision)
	if len(history) == 0 {
		b.WriteString(`<p class="muted">Your first save creates version 1. Every later change is kept here.</p>`)
	}
	for _, revision := range history {
		fmt.Fprintf(&b, `<details class="instruction-history"><summary>Version %d <span class="muted">%s</span></summary><pre>%s</pre>`, revision.Revision, html.EscapeString(revision.CreatedAt), html.EscapeString(revision.Body))
		if revision.Revision != current.Revision {
			fmt.Fprintf(&b, `<form method="post" action="/instructions"><input type="hidden" name="revision" value="%d"><input type="hidden" name="restore" value="%d"><button class="secondary" type="submit">Restore as new version</button></form>`, current.Revision, revision.Revision)
		}
		b.WriteString(`</details>`)
	}
	b.WriteString(`</details></aside></div>`)
	if len(draft) > 0 {
		return template.HTML(strings.Replace(b.String(), `<details class="history-drawer">`, `<details class="history-drawer" open>`, 1))
	}
	return template.HTML(b.String())
}
