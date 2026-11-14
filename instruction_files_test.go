package skillsdex

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestInstructionFilePreservesChangesAndBacksUp(t *testing.T) {
	for _, agent := range []string{"codex", "claude", "gemini"} {
		t.Run(agent, func(t *testing.T) {
			path, err := instructionPath(t.TempDir(), agent, "")
			if err != nil {
				t.Fatal(err)
			}
			first := []byte("First instructions")
			if err := applyInstructionFile(path, "", first); err != nil {
				t.Fatal(err)
			}
			if err := applyInstructionFile(path, "", []byte("Unmanaged replacement")); !errors.Is(err, errLocalInstructionConflict) {
				t.Fatalf("unmanaged overwrite: %v", err)
			}
			second := []byte("Updated instructions")
			if err := applyInstructionFile(path, sha256String(first), second); err != nil {
				t.Fatal(err)
			}
			backups, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".skillsdex-backup-*"))
			if err != nil || len(backups) != 1 {
				t.Fatalf("backup missing: %v", err)
			}
			backup, err := os.ReadFile(backups[0])
			if err != nil || string(backup) != string(first) {
				t.Fatal("backup did not preserve original")
			}
			if err := os.WriteFile(path, []byte("Local customization"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := applyInstructionFile(path, sha256String(second), first); !errors.Is(err, errLocalInstructionConflict) {
				t.Fatalf("local edit overwritten: %v", err)
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != "Local customization" {
				t.Fatal("local edit lost")
			}
		})
	}
}
