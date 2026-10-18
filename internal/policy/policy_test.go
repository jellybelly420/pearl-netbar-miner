package policy

import (
	"testing"
	"time"
)

func TestSmartPolicyYieldsAndResumes(t *testing.T) {
	now := time.Unix(1000, 0)
	cfg := Config{Mode: ModeSmart, StartIdleSeconds: 300, StopIdleBelowSeconds: 15, ResumeCooldownSeconds: 60}

	got := Evaluate(cfg, Signals{Now: now, IdleFor: 5 * time.Second}, Runtime{MinerRunning: true})
	if got.Action != ActionStop {
		t.Fatalf("expected stop on activity, got %+v", got)
	}

	got = Evaluate(cfg, Signals{Now: now, IdleFor: 301 * time.Second}, Runtime{LastStopped: now.Add(-61 * time.Second)})
	if got.Action != ActionStart {
		t.Fatalf("expected start after idle and cooldown, got %+v", got)
	}
}

func TestBlockedProcessWins(t *testing.T) {
	cfg := Config{Mode: ModeAvoid, BlockedProcesses: []string{"CS2.EXE"}}
	got := Evaluate(cfg, Signals{Now: time.Now(), RunningProcesses: []string{`C:\Games\cs2.exe`}}, Runtime{MinerRunning: true})
	if got.Action != ActionStop {
		t.Fatalf("expected blocked process to stop miner, got %+v", got)
	}
}

func TestScheduleAcrossMidnight(t *testing.T) {
	cfg := Config{Mode: ModeFull, ScheduleEnabled: true, ScheduleStart: "22:00", ScheduleStop: "07:00"}
	inside := Evaluate(cfg, Signals{Now: time.Date(2026, 10, 17, 23, 0, 0, 0, time.Local)}, Runtime{})
	if inside.Action != ActionStart {
		t.Fatalf("expected overnight window to start, got %+v", inside)
	}
	out := Evaluate(cfg, Signals{Now: time.Date(2026, 10, 17, 12, 0, 0, 0, time.Local)}, Runtime{MinerRunning: true})
	if out.Action != ActionStop {
		t.Fatalf("expected daytime to stop, got %+v", out)
	}
}

func TestScheduledRest(t *testing.T) {
	now := time.Now()
	cfg := Config{Mode: ModeFull, RestAfterMinutes: 60, RestDurationMinutes: 5}
	got := Evaluate(cfg, Signals{Now: now}, Runtime{MinerRunning: true, RunStarted: now.Add(-61 * time.Minute)})
	if got.Action != ActionStop || got.RestForSeconds != 300 {
		t.Fatalf("expected five-minute rest, got %+v", got)
	}
}

func TestGPUUtilizationBlocksOnlyStart(t *testing.T) {
	cfg := Config{Mode: ModeSmart, StartIdleSeconds: 1, PauseOnGPUUtilization: true, GPUUtilizationThreshold: 20}
	signals := Signals{Now: time.Now(), IdleFor: time.Hour, GPUMetricsKnown: true, MaxGPUUtilization: 40}
	if got := Evaluate(cfg, signals, Runtime{}); got.Action != ActionHold {
		t.Fatalf("expected busy GPU to block start, got %+v", got)
	}
	if got := Evaluate(cfg, signals, Runtime{MinerRunning: true}); got.Action != ActionHold || got.Reason != "smart mining continues" {
		t.Fatalf("expected miner load not to stop itself, got %+v", got)
	}
}

func TestApplyOverrideRejectsUnknownField(t *testing.T) {
	_, err := ApplyOverride(Config{Mode: ModeSmart}, map[string]string{"shell": "whoami"})
	if err == nil {
		t.Fatal("expected unknown policy field to be rejected")
	}
}

func TestFullscreenAllowlist(t *testing.T) {
	cfg := Config{Mode: ModeAvoid, PauseOnFullscreen: true, AllowedFullscreenProcesses: []string{"video.exe"}}
	got := Evaluate(cfg, Signals{Now: time.Now(), Foreground: "video.exe", Fullscreen: true}, Runtime{})
	if got.Action != ActionStart {
		t.Fatalf("expected allowlisted fullscreen app to permit mining, got %+v", got)
	}
}

func TestManualPauseWins(t *testing.T) {
	cfg := Config{Mode: ModeFull}
	got := Evaluate(cfg, Signals{Now: time.Now()}, Runtime{MinerRunning: true, ManualPaused: true})
	if got.Action != ActionStop {
		t.Fatalf("expected operator pause to stop miner, got %+v", got)
	}
}

func TestUnknownGPUProcessStateFailsSafe(t *testing.T) {
	cfg := Config{Mode: ModeSmart, PauseOnOtherGPUProcess: true}
	got := Evaluate(cfg, Signals{Now: time.Now(), IdleFor: time.Hour, GPUProcessKnown: false}, Runtime{MinerRunning: true})
	if got.Action != ActionStop {
		t.Fatalf("expected unknown GPU process state to stop miner, got %+v", got)
	}
}
