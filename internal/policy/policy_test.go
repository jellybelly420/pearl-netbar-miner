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
	got := Evaluate(cfg, Signals{Now: time.Now(), Foreground: `C:\Games\cs2.exe`}, Runtime{MinerRunning: true})
	if got.Action != ActionStop {
		t.Fatalf("expected blocked process to stop miner, got %+v", got)
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
