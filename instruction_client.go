package skillsdex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

type instructionApplied struct {
	KeepLocal bool   `json:"keep_local,omitempty"`
	Path      string `json:"path"`
	Hash      string `json:"hash"`
	Revision  int    `json:"revision"`
}

type instructionClientState struct {
	ETag     string                        `json:"etag"`
	Manifest InstructionManifest           `json:"manifest"`
	Applied  map[string]instructionApplied `json:"applied"`
}

type instructionSyncClient struct {
	ExpectedRevision int
	Base             string
	Credential       string
	HTTP             *http.Client
}

func (c instructionSyncClient) request(ctx context.Context, method, path string, body []byte, etag string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.Base, "/")+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Credential)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	return c.HTTP.Do(req)
}

func (c instructionSyncClient) syncOnce(ctx context.Context, state *instructionClientState, targets map[string]string, persist func() error) error {
	res, err := c.request(ctx, http.MethodGet, "/api/v1/sync/manifest", nil, state.ETag)
	if err != nil {
		return fmt.Errorf("fetch instructions: %w", err)
	}
	if res.StatusCode == http.StatusOK {
		var manifest InstructionManifest
		decodeErr := json.NewDecoder(io.LimitReader(res.Body, 512*1024)).Decode(&manifest)
		closeErr := res.Body.Close()
		if decodeErr != nil {
			return fmt.Errorf("decode instructions: %w", decodeErr)
		}
		if closeErr != nil {
			return closeErr
		}
		if len(manifest.Body) > 65536 || !utf8.ValidString(manifest.Body) || manifest.Revision < state.Manifest.Revision || manifest.Revision < 0 || (manifest.Revision == 0 && manifest.Body != "") || manifest.Hash != sha256String([]byte(manifest.Body)) {
			return errors.New("invalid instruction manifest")
		}
		if c.ExpectedRevision > 0 && manifest.Revision != c.ExpectedRevision {
			return errors.New("shared instructions changed during review; run setup again to review the latest version")
		}
		state.Manifest = manifest
		state.ETag = res.Header.Get("ETag")
	} else {
		if err := res.Body.Close(); err != nil {
			return err
		}
		if res.StatusCode != http.StatusNotModified {
			return fmt.Errorf("instruction server returned %d", res.StatusCode)
		}
		if state.Manifest.Hash != sha256String([]byte(state.Manifest.Body)) {
			return errors.New("cached instructions failed integrity check")
		}
	}
	if c.ExpectedRevision > 0 && state.Manifest.Revision != c.ExpectedRevision {
		return errors.New("shared instructions changed during review; run setup again")
	}
	if state.Manifest.Revision == 0 {
		return persist()
	}
	if state.Applied == nil {
		state.Applied = make(map[string]instructionApplied)
	}
	var failures []error
	for agent, path := range targets {
		if err := ctx.Err(); err != nil {
			return err
		}
		prior := state.Applied[agent]
		baseline := prior.Hash
		if prior.Path != path {
			baseline = ""
		}
		report := AgentSyncReport{Agent: agent, Revision: prior.Revision, State: "synced"}
		var applyErr error
		if prior.KeepLocal && prior.Path == path {
			report.State = "local"
		} else {
			applyErr = applyInstructionFile(path, baseline, []byte(state.Manifest.Body))
		}
		if applyErr != nil {
			report.State = "error"
			if errors.Is(applyErr, errLocalInstructionConflict) {
				report.State = "conflict"
			}
			failures = append(failures, fmt.Errorf("%s: %w", agent, applyErr))
		} else if report.State != "local" {
			state.Applied[agent] = instructionApplied{Path: path, Hash: state.Manifest.Hash, Revision: state.Manifest.Revision}
			report.Revision = state.Manifest.Revision
		}
		// Persist each applied target before acknowledging it remotely. If this
		// fails, stop; identical file bytes allow recovery on the next cycle.
		if err := persist(); err != nil {
			return fmt.Errorf("save local sync state: %w", err)
		}
		body, err := json.Marshal(report)
		if err != nil {
			return err
		}
		response, err := c.request(ctx, http.MethodPost, "/api/v1/sync/status", body, "")
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if err := response.Body.Close(); err != nil {
			failures = append(failures, err)
		}
		if response.StatusCode != http.StatusNoContent {
			failures = append(failures, fmt.Errorf("%s status report returned %d", agent, response.StatusCode))
		}
	}
	if err := persist(); err != nil {
		return err
	}
	return errors.Join(failures...)
}

func saveInstructionClientState(path string, state instructionClientState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	// State is not an agent instruction file and may exceed the content limit.
	if err := rejectInstructionLinks(path); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".sync-state-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
