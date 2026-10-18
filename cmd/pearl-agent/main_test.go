package main

import (
	"testing"

	"github.com/jellybelly420/pearl-netbar-miner/internal/agent"
	"github.com/jellybelly420/pearl-netbar-miner/internal/config"
	"github.com/jellybelly420/pearl-netbar-miner/internal/control"
	"github.com/jellybelly420/pearl-netbar-miner/internal/policy"
)

func commandTestConfig() config.Config {
	return config.Config{
		Mining: config.Mining{
			Wallet: "prl1pazs85kcfv2skj03jlg949w8htzlcktnn0me22ka76d6xur9cgnjqg9gx62",
			Worker: "seat-001", Pool: "pool.pearlhash.xyz:9000", APIPort: 4002,
		},
		Policy: policy.Config{Mode: policy.ModeSmart},
	}
}

func TestApplyPolicyAndMiningCommands(t *testing.T) {
	cfg := commandTestConfig()
	state := agent.State{}
	if _, err := applyCommand(&cfg, &state, control.Command{Type: "set-policy", Payload: map[string]string{"start_idle_seconds": "600"}}); err != nil {
		t.Fatal(err)
	}
	if cfg.Policy.StartIdleSeconds != 600 || state.Policy == nil {
		t.Fatalf("policy was not persisted: %+v %+v", cfg.Policy, state)
	}
	effect, err := applyCommand(&cfg, &state, control.Command{Type: "set-mining", Payload: map[string]string{"power_limit": "175"}})
	if err != nil || !effect.MiningChanged || cfg.Mining.PowerLimit != 175 || state.Mining == nil {
		t.Fatalf("mining update failed: effect=%+v err=%v cfg=%+v", effect, err, cfg.Mining)
	}
}

func TestRemotePowerRequiresLocalOptIn(t *testing.T) {
	cfg := commandTestConfig()
	if _, err := applyCommand(&cfg, &agent.State{}, control.Command{Type: "reboot"}); err == nil {
		t.Fatal("expected remote reboot to require local opt-in")
	}
}
