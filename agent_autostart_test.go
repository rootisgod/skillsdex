package skillsdex

import (
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentServiceDefinitions(t *testing.T) {
	base := t.TempDir()
	executable := filepath.Join(base, "agent & $tool%", "skillsdex")
	for _, platform := range []string{"linux", "darwin"} {
		t.Run(platform, func(t *testing.T) {
			content, err := agentServiceDefinition(platform, executable, base, "a&b", "")
			if err != nil {
				t.Fatal(err)
			}
			if platform == "linux" {
				if !strings.Contains(content, "$$tool%%") || !strings.Contains(content, "UMask=0077") {
					t.Fatal(content)
				}
			} else {
				decoder := xml.NewDecoder(strings.NewReader(content))
				for {
					_, err := decoder.Token()
					if err == io.EOF {
						break
					}
					if err != nil {
						t.Fatal(err)
					}
				}
				if !strings.Contains(content, "a&amp;b") {
					t.Fatal(content)
				}
			}
		})
	}
	if _, err := agentServiceDefinition("linux", executable, base, "bad\nEnvironment=EVIL", ""); err == nil {
		t.Fatal("accepted line injection")
	}
	if _, err := agentServiceDefinition("linux", "relative", base, "", ""); err == nil {
		t.Fatal("accepted relative executable")
	}
}

func TestAgentServiceRetryPreservesCustomisation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "service")
	for i := 0; i < 2; i++ {
		if err := writeAgentService(path, "managed"); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(path, []byte("custom"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeAgentService(path, "managed"); err == nil {
		t.Fatal("overwrote customised service")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "custom" {
		t.Fatalf("%q: %v", data, err)
	}
}
