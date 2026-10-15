//go:build !windows

package platform

import (
	"context"
	"errors"
)

type unsupportedSensor struct{}

func newSensor() Sensor { return unsupportedSensor{} }

func (unsupportedSensor) Snapshot(context.Context, []string) (Snapshot, error) {
	return Snapshot{}, errors.New("desktop sensing is supported only on Windows")
}
