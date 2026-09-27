//go:build windows

package command

import "os/exec"

func prepareAgent(cmd *exec.Cmd) {}
func killAgent(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
