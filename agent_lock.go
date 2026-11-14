package skillsdex

import (
	"fmt"
	"os"
)

func acquireAgentLock(path string) (*os.File, error) {
	if err := rejectInstructionLinks(path); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := lockAgentFile(file); err != nil {
		file.Close()
		return nil, fmt.Errorf("another agent sync process may be running: %w", err)
	}
	// Closing the handle releases the OS lock, including on process exit.
	// Keep the file in place so another process cannot lock a different inode.
	return file, nil
}
