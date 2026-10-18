package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/jellybelly420/pearl-netbar-miner/internal/agent"
	"github.com/jellybelly420/pearl-netbar-miner/internal/config"
	"github.com/jellybelly420/pearl-netbar-miner/internal/control"
	"github.com/jellybelly420/pearl-netbar-miner/internal/health"
	"github.com/jellybelly420/pearl-netbar-miner/internal/miner"
	"github.com/jellybelly420/pearl-netbar-miner/internal/platform"
	"github.com/jellybelly420/pearl-netbar-miner/internal/policy"
	updatepkg "github.com/jellybelly420/pearl-netbar-miner/internal/update"
)

const version = "0.2.0"

type commandEffect struct {
	MiningChanged bool
	PowerAction   platform.PowerAction
}

func main() {
	configPath := flag.String("config", "config.json", "agent configuration")
	dryRun := flag.Bool("dry-run", false, "evaluate policy without downloading or starting a miner")
	once := flag.Bool("once", false, "run one policy cycle")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	if runtime.GOOS != "windows" && !*dryRun {
		log.Fatal("mining agent execution is supported only on Windows; use -dry-run for configuration checks")
	}
	state, err := agent.LoadState(cfg.StatePath)
	if err != nil {
		log.Fatal(err)
	}
	if state.Policy != nil {
		cfg.Policy = *state.Policy
	} else if state.PolicyMode != "" {
		cfg.Policy.Mode = policy.Mode(state.PolicyMode)
	}
	if state.Mining != nil {
		cfg.Mining = *state.Mining
	}
	if state.Miner != nil {
		cfg.Miner = *state.Miner
	}
	if err := cfg.Validate(); err != nil {
		log.Fatalf("persisted configuration is invalid: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	runner := miner.NewRunner(cfg.Miner, cfg.Mining)
	defer runner.Stop()
	healthMonitor := health.New(cfg.Health)
	sensor := platform.NewSensor()
	var client *agent.Client
	if cfg.ControlURL != "" {
		client = agent.NewClient(cfg.ControlURL, cfg.AgentID, cfg.AgentSecret)
	}
	lastHeartbeat := time.Time{}
	lastUpdateCheck := time.Time{}

	for {
		if client != nil {
			commands, commandErr := client.Commands(state.LastCommand)
			if commandErr != nil {
				log.Printf("control poll: %v", commandErr)
			} else {
				for _, command := range commands {
					previousConfig, previousState := cfg, state
					effect, applyErr := applyCommand(&cfg, &state, command)
					if applyErr == nil && effect.MiningChanged {
						applyErr = runner.Update(cfg.Miner, cfg.Mining)
					}
					if applyErr == nil {
						applyErr = agent.SaveState(cfg.StatePath, state)
					}
					acknowledgement := control.CommandAck{Result: "ok"}
					if applyErr != nil {
						cfg, state = previousConfig, previousState
						_ = runner.Update(cfg.Miner, cfg.Mining)
						acknowledgement = control.CommandAck{Result: "error", Message: applyErr.Error()}
					}
					if err := client.Ack(command.Sequence, acknowledgement); err != nil {
						log.Printf("ack command %d: %v", command.Sequence, err)
						break
					}
					state.LastCommand = command.Sequence
					if err := agent.SaveState(cfg.StatePath, state); err != nil {
						log.Printf("save acknowledged command state: %v", err)
					}
					if applyErr == nil && effect.PowerAction != "" {
						_ = runner.Stop()
						if err := platform.RequestPowerAction(effect.PowerAction); err != nil {
							log.Printf("power action %s: %v", effect.PowerAction, err)
						}
					}
				}
			}
		}
		if cfg.Update.Enabled && updatepkg.Eligible(cfg.AgentID, cfg.Update.CanaryPercent) &&
			(lastUpdateCheck.IsZero() || time.Since(lastUpdateCheck) >= time.Duration(cfg.Update.CheckIntervalSeconds)*time.Second) {
			lastUpdateCheck = time.Now()
			updateContext, updateCancel := context.WithTimeout(ctx, 12*time.Minute)
			updated, updateErr := applySignedMinerUpdate(updateContext, &cfg, &state, runner)
			updateCancel()
			if updateErr != nil {
				log.Printf("signed miner update: %v", updateErr)
			} else if updated {
				log.Printf("signed miner update applied: %s", cfg.Miner.Version)
			}
		}

		snapshot, senseErr := sensor.Snapshot(ctx, []string{cfg.Miner.Executable})
		if senseErr != nil {
			if !*dryRun {
				log.Printf("sensing failed; fail-safe stop: %v", senseErr)
				_ = runner.Stop()
			}
			snapshot = platform.Snapshot{CapturedAt: time.Now().UTC(), SensingAvailable: false}
		}
		running := runner.Running()
		decision := policy.Decision{Action: policy.ActionHold, Reason: "sensing unavailable"}
		if snapshot.SensingAvailable || *dryRun {
			if state.HealthPauseUntil.After(snapshot.CapturedAt) {
				decision = policy.Decision{Action: policy.ActionStop, Reason: "health restart budget cooldown"}
			} else if state.ForceRun && !state.ManualPaused {
				decision = policy.Decision{Action: policy.ActionStart, Reason: "operator force start"}
			} else {
				maxUtilization := 0
				for _, gpu := range snapshot.GPUs {
					if gpu.Utilization > maxUtilization {
						maxUtilization = gpu.Utilization
					}
				}
				decision = policy.Evaluate(cfg.Policy, policy.Signals{
					Now: snapshot.CapturedAt, IdleFor: time.Duration(snapshot.IdleSeconds) * time.Second,
					Foreground: snapshot.Foreground, Fullscreen: snapshot.Fullscreen,
					RunningProcesses: snapshot.RunningProcesses, ProcessScanKnown: snapshot.ProcessScanKnown,
					OtherGPUProcess: snapshot.OtherGPUProcess, GPUProcessKnown: snapshot.GPUProcessKnown,
					MaxGPUUtilization: maxUtilization, GPUMetricsKnown: snapshot.GPUMetricsKnown,
				}, policy.Runtime{
					MinerRunning: running, ManualPaused: state.ManualPaused, LastStopped: state.LastStoppedAt,
					RunStarted: state.RunStartedAt, RestUntil: state.RestUntil,
				})
			}
		}

		if !*dryRun {
			stateChanged := false
			switch decision.Action {
			case policy.ActionStart:
				wasRunning := runner.Running()
				if err := runner.Start(ctx); err != nil {
					log.Printf("start miner: %v", err)
				} else if !wasRunning && runner.Running() {
					state.RunStartedAt = time.Now().UTC()
					state.RestUntil = time.Time{}
					stateChanged = true
				}
			case policy.ActionStop:
				wasRunning := runner.Running()
				if err := runner.Stop(); err != nil {
					log.Printf("stop miner: %v", err)
				} else if wasRunning {
					state.LastStoppedAt = time.Now().UTC()
					state.RunStartedAt = time.Time{}
					stateChanged = true
				}
				if decision.RestForSeconds > 0 {
					state.RestUntil = time.Now().UTC().Add(time.Duration(decision.RestForSeconds) * time.Second)
					stateChanged = true
				}
			}
			if stateChanged {
				if err := agent.SaveState(cfg.StatePath, state); err != nil {
					log.Printf("save runtime state: %v", err)
				}
			}
		}

		gpuStatuses := make([]control.GPUStatus, 0, len(snapshot.GPUs))
		for _, gpu := range snapshot.GPUs {
			gpuStatuses = append(gpuStatuses, control.GPUStatus{
				Index: gpu.Index, Name: gpu.Name, Utilization: gpu.Utilization,
				Temperature: gpu.Temperature, PowerWatts: gpu.PowerWatts,
			})
		}
		status := control.Heartbeat{
			MinerRunning: runner.Running(), PolicyMode: string(cfg.Policy.Mode), Decision: string(decision.Action), Reason: decision.Reason,
			Foreground: snapshot.Foreground, Fullscreen: snapshot.Fullscreen, IdleSeconds: snapshot.IdleSeconds,
			ProcessScanKnown: snapshot.ProcessScanKnown,
			OtherGPUProcess:  snapshot.OtherGPUProcess, GPUProcessKnown: snapshot.GPUProcessKnown,
			GPUCount: len(snapshot.GPUs), GPUMetricsKnown: snapshot.GPUMetricsKnown, GPUs: gpuStatuses, Version: version,
			Health: "ok",
		}
		var statistics *miner.Statistics
		if runner.Running() {
			statisticsContext, statisticsCancel := context.WithTimeout(ctx, 2*time.Second)
			fetched, statisticsErr := miner.FetchStatistics(statisticsContext, cfg.Mining.APIPort)
			statisticsCancel()
			if statisticsErr == nil {
				statistics = &fetched
				status.Hashrate10s = fetched.Hashrate10s
				status.AcceptedShares = fetched.Accepted
				status.RejectedShares = fetched.Rejected
				status.Pool = fetched.Pool
			}
		}
		if !*dryRun {
			healthResult := healthMonitor.Observe(time.Now().UTC(), runner.Running(), statistics)
			if healthResult.Action == health.ActionNone {
				status.Health = "ok"
			} else {
				status.Health = string(healthResult.Action)
			}
			if healthResult.Reason != "" {
				status.Health += ": " + healthResult.Reason
			}
			switch healthResult.Action {
			case health.ActionRestart:
				if err := runner.Restart(ctx); err != nil {
					log.Printf("health restart: %v", err)
				} else {
					state.RunStartedAt = time.Now().UTC()
					_ = agent.SaveState(cfg.StatePath, state)
				}
			case health.ActionPause:
				if rolledBack, err := rollbackMinerUpdate(ctx, &cfg, &state, runner); err != nil {
					log.Printf("miner rollback: %v", err)
				} else if rolledBack {
					healthMonitor = health.New(cfg.Health)
					state.RunStartedAt = time.Now().UTC()
					_ = agent.SaveState(cfg.StatePath, state)
				} else {
					_ = runner.Stop()
					state.HealthPauseUntil = healthResult.PauseUntil
					state.RunStartedAt = time.Time{}
					_ = agent.SaveState(cfg.StatePath, state)
				}
			case health.ActionReboot:
				if rolledBack, err := rollbackMinerUpdate(ctx, &cfg, &state, runner); err != nil {
					log.Printf("miner rollback: %v", err)
				} else if rolledBack {
					healthMonitor = health.New(cfg.Health)
				} else if cfg.AllowRemotePowerActions {
					_ = runner.Stop()
					if err := platform.RequestPowerAction(platform.PowerReboot); err != nil {
						log.Printf("health reboot: %v", err)
					}
				} else {
					state.HealthPauseUntil = time.Now().UTC().Add(time.Duration(cfg.Health.RestartWindowSeconds) * time.Second)
					_ = agent.SaveState(cfg.StatePath, state)
				}
			}
			if healthResult.Action == health.ActionNone && state.PreviousMiner != nil &&
				time.Since(state.MinerUpdatedAt) >= 6*time.Hour {
				state.PreviousMiner = nil
				_ = agent.SaveState(cfg.StatePath, state)
			}
		}
		if *dryRun {
			encoded, _ := json.MarshalIndent(map[string]any{"snapshot": snapshot, "decision": decision, "config_valid": true}, "", "  ")
			fmt.Println(string(encoded))
		}
		if client != nil && (lastHeartbeat.IsZero() || time.Since(lastHeartbeat) >= time.Duration(cfg.HeartbeatSeconds)*time.Second) {
			if err := client.Heartbeat(status); err != nil {
				log.Printf("heartbeat: %v", err)
			} else {
				lastHeartbeat = time.Now()
			}
		}
		if *once {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(cfg.PollSeconds) * time.Second):
		}
	}
}

func applySignedMinerUpdate(ctx context.Context, cfg *config.Config, state *agent.State, runner *miner.Runner) (bool, error) {
	manifest, err := updatepkg.Fetch(ctx, cfg.Update.ManifestURL, cfg.Update.PublicKey)
	if err != nil {
		return false, err
	}
	if manifest.Channel != cfg.Update.Channel {
		return false, fmt.Errorf("manifest channel %q does not match %q", manifest.Channel, cfg.Update.Channel)
	}
	if manifest.GeneratedAt.After(time.Now().UTC().Add(10 * time.Minute)) {
		return false, fmt.Errorf("manifest generated_at is unexpectedly in the future")
	}
	if !state.ManifestGeneratedAt.IsZero() && !manifest.GeneratedAt.After(state.ManifestGeneratedAt) {
		return false, nil
	}
	if manifest.Miner.Version == cfg.Miner.Version && manifest.Miner.SHA256 == cfg.Miner.SHA256 &&
		manifest.Miner.ExecutableSHA256 == cfg.Miner.ExecutableSHA256 {
		state.ManifestGeneratedAt = manifest.GeneratedAt
		return false, agent.SaveState(cfg.StatePath, *state)
	}
	if _, err := miner.Ensure(ctx, manifest.Miner); err != nil {
		return false, err
	}
	previousConfig, previousState := *cfg, *state
	previousMiner := cfg.Miner
	if err := runner.Update(manifest.Miner, cfg.Mining); err != nil {
		return false, err
	}
	cfg.Miner = manifest.Miner
	state.PreviousMiner = &previousMiner
	state.Miner = &manifest.Miner
	state.MinerUpdatedAt = time.Now().UTC()
	state.ManifestGeneratedAt = manifest.GeneratedAt
	if err := agent.SaveState(cfg.StatePath, *state); err != nil {
		*cfg, *state = previousConfig, previousState
		_ = runner.Update(cfg.Miner, cfg.Mining)
		return false, err
	}
	return true, nil
}

func rollbackMinerUpdate(ctx context.Context, cfg *config.Config, state *agent.State, runner *miner.Runner) (bool, error) {
	if state.PreviousMiner == nil {
		return false, nil
	}
	previous := *state.PreviousMiner
	if _, err := miner.Ensure(ctx, previous); err != nil {
		return false, err
	}
	currentMiner, previousState := cfg.Miner, *state
	if err := runner.Update(previous, cfg.Mining); err != nil {
		return false, err
	}
	cfg.Miner = previous
	state.Miner = &previous
	state.PreviousMiner = nil
	state.MinerUpdatedAt = time.Time{}
	state.HealthPauseUntil = time.Time{}
	if err := agent.SaveState(cfg.StatePath, *state); err != nil {
		cfg.Miner = currentMiner
		*state = previousState
		_ = runner.Update(currentMiner, cfg.Mining)
		return false, err
	}
	if err := runner.Start(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func applyCommand(cfg *config.Config, state *agent.State, command control.Command) (commandEffect, error) {
	switch command.Type {
	case "start":
		state.ManualPaused = false
		state.ForceRun = true
	case "stop":
		state.ManualPaused = true
		state.ForceRun = false
	case "resume":
		state.ManualPaused = false
		state.ForceRun = false
	case "set-mode":
		mode := policy.Mode(command.Payload["mode"])
		candidate := cfg.Policy
		candidate.Mode = mode
		if candidate.Valid() {
			cfg.Policy = candidate
			state.PolicyMode = string(mode)
			state.Policy = &candidate
			state.ForceRun = false
			return commandEffect{}, nil
		}
		return commandEffect{}, fmt.Errorf("invalid policy mode %q", mode)
	case "set-policy":
		candidate, err := policy.ApplyOverride(cfg.Policy, command.Payload)
		if err != nil {
			return commandEffect{}, err
		}
		cfg.Policy = candidate
		state.PolicyMode = string(candidate.Mode)
		state.Policy = &candidate
		state.ForceRun = false
	case "set-mining":
		candidate, err := config.ApplyMiningOverride(cfg.Mining, command.Payload)
		if err != nil {
			return commandEffect{}, err
		}
		cfg.Mining = candidate
		state.Mining = &candidate
		return commandEffect{MiningChanged: true}, nil
	case "set-gpu-profile":
		candidate, err := config.ApplyGPUProfile(cfg.Mining, command.Payload)
		if err != nil {
			return commandEffect{}, err
		}
		cfg.Mining = candidate
		state.Mining = &candidate
		return commandEffect{MiningChanged: true}, nil
	case "reset-gpu-profiles":
		candidate := cfg.Mining
		candidate.GPUProfiles = nil
		candidate.ResetOCOnStart = true
		cfg.Mining = candidate
		state.Mining = &candidate
		return commandEffect{MiningChanged: true}, nil
	case "reboot":
		if !cfg.AllowRemotePowerActions {
			return commandEffect{}, fmt.Errorf("remote power actions are disabled on this agent")
		}
		return commandEffect{PowerAction: platform.PowerReboot}, nil
	case "shutdown":
		if !cfg.AllowRemotePowerActions {
			return commandEffect{}, fmt.Errorf("remote power actions are disabled on this agent")
		}
		return commandEffect{PowerAction: platform.PowerShutdown}, nil
	default:
		return commandEffect{}, fmt.Errorf("unsupported command type %q", command.Type)
	}
	return commandEffect{}, nil
}
