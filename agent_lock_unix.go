//go:build !windows

package skillsdex

import (
	"golang.org/x/sys/unix"
	"os"
)

func lockAgentFile(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
}
