package policy

import (
	"errors"
	"path/filepath"
	"strconv"
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
	PauseOnGPUUtilization      bool     `json:"pause_on_gpu_utilization"`
	GPUUtilizationThreshold    int      `json:"gpu_utilization_threshold"`
	BlockedProcesses           []string `json:"blocked_processes"`
	AllowedFullscreenProcesses []string `json:"allowed_fullscreen_processes"`
	ScheduleEnabled            bool     `json:"schedule_enabled"`
	ScheduleStart              string   `json:"schedule_start"`
	ScheduleStop               string   `json:"schedule_stop"`
	RestAfterMinutes           int      `json:"rest_after_minutes"`
	RestDurationMinutes        int      `json:"rest_duration_minutes"`
}

type Signals struct {
	Now               time.Time `json:"now"`
	IdleFor           time.Duration
	Foreground        string   `json:"foreground"`
	Fullscreen        bool     `json:"fullscreen"`
	RunningProcesses  []string `json:"running_processes"`
	ProcessScanKnown  bool     `json:"process_scan_available"`
	OtherGPUProcess   bool     `json:"other_gpu_process"`
	GPUProcessKnown   bool     `json:"gpu_process_sensor_available"`
	MaxGPUUtilization int      `json:"max_gpu_utilization_percent"`
	GPUMetricsKnown   bool     `json:"gpu_metrics_available"`
}

type Runtime struct {
	MinerRunning bool
	ManualPaused bool
	LastStopped  time.Time
	RunStarted   time.Time
	RestUntil    time.Time
}

type Action string

const (
	ActionStart Action = "start"
	ActionStop  Action = "stop"
	ActionHold  Action = "hold"
)

type Decision struct {
	Action         Action `json:"action"`
	Reason         string `json:"reason"`
	RestForSeconds int    `json:"rest_for_seconds,omitempty"`
}

func (c Config) Valid() bool {
	switch c.Mode {
	case ModeFull, ModeAvoid, ModeSmart, ModeManual:
		if c.StartIdleSeconds < 0 || c.StopIdleBelowSeconds < 0 || c.ResumeCooldownSeconds < 0 {
			return false
		}
	default:
		return false
	}
	if c.GPUUtilizationThreshold < 0 || c.GPUUtilizationThreshold > 100 {
		return false
	}
	if c.RestAfterMinutes < 0 || c.RestDurationMinutes < 0 {
		return false
	}
	if (c.RestAfterMinutes == 0) != (c.RestDurationMinutes == 0) {
		return false
	}
	if c.ScheduleEnabled {
		if _, ok := minuteOfDay(c.ScheduleStart); !ok {
			return false
		}
		if _, ok := minuteOfDay(c.ScheduleStop); !ok {
			return false
		}
	}
	return true
}

