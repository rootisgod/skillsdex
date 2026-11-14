package skillsdex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAgentCLIConnectAndSync(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	code, err := createDeviceEnrolment(ctx, a.DB, a.Admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := saveInstructions(ctx, a.DB, a.Admin.ID, 0, "Prefer targeted file reads.", 0); err != nil {
		t.Fatal(err)
	}
	_, err = captureStdout(t, func() error {
		return withStdin(t, code+"\n", func() error {
			return runAgentCommand([]string{"connect", "--server", a.Server.URL, "--name", "CLI integration test"})
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	for _, agent := range []string{"codex", "claude", "gemini"} {
		name := agent
		if runtime.GOOS == "windows" {
			name += ".cmd"
		}
		if err := os.WriteFile(filepath.Join(bin, name), []byte(""), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	t.Setenv("CODEX_HOME", "")
	out, err := captureStdout(t, func() error { return runAgentCommand([]string{"sync"}) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "3 detected agents") {
		t.Fatalf("agent detection: %s", out)
	}
	confirmations := 0
	out, err = captureStdout(t, func() error {
		return runAgentSetupFlow(nil, func(prompt string) error {
			confirmations++
			if confirmations == 2 {
				return errors.New("automatic startup declined")
			}
			return nil
		})
	})
	if err != nil || confirmations != 2 || !strings.Contains(out, "Review your agents") || !strings.Contains(out, "Prefer targeted file reads.") || !strings.Contains(out, "agent autostart") {
		t.Fatalf("guided setup: %s; confirmations=%d; err=%v", out, confirmations, err)
	}
	// The user may read version 1 while another browser saves version 2.
	// Reject that change before any unreviewed content reaches local files.
	_, err = captureStdout(t, func() error {
		return runAgentSetupFlow(nil, func(prompt string) error {
			return saveInstructions(ctx, a.DB, a.Admin.ID, 1, "New unreviewed policy", 0)
		})
	})
	if err == nil || !strings.Contains(err.Error(), "changed during review") {
		t.Fatalf("stale review accepted: %v", err)
	}
	for _, agent := range []string{"codex", "claude", "gemini"} {
		path, err := instructionPath(a.Home, agent, "")
		if err != nil {
			t.Fatal(err)
		}
		content, err := os.ReadFile(path)
		if err != nil || string(content) != "Prefer targeted file reads." {
			t.Fatalf("%s output: %q, %v", agent, content, err)
		}
	}
	devices, err := listDevices(ctx, a.DB, a.Admin.ID)
	if err != nil || len(devices) != 1 {
		t.Fatalf("registered devices: %+v, %v", devices, err)
	}
	reports, err := loadDeviceReports(ctx, a.DB, a.Admin.ID)
	if err != nil || len(reports[devices[0].ID]) != 3 {
		t.Fatalf("missing acknowledgements: %+v, %v", reports, err)
	}
}

func TestDeviceServerTransportValidation(t *testing.T) {
	for _, tc := range []struct {
		url   string
		valid bool
	}{
		{"https://config.example.com", true},
		{"http://127.0.0.1:8765", true},
		{"http://[::1]:8765", true},
		{"http://example.com", false},
		{"https://user:password@example.com", false},
		{"https://example.com/?token=secret", false},
		{"https://example.com/path", false},
		{"file:///tmp/config", false},
	} {
		t.Run(tc.url, func(t *testing.T) {
			if got := validateDeviceServer(tc.url); (got == nil) != tc.valid {
				t.Fatalf("validation=%v, valid=%v", got, tc.valid)
			}
		})
	}
}
