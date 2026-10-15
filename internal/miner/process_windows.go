//go:build windows

package miner

import (
	"os/exec"
	"strconv"
	"syscall"
)

func configureProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}

func terminateProcess(command *exec.Cmd) error {
	if command.Process == nil {
		return nil
	}
	kill := exec.Command("taskkill", "/PID", strconv.Itoa(command.Process.Pid), "/T", "/F")
	kill.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if err := kill.Run(); err == nil {
		return nil
	}
	return command.Process.Kill()
}
