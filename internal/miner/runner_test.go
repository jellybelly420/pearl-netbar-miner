package miner

import (
	"slices"
	"testing"

	"github.com/jellybelly420/pearl-netbar-miner/internal/config"
)

func TestArgumentsIncludePerGPUProfiles(t *testing.T) {
	runner := NewRunner(config.Miner{}, config.Mining{
		Wallet: "prl1wallet", Worker: "seat-001", Pool: "pool:9000", Password: "x", APIPort: 4002,
		ResetOCOnStart: true, Watchdog: true,
		GPUProfiles: []config.GPUProfile{
			{Index: 2, PowerLimit: 180, CoreOffset: -100},
			{Index: 0, PowerLimit: 150, FanSpeed: 70},
		},
	})
	arguments := runner.arguments()
	for _, sequence := range [][]string{
		{"--gpu-list", "0,2"}, {"--gpu-powerlimit", "150,180"},
		{"--gpu-core-offset", "*,-100"}, {"--gpu-fan-speed", "70,*"},
	} {
		if !containsSequence(arguments, sequence) {
			t.Fatalf("arguments missing %v: %v", sequence, arguments)
		}
	}
	if !slices.Contains(arguments, "--gpu-reset-oc") {
		t.Fatalf("arguments missing reset: %v", arguments)
	}
	if !slices.Contains(arguments, "--watchdog") {
		t.Fatalf("arguments missing watchdog: %v", arguments)
	}
}

func containsSequence(values, sequence []string) bool {
	for index := 0; index+len(sequence) <= len(values); index++ {
		if slices.Equal(values[index:index+len(sequence)], sequence) {
			return true
		}
	}
	return false
}
