//go:build windows

package skillsdex

import (
	"bytes"
	"testing"
)

func TestDeviceConnectionDPAPIProtection(t *testing.T) {
	plain := []byte(`{"server":"https://example.com","credential":"sensitive-test-credential"}`)
	protected, err := protectDeviceConnection(plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(protected, []byte("sensitive-test-credential")) {
		t.Fatal("credential persisted as plaintext")
	}
	restored, err := unprotectDeviceConnection(protected)
	if err != nil || !bytes.Equal(restored, plain) {
		t.Fatalf("DPAPI round trip failed: %v", err)
	}
	protected[len(protected)-1] ^= 1
	if _, err := unprotectDeviceConnection(protected); err == nil {
		t.Fatal("tampered data accepted")
	}
	if _, err := unprotectDeviceConnection(plain); err == nil {
		t.Fatal("plaintext accepted on Windows")
	}
}
