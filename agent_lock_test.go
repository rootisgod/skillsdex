package skillsdex

import (
	"path/filepath"
	"testing"
)

func TestAgentLockExcludesConcurrentWriterAndReleases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sync.lock")
	first, err := acquireAgentLock(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := acquireAgentLock(path)
	if err == nil {
		second.Close()
		first.Close()
		t.Fatal("concurrent watcher acquired lock")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	third, err := acquireAgentLock(path)
	if err != nil {
		t.Fatalf("lock not released: %v", err)
	}
	if err := third.Close(); err != nil {
		t.Fatal(err)
	}
}
