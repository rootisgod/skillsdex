//go:build windows

package skillsdex

import (
	"os/exec"
	"syscall"
)

func hideAgentCommand(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} }
