//go:build !windows

package skillsdex

import "os/exec"

func hideAgentCommand(cmd *exec.Cmd) {}
