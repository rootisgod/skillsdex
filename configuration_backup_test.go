package skillsdex

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestConfigurationBackupRoundTripPreservesHistoryWithoutCredentials(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	for i, body := range []string{"First policy", "Current policy"} {
		if err := saveInstructions(ctx, a.DB, a.Admin.ID, i, body, 0); err != nil {
			t.Fatal(err)
		}
	}
	code, err := createDeviceEnrolment(ctx, a.DB, a.Admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, credential, err := registerDevice(ctx, a.DB, code, "Desktop", "windows")
	if err != nil {
		t.Fatal(err)
	}
	archive, err := buildSkillsExport(ctx, a.DB, a.Admin)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := readConfigurationBackup(archive)
	if err != nil {
		t.Fatal(err)
	}
	if backup == nil || len(backup.Instructions) != 2 || len(backup.Machines) != 1 {
		t.Fatalf("incomplete backup: %+v", backup)
	}
	encoded, err := json.Marshal(backup)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{credential, apiTokenHash(credential), code} {
		if strings.Contains(string(encoded), secret) {
			t.Fatal("backup leaked credential material")
		}
	}
	if err := saveInstructions(ctx, a.DB, a.Admin.ID, 2, "Keep existing history", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := a.App.restoreSkillsDexExport(ctx, a.Admin, archive); err != nil {
		t.Fatal(err)
	}
	history, err := instructionHistory(ctx, a.DB, a.Admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 5 || history[0].Body != "Current policy" || history[2].Body != "Keep existing history" {
		t.Fatalf("restore corrupted history: %+v", history)
	}
	summary, err := inspectSkillsDexBackup(archive)
	if err != nil {
		t.Fatal(err)
	}
	if summary.InstructionVersions != 2 || summary.MachineInventory != 1 {
		t.Fatalf("incomplete inspect summary: %+v", summary)
	}
}
