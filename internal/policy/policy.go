package policy

import (
	"path/filepath"
	"strings"
	"time"
)

type Mode string

const (
	ModeFull   Mode = "full"
	ModeAvoid  Mode = "avoid"
	ModeSmart  Mode = "smart"
	ModeManual Mode = "manual"
)

type Config struct {
	Mode                       Mode     `json:"mode"`
	StartIdleSeconds           int      `json:"start_idle_seconds"`
	StopIdleBelowSeconds       int      `json:"stop_idle_below_seconds"`
	ResumeCooldownSeconds      int      `json:"resume_cooldown_seconds"`
	PauseOnFullscreen          bool     `json:"pause_on_fullscreen"`
	PauseOnOtherGPUProcess     bool     `json:"pause_on_other_gpu_process"`
	BlockedProcesses           []string `json:"blocked_processes"`
	AllowedFullscreenProcesses []string `json:"allowed_fullscreen_processes"`
}

type Signals struct {
	Now             time.Time `json:"now"`
	IdleFor         time.Duration
	Foreground      string `json:"foreground"`
	Fullscreen      bool   `json:"fullscreen"`
	OtherGPUProcess bool   `json:"other_gpu_process"`
	GPUProcessKnown bool   `json:"gpu_process_sensor_available"`
}

type Runtime struct {
	MinerRunning bool
	ManualPaused bool
	LastStopped  time.Time
}

type Action string

const (
	ActionStart Action = "start"
	ActionStop  Action = "stop"
	ActionHold  Action = "hold"
)

type Decision struct {
	Action Action `json:"action"`
	Reason string `json:"reason"`
}

func (c Config) Valid() bool {
	switch c.Mode {
	case ModeFull, ModeAvoid, ModeSmart, ModeManual:
		return c.StartIdleSeconds >= 0 && c.StopIdleBelowSeconds >= 0 && c.ResumeCooldownSeconds >= 0
	default:
		return false
	}
}

func Evaluate(c Config, s Signals, r Runtime) Decision {
	if r.ManualPaused {
		return desired(false, r.MinerRunning, "operator pause")
	}
	if c.Mode == ModeManual {
		return Decision{Action: ActionHold, Reason: "manual mode"}
	}
	if c.Mode == ModeFull {
		return desired(true, r.MinerRunning, "full mode")
	}

	foreground := normalizeProcess(s.Foreground)
	if containsProcess(c.BlockedProcesses, foreground) {
		return desired(false, r.MinerRunning, "blocked foreground process: "+foreground)
	}
	if c.PauseOnFullscreen && s.Fullscreen && !containsProcess(c.AllowedFullscreenProcesses, foreground) {
		return desired(false, r.MinerRunning, "unapproved fullscreen application")
	}
	if c.PauseOnOtherGPUProcess {
		if !s.GPUProcessKnown {
			return desired(false, r.MinerRunning, "GPU process sensor unavailable")
		}
		if s.OtherGPUProcess {
			return desired(false, r.MinerRunning, "another GPU process is active")
		}
	}
	if c.Mode == ModeAvoid {
		return desired(true, r.MinerRunning, "avoidance checks clear")
	}

	if r.MinerRunning && s.IdleFor < time.Duration(c.StopIdleBelowSeconds)*time.Second {
		return Decision{Action: ActionStop, Reason: "user activity detected"}
	}
	if !r.MinerRunning {
		if s.IdleFor < time.Duration(c.StartIdleSeconds)*time.Second {
			return Decision{Action: ActionHold, Reason: "waiting for user idle threshold"}
		}
		if !r.LastStopped.IsZero() && s.Now.Sub(r.LastStopped) < time.Duration(c.ResumeCooldownSeconds)*time.Second {
			return Decision{Action: ActionHold, Reason: "resume cooldown"}
		}
		return Decision{Action: ActionStart, Reason: "smart idle threshold reached"}
	}
	return Decision{Action: ActionHold, Reason: "smart mining continues"}
}

func desired(run, running bool, reason string) Decision {
	if run && !running {
		return Decision{Action: ActionStart, Reason: reason}
	}
	if !run && running {
		return Decision{Action: ActionStop, Reason: reason}
	}
	return Decision{Action: ActionHold, Reason: reason}
}

func normalizeProcess(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), `\`, "/")
	return strings.ToLower(filepath.Base(value))
}

func containsProcess(values []string, candidate string) bool {
	if candidate == "" {
		return false
	}
	for _, value := range values {
		if normalizeProcess(value) == candidate {
			return true
		}
	}
	return false
}
