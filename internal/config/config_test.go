package config

import (
	"testing"

	"github.com/jellybelly420/pearl-netbar-miner/internal/policy"
)

func validConfig() Config {
	return Config{
		AgentID: "seat-001", AgentSecret: "12345678901234567890123456789012", ControlURL: "https://control.example.com",
		PollSeconds: 10, HeartbeatSeconds: 30, StatePath: "state.json",
		Miner:  Miner{Version: "1", URL: "https://example.com/miner.zip", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Executable: "wildrig.exe", ExecutableSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", InstallDir: "miners/wildrig"},
		Mining: Mining{Wallet: "prl1pazs85kcfv2skj03jlg949w8htzlcktnn0me22ka76d6xur9cgnjqg9gx62", Worker: "seat-001", Pool: "pool.example.com:9000", APIPort: 4002},
		Policy: policy.Config{Mode: policy.ModeSmart},
	}
}

func TestValidate(t *testing.T) {
	if err := validConfig().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestRejectsPlainRemoteControl(t *testing.T) {
	cfg := validConfig()
	cfg.ControlURL = "http://control.example.com"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected insecure remote control URL to fail")
	}
}

func TestRejectsPlaceholderWallet(t *testing.T) {
	cfg := validConfig()
	cfg.Mining.Wallet = "prl1-replace-me"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected placeholder wallet to fail")
	}
}

func TestRejectsGPUListAndProfilesTogether(t *testing.T) {
	cfg := validConfig()
	cfg.Mining.GPUList = "0"
	cfg.Mining.GPUProfiles = []GPUProfile{{Index: 0, PowerLimit: 150}}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected conflicting GPU selection to fail")
	}
}

func TestApplyMiningOverrideIsAllowlisted(t *testing.T) {
	base := validConfig().Mining
	updated, err := ApplyMiningOverride(base, map[string]string{"pool": "pool.pearlhash.xyz:9000", "power_limit": "175"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Pool != "pool.pearlhash.xyz:9000" || updated.PowerLimit != 175 {
		t.Fatalf("unexpected update: %+v", updated)
	}
	if _, err := ApplyMiningOverride(base, map[string]string{"executable": "cmd.exe"}); err == nil {
		t.Fatal("expected arbitrary mining field to be rejected")
	}
}

func TestGPUProfileOverrideDoesNotMutateBase(t *testing.T) {
	base := validConfig().Mining
	base.GPUProfiles = []GPUProfile{{Index: 0, PowerLimit: 150}}
	updated, err := ApplyGPUProfile(base, map[string]string{"index": "0", "power_limit": "175"})
	if err != nil {
		t.Fatal(err)
	}
	if base.GPUProfiles[0].PowerLimit != 150 || updated.GPUProfiles[0].PowerLimit != 175 {
		t.Fatalf("profile update mutated its input: base=%+v updated=%+v", base.GPUProfiles, updated.GPUProfiles)
	}
}
