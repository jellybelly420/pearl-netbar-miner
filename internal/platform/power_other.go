//go:build !windows

package platform

import "fmt"

func requestPowerAction(action PowerAction) error {
	return fmt.Errorf("power action %q is supported only on Windows", action)
}
