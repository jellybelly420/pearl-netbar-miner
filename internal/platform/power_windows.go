//go:build windows

package platform

import (
	"fmt"
	"os/exec"
	"syscall"
)

func requestPowerAction(action PowerAction) error {
	var arguments []string
	switch action {
	case PowerReboot:
		arguments = []string{"/r", "/t", "15", "/d", "p:4:1", "/c", "Pearl fleet requested an authorized reboot"}
	case PowerShutdown:
		arguments = []string{"/s", "/t", "15", "/d", "p:4:1", "/c", "Pearl fleet requested an authorized shutdown"}
	default:
		return fmt.Errorf("unsupported power action %q", action)
	}
	command := exec.Command("shutdown.exe", arguments...)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return command.Run()
}
