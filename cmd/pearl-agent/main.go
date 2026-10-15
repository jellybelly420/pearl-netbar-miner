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
	"github.com/jellybelly420/pearl-netbar-miner/internal/miner"
	"github.com/jellybelly420/pearl-netbar-miner/internal/platform"
	"github.com/jellybelly420/pearl-netbar-miner/internal/policy"
)

const version = "0.1.0"

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
	if state.PolicyMode != "" {
		cfg.Policy.Mode = policy.Mode(state.PolicyMode)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	runner := miner.NewRunner(cfg.Miner, cfg.Mining)
	defer runner.Stop()
	sensor := platform.NewSensor()
	var client *agent.Client
	if cfg.ControlURL != "" {
		client = agent.NewClient(cfg.ControlURL, cfg.AgentID, cfg.AgentSecret)
	}
	lastHeartbeat := time.Time{}
	lastStopped := time.Time{}

	for {
		if client != nil {
			commands, commandErr := client.Commands(state.LastCommand)
			if commandErr != nil {
				log.Printf("control poll: %v", commandErr)
			} else {
				for _, command := range commands {
					applyCommand(&cfg, &state, command)
					if err := agent.SaveState(cfg.StatePath, state); err != nil {
						log.Printf("save state: %v", err)
						break
					}
					if err := client.Ack(command.Sequence); err != nil {
						log.Printf("ack command %d: %v", command.Sequence, err)
						break
					}
					state.LastCommand = command.Sequence
					if err := agent.SaveState(cfg.StatePath, state); err != nil {
						log.Printf("save acknowledged command state: %v", err)
					}
				}
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
			if state.ForceRun && !state.ManualPaused {
				decision = policy.Decision{Action: policy.ActionStart, Reason: "operator force start"}
			} else {
				decision = policy.Evaluate(cfg.Policy, policy.Signals{
					Now: snapshot.CapturedAt, IdleFor: time.Duration(snapshot.IdleSeconds) * time.Second,
					Foreground: snapshot.Foreground, Fullscreen: snapshot.Fullscreen,
					OtherGPUProcess: snapshot.OtherGPUProcess, GPUProcessKnown: snapshot.GPUProcessKnown,
				}, policy.Runtime{MinerRunning: running, ManualPaused: state.ManualPaused, LastStopped: lastStopped})
			}
		}

		if !*dryRun {
			switch decision.Action {
			case policy.ActionStart:
				if err := runner.Start(ctx); err != nil {
					log.Printf("start miner: %v", err)
				}
			case policy.ActionStop:
				if err := runner.Stop(); err != nil {
					log.Printf("stop miner: %v", err)
				}
				lastStopped = time.Now().UTC()
			}
		}

		status := control.Heartbeat{
			MinerRunning: runner.Running(), PolicyMode: string(cfg.Policy.Mode), Decision: string(decision.Action), Reason: decision.Reason,
			Foreground: snapshot.Foreground, Fullscreen: snapshot.Fullscreen, IdleSeconds: snapshot.IdleSeconds,
			OtherGPUProcess: snapshot.OtherGPUProcess, GPUProcessKnown: snapshot.GPUProcessKnown,
			GPUCount: len(snapshot.GPUs), Version: version,
		}
		if runner.Running() {
			statisticsContext, statisticsCancel := context.WithTimeout(ctx, 2*time.Second)
			statistics, statisticsErr := miner.FetchStatistics(statisticsContext, cfg.Mining.APIPort)
			statisticsCancel()
			if statisticsErr == nil {
				status.Hashrate10s = statistics.Hashrate10s
				status.AcceptedShares = statistics.Accepted
				status.RejectedShares = statistics.Rejected
				status.Pool = statistics.Pool
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

func applyCommand(cfg *config.Config, state *agent.State, command control.Command) {
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
			cfg.Policy.Mode = mode
			state.PolicyMode = string(mode)
			state.ForceRun = false
		}
	}
}
