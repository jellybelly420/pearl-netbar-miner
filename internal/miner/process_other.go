//go:build !windows

package miner

import "os/exec"

func configureProcess(*exec.Cmd) {}

func terminateProcess(command *exec.Cmd) error {
	return command.Process.Kill()
}