func Evaluate(c Config, s Signals, r Runtime) Decision {
	if r.ManualPaused {
		return desired(false, r.MinerRunning, "operator pause")
	}
	if c.ScheduleEnabled && !withinDailyWindow(s.Now, c.ScheduleStart, c.ScheduleStop) {
		return desired(false, r.MinerRunning, "outside scheduled mining window")
	}
	if r.RestUntil.After(s.Now) {
		return desired(false, r.MinerRunning, "scheduled rest in progress")
	}
	if r.MinerRunning && c.RestAfterMinutes > 0 && !r.RunStarted.IsZero() &&
		s.Now.Sub(r.RunStarted) >= time.Duration(c.RestAfterMinutes)*time.Minute {
		return Decision{Action: ActionStop, Reason: "scheduled rest due", RestForSeconds: c.RestDurationMinutes * 60}
	}
	if c.Mode == ModeManual {
		return Decision{Action: ActionHold, Reason: "manual mode"}
	}
	if c.Mode == ModeFull {
		return desired(true, r.MinerRunning, "full mode")
	}

	foreground := normalizeProcess(s.Foreground)
	if blocked := firstBlockedProcess(c.BlockedProcesses, append([]string{foreground}, s.RunningProcesses...)); blocked != "" {
		return desired(false, r.MinerRunning, "blocked process: "+blocked)
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
	// Total utilization includes this miner while it is running. Use the
	// threshold as a conservative start gate; running games are caught by the
	// process/full-screen checks above without making the miner stop itself.
	if c.PauseOnGPUUtilization && !r.MinerRunning {
		if !s.GPUMetricsKnown {
			return Decision{Action: ActionHold, Reason: "GPU utilization sensor unavailable"}
		}
		if s.MaxGPUUtilization >= c.GPUUtilizationThreshold {
			return Decision{Action: ActionHold, Reason: "GPU utilization is above threshold"}
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

// ApplyOverride applies the allowlisted fleet policy fields. It deliberately
// rejects unknown keys so the control plane cannot become an arbitrary config
// or command channel.
func ApplyOverride(base Config, values map[string]string) (Config, error) {
	candidate := base
	for key, value := range values {
		var err error
		switch key {
		case "mode":
			candidate.Mode = Mode(value)
		case "start_idle_seconds":
			candidate.StartIdleSeconds, err = strconv.Atoi(value)
		case "stop_idle_below_seconds":
			candidate.StopIdleBelowSeconds, err = strconv.Atoi(value)
		case "resume_cooldown_seconds":
			candidate.ResumeCooldownSeconds, err = strconv.Atoi(value)
		case "pause_on_fullscreen":
			candidate.PauseOnFullscreen, err = strconv.ParseBool(value)
		case "pause_on_other_gpu_process":
			candidate.PauseOnOtherGPUProcess, err = strconv.ParseBool(value)
		case "pause_on_gpu_utilization":
			candidate.PauseOnGPUUtilization, err = strconv.ParseBool(value)
		case "gpu_utilization_threshold":
			candidate.GPUUtilizationThreshold, err = strconv.Atoi(value)
		case "blocked_processes":
			candidate.BlockedProcesses = splitList(value)
		case "allowed_fullscreen_processes":
			candidate.AllowedFullscreenProcesses = splitList(value)
		case "schedule_enabled":
			candidate.ScheduleEnabled, err = strconv.ParseBool(value)
		case "schedule_start":
			candidate.ScheduleStart = value
		case "schedule_stop":
			candidate.ScheduleStop = value
		case "rest_after_minutes":
			candidate.RestAfterMinutes, err = strconv.Atoi(value)
		case "rest_duration_minutes":
			candidate.RestDurationMinutes, err = strconv.Atoi(value)
		default:
			return Config{}, errors.New("unsupported policy field: " + key)
		}
		if err != nil {
			return Config{}, errors.New("invalid policy field " + key)
		}
	}
	if !candidate.Valid() {
		return Config{}, errors.New("invalid policy configuration")
	}
	return candidate, nil
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

func firstBlockedProcess(blocked, running []string) string {
	blockedSet := make(map[string]struct{}, len(blocked))
	for _, value := range blocked {
		if normalized := normalizeProcess(value); normalized != "" {
			blockedSet[normalized] = struct{}{}
		}
	}
	for _, value := range running {
		normalized := normalizeProcess(value)
		if _, found := blockedSet[normalized]; found {
			return normalized
		}
	}
	return ""
}

func splitList(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' || r == '\n' })
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if normalized := normalizeProcess(part); normalized != "" {
			result = append(result, normalized)
		}
	}
	return result
}

func minuteOfDay(value string) (int, bool) {
	parsed, err := time.Parse("15:04", value)
	if err != nil {
		return 0, false
	}
	return parsed.Hour()*60 + parsed.Minute(), true
}

func withinDailyWindow(now time.Time, startValue, stopValue string) bool {
	start, startOK := minuteOfDay(startValue)
	stop, stopOK := minuteOfDay(stopValue)
	if !startOK || !stopOK {
		return false
	}
	if start == stop {
		return true
	}
	current := now.Hour()*60 + now.Minute()
	if start < stop {
		return current >= start && current < stop
	}
	return current >= start || current < stop
}
