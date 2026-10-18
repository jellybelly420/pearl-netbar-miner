package platform

import (
	"context"
	"time"
)

type GPU struct {
	Index       int     `json:"index"`
	Name        string  `json:"name"`
	Utilization int     `json:"utilization_percent"`
	Temperature int     `json:"temperature_c"`
	PowerWatts  float64 `json:"power_watts"`
}

type Snapshot struct {
	CapturedAt       time.Time `json:"captured_at"`
	IdleSeconds      int64     `json:"idle_seconds"`
	Foreground       string    `json:"foreground"`
	Fullscreen       bool      `json:"fullscreen"`
	RunningProcesses []string  `json:"running_processes"`
	ProcessScanKnown bool      `json:"process_scan_available"`
	OtherGPUProcess  bool      `json:"other_gpu_process"`
	GPUProcessKnown  bool      `json:"gpu_process_sensor_available"`
	GPUs             []GPU     `json:"gpus"`
	GPUMetricsKnown  bool      `json:"gpu_metrics_available"`
	SensingAvailable bool      `json:"sensing_available"`
}

type Sensor interface {
	Snapshot(context.Context, []string) (Snapshot, error)
}

func NewSensor() Sensor {
	return newSensor()
}

type PowerAction string

const (
	PowerReboot   PowerAction = "reboot"
	PowerShutdown PowerAction = "shutdown"
)

func RequestPowerAction(action PowerAction) error {
	return requestPowerAction(action)
}
