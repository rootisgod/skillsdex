package skillsdex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDeviceSyncManifestStatusAndRevocation(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	code, err := createDeviceEnrolment(ctx, a.DB, a.Admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	d, token, err := registerDevice(ctx, a.DB, code, "Test", "linux")
	if err != nil {
		t.Fatal(err)
	}
	if err := saveInstructions(ctx, a.DB, a.Admin.ID, 0, "Read targeted files.", 0); err != nil {
		t.Fatal(err)
	}
	request := func(method, path, body, credential, etag string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+credential)
		r.Header.Set("If-None-Match", etag)
		w := httptest.NewRecorder()
		a.Mux.ServeHTTP(w, r)
		return w
	}
	w := request("GET", "/api/v1/sync/manifest", "", token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("manifest: %d %s", w.Code, w.Body.String())
	}
	var m InstructionManifest
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m.Revision != 1 || m.Hash != sha256String([]byte(m.Body)) {
		t.Fatalf("invalid manifest: %+v", m)
	}
	if request("GET", "/api/v1/sync/manifest", "", token, w.Header().Get("ETag")).Code != 304 {
		t.Fatal("unchanged manifest downloaded again")
	}
	if request("GET", "/api/v1/sync/manifest", "", a.Token, "").Code != 401 {
		t.Fatal("library credential accepted as device")
	}
	if request("GET", "/v1/me", "", token, "").Code != 401 {
		t.Fatal("device credential accepted as library token")
	}
	if request("POST", "/api/v1/sync/status", `{"agent":"codex","revision":1,"state":"synced"}`, token, "").Code != 204 {
		t.Fatal("status rejected")
	}
	if request("POST", "/api/v1/sync/status", `{"agent":"codex","revision":2,"state":"synced"}`, token, "").Code != 400 {
		t.Fatal("future revision accepted")
	}
	if err := revokeDevice(ctx, a.DB, a.Admin.ID, d.ID); err != nil {
		t.Fatal(err)
	}
	if request("GET", "/api/v1/sync/manifest", "", token, "").Code != 401 {
		t.Fatal("revoked device still syncs")
	}
}
