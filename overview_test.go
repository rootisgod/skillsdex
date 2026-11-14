package skillsdex

import (
	"strings"
	"testing"
	"time"
)

func TestReportedAgentState(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		name, state string
		revision    int
		age         time.Duration
		want        string
	}{
		{"current", "synced", 2, 0, "Synced"},
		{"stale revision", "synced", 1, 0, "Update pending"},
		{"offline", "synced", 2, 4 * time.Minute, "Offline"},
		{"conflict", "conflict", 1, 0, "Local changes"},
		{"error", "error", 1, 0, "Needs attention"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := deviceAgentStatus{State: tc.state, Revision: tc.revision, ReportedAt: now.Add(-tc.age).Format(time.RFC3339)}
			if got := reportedAgentState(s, 2, now); got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestOverviewDoesNotInventSyncStatus(t *testing.T) {
	page := string(overviewPage([]Device{{ID: "device", Name: "<script>"}}, nil, nil, time.Now()))
	if strings.Contains(page, "Everything is up to date") || strings.Contains(page, "<script>") || !strings.Contains(page, "Awaiting first report") {
		t.Fatal("overview misrepresents unreported device or fails escaping")
	}
}

func TestOfflineMachineIsNotAnActionableError(t *testing.T) {
	now := time.Now().UTC()
	reports := map[string][]deviceAgentStatus{"d": {{Agent: "codex", Revision: 1, State: "synced", ReportedAt: now.Add(-time.Hour).Format(time.RFC3339)}}}
	page := string(overviewPage([]Device{{ID: "d", Name: "Sleeping laptop"}}, []instructionRevision{{Revision: 2}}, reports, now))
	if strings.Contains(page, "Needs your attention") || !strings.Contains(page, "1 machine offline") || !strings.Contains(page, "updates on reconnect") {
		t.Fatal("sleeping machine presented as error")
	}
	reports["d"][0] = deviceAgentStatus{Agent: "codex", Revision: 1, State: "conflict", ReportedAt: now.Format(time.RFC3339)}
	page = string(overviewPage([]Device{{ID: "d"}}, []instructionRevision{{Revision: 2}}, reports, now))
	if !strings.Contains(page, "Needs your attention") {
		t.Fatal("conflict is not actionable")
	}
}
