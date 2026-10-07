//go:build windows

package harness

import "os/exec"

func prepare(cmd *exec.Cmd) {}
func kill(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
