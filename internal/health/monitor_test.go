package health

import (
	"testing"
	"time"

	"github.com/jellybelly420/pearl-netbar-miner/internal/config"
	"github.com/jellybelly420/pearl-netbar-miner/internal/miner"
)

func TestRestartsAfterZeroHashTimeout(t *testing.T) {
	monitor := New(config.Health{StartupGraceSeconds: 10, ZeroHashTimeoutSeconds: 30, RestartCooldownSeconds: 5, MaxRestarts: 2, RestartWindowSeconds: 300})
	now := time.Unix(1000, 0)
	if got := monitor.Observe(now, true, nil); got.Action != ActionNone {
		t.Fatalf("unexpected startup action: %+v", got)
	}
	if got := monitor.Observe(now.Add(31*time.Second), true, nil); got.Action != ActionRestart {
		t.Fatalf("expected restart, got %+v", got)
	}
}

func TestPausesWhenRestartBudgetIsExhausted(t *testing.T) {
	monitor := New(config.Health{ZeroHashTimeoutSeconds: 1, MaxRestarts: 1, RestartWindowSeconds: 300})
	now := time.Unix(2000, 0)
	_ = monitor.Observe(now, true, &miner.Statistics{})
	if got := monitor.Observe(now.Add(2*time.Second), true, &miner.Statistics{}); got.Action != ActionRestart {
		t.Fatalf("expected first recovery to restart, got %+v", got)
	}
	if got := monitor.Observe(now.Add(4*time.Second), true, &miner.Statistics{}); got.Action != ActionPause {
		t.Fatalf("expected exhausted budget to pause, got %+v", got)
	}
}

func TestAcceptedShareKeepsMonitorHealthy(t *testing.T) {
	monitor := New(config.Health{NoShareTimeoutSeconds: 30})
	now := time.Unix(3000, 0)
	if got := monitor.Observe(now, true, &miner.Statistics{Hashrate10s: 10, Accepted: 1}); got.Action != ActionNone {
		t.Fatalf("unexpected action: %+v", got)
	}
	if got := monitor.Observe(now.Add(20*time.Second), true, &miner.Statistics{Hashrate10s: 10, Accepted: 2}); got.Action != ActionNone {
		t.Fatalf("accepted share should refresh health: %+v", got)
	}
}
