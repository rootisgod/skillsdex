package skillsdex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDeviceRegistrationHTTP(t *testing.T) {
	a := newTestApp(t)
	token, err := createDeviceEnrolment(context.Background(), a.DB, a.Admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	input, _ := json.Marshal(map[string]string{"enrolment": token, "name": "Laptop", "platform": "linux"})
	request := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/v1/devices/register", strings.NewReader(string(input)))
		a.Mux.ServeHTTP(w, r)
		return w
	}
	w := request()
	if w.Code != http.StatusCreated {
		t.Fatalf("registration: %d %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("unsafe response headers: %v", w.Header())
	}
	var response struct {
		Device     Device `json:"device"`
		Credential string `json:"credential"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Device.Name != "Laptop" || response.Credential == "" {
		t.Fatal("incomplete registration response")
	}
	if request().Code != http.StatusBadRequest {
		t.Fatal("registration replay accepted")
	}
	page := string(machinesPage([]Device{response.Device}, ""))
	if strings.Contains(page, response.Credential) || strings.Contains(page, "All synced") {
		t.Fatal("machine page exposes credential or claims unverified sync")
	}
}

func TestDeviceEnrolmentConsumptionAndRevocation(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	token, err := createDeviceEnrolment(ctx, a.DB, a.Admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	d, credential, err := registerDevice(ctx, a.DB, token, "Main desktop", "windows")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := registerDevice(ctx, a.DB, token, "Replay", "windows"); err == nil {
		t.Fatal("enrolment replay succeeded")
	}
	if _, err := deviceForCredential(ctx, a.DB, credential); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := a.DB.QueryRow(`SELECT credential_hash FROM devices WHERE id=?`, d.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == credential || stored != apiTokenHash(credential) {
		t.Fatal("credential not stored as hash")
	}
	if err := revokeDevice(ctx, a.DB, "another-user", d.ID); err == nil {
		t.Fatal("cross-user revocation succeeded")
	}
	other, err := listDevices(ctx, a.DB, "another-user")
	if err != nil || len(other) != 0 {
		t.Fatal("device list leaked")
	}
	if err := revokeDevice(ctx, a.DB, a.Admin.ID, d.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := deviceForCredential(ctx, a.DB, credential); err == nil {
		t.Fatal("revoked credential accepted")
	}
}

func TestExpiredDeviceEnrolment(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	token, err := createDeviceEnrolment(ctx, a.DB, a.Admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.DB.Exec(`UPDATE device_enrolments SET expires_at='2000-01-01T00:00:00Z'`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := registerDevice(ctx, a.DB, token, "Laptop", "darwin"); err == nil {
		t.Fatal("expired token accepted")
	}
}
