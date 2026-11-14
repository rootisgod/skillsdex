package skillsdex

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestKeepMineSurvivesSharedUpdatesAndUseSharedBacksUp(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	code, err := createDeviceEnrolment(ctx, a.DB, a.Admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	d, credential, err := registerDevice(ctx, a.DB, code, "Conflict test", "windows")
	if err != nil {
		t.Fatal(err)
	}
	if err := saveInstructions(ctx, a.DB, a.Admin.ID, 0, "Shared", 0); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "AGENTS.md")
	if err := os.WriteFile(path, []byte("Mine"), 0600); err != nil {
		t.Fatal(err)
	}
	state := instructionClientState{}
	client := instructionSyncClient{Base: a.Server.URL, Credential: credential, HTTP: a.Server.Client()}
	targets := map[string]string{"codex": path}
	persist := func() error { return nil }
	if err := client.syncOnce(ctx, &state, targets, persist); err == nil {
		t.Fatal("expected initial conflict")
	}
	if err := resolveInstructionChoice(&state, "codex", path, "Mine", "--keep-mine"); err != nil {
		t.Fatal(err)
	}
	if err := saveInstructions(ctx, a.DB, a.Admin.ID, 1, "New shared", 0); err != nil {
		t.Fatal(err)
	}
	if err := client.syncOnce(ctx, &state, targets, persist); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "Mine" {
		t.Fatal("local preference overwritten")
	}
	reports, err := loadDeviceReports(ctx, a.DB, a.Admin.ID)
	if err != nil || reports[d.ID][0].State != "local" {
		t.Fatalf("local state not reported: %v", err)
	}
	if err := resolveInstructionChoice(&state, "codex", path, "stale review", "--use-shared"); err == nil {
		t.Fatal("stale local review overwritten")
	}
	if err := resolveInstructionChoice(&state, "codex", path, "Mine", "--use-shared"); err != nil {
		t.Fatal(err)
	}
	content, err = os.ReadFile(path)
	if err != nil || string(content) != "New shared" {
		t.Fatal("shared choice not applied")
	}
	backups, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".skillsdex-backup-*"))
	if err != nil || len(backups) != 1 {
		t.Fatal("local backup missing")
	}
	content, err = os.ReadFile(backups[0])
	if err != nil || string(content) != "Mine" {
		t.Fatal("backup lost local content")
	}
	if state.Applied["codex"].KeepLocal {
		t.Fatal("local preference not cleared")
	}
}
