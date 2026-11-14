package skillsdex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestInstructionClientRejectsBadDownloadsWithoutTouchingFiles(t *testing.T) {
	for _, tc := range []struct {
		name     string
		manifest InstructionManifest
	}{
		{"bad hash", InstructionManifest{Revision: 3, Body: "Bad", Hash: "wrong"}},
		{"old revision", InstructionManifest{Revision: 1, Body: "Old", Hash: sha256String([]byte("Old"))}},
		{"unversioned content", InstructionManifest{Revision: 0, Body: "Unversioned", Hash: sha256String([]byte("Unversioned"))}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(tc.manifest) }))
			defer server.Close()
			path := filepath.Join(t.TempDir(), "AGENTS.md")
			if err := os.WriteFile(path, []byte("Keep"), 0600); err != nil {
				t.Fatal(err)
			}
			state := instructionClientState{Manifest: InstructionManifest{Revision: 2, Body: "Keep", Hash: sha256String([]byte("Keep"))}}
			client := instructionSyncClient{Base: server.URL, HTTP: server.Client()}
			persisted := false
			if err := client.syncOnce(context.Background(), &state, map[string]string{"codex": path}, func() error { persisted = true; return nil }); err == nil {
				t.Fatal("invalid manifest accepted")
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != "Keep" || persisted || state.Manifest.Revision != 2 {
				t.Fatal("invalid response changed local state")
			}
		})
	}
}

func TestInstructionClientOfflineAndCancellationPreserveFiles(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "offline", 503) }))
	defer server.Close()
	client := instructionSyncClient{Base: server.URL, HTTP: server.Client()}
	path := filepath.Join(t.TempDir(), "AGENTS.md")
	if err := os.WriteFile(path, []byte("Last valid instructions"), 0600); err != nil {
		t.Fatal(err)
	}
	state := instructionClientState{}
	for _, cancelled := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		if cancelled {
			cancel()
		}
		err := client.syncOnce(ctx, &state, map[string]string{"codex": path}, func() error { t.Error("persist called during failed fetch"); return nil })
		cancel()
		if err == nil {
			t.Fatal("failed fetch reported success")
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != "Last valid instructions" {
			t.Fatal("offline operation modified instructions")
		}
	}
}

func TestInstructionClientDeliveryConflictAndRollback(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	code, err := createDeviceEnrolment(ctx, a.DB, a.Admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := registerDevice(ctx, a.DB, code, "Test client", "windows")
	if err != nil {
		t.Fatal(err)
	}
	client := instructionSyncClient{Base: a.Server.URL, Credential: token, HTTP: a.Server.Client()}
	state := instructionClientState{}
	home := t.TempDir()
	targets := map[string]string{}
	for _, agent := range []string{"codex", "claude", "gemini"} {
		path, err := instructionPath(home, agent, "")
		if err != nil {
			t.Fatal(err)
		}
		targets[agent] = path
	}
	persist := func() error { return saveInstructionClientState(filepath.Join(home, "state.json"), state) }
	if err := saveInstructions(ctx, a.DB, a.Admin.ID, 0, "Original", 0); err != nil {
		t.Fatal(err)
	}
	if err := client.syncOnce(ctx, &state, targets, persist); err != nil {
		t.Fatal(err)
	}
	if err := client.syncOnce(ctx, &state, targets, persist); err != nil {
		t.Fatalf("unchanged sync: %v", err)
	}
	if err := os.WriteFile(targets["claude"], []byte("Local change"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := saveInstructions(ctx, a.DB, a.Admin.ID, 1, "Updated", 0); err != nil {
		t.Fatal(err)
	}
	if err := client.syncOnce(ctx, &state, targets, persist); err == nil {
		t.Fatal("conflict not reported")
	}
	for agent, path := range targets {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		want := "Updated"
		if agent == "claude" {
			want = "Local change"
		}
		if string(got) != want {
			t.Fatalf("%s: %q", agent, got)
		}
	}
	if err := saveInstructions(ctx, a.DB, a.Admin.ID, 2, "", 1); err != nil {
		t.Fatal(err)
	}
	delete(targets, "claude")
	if err := client.syncOnce(ctx, &state, targets, persist); err != nil {
		t.Fatal(err)
	}
	for agent, path := range targets {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != "Original" {
			t.Fatalf("rollback failed for %s", agent)
		}
	}
}
