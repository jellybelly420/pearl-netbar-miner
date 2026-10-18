package miner

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jellybelly420/pearl-netbar-miner/internal/config"
)

type Runner struct {
	mu      sync.Mutex
	command *exec.Cmd
	logFile *os.File
	miner   config.Miner
	mining  config.Mining
}

func NewRunner(miner config.Miner, mining config.Mining) *Runner {
	return &Runner{miner: miner, mining: mining}
}

func (r *Runner) Running() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.command != nil && r.command.Process != nil
}

func (r *Runner) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.command != nil && r.command.Process != nil {
		return nil
	}
	executable, err := Ensure(ctx, r.miner)
	if err != nil {
		return err
	}
	if err := os.MkdirAll("logs", 0o755); err != nil {
		return err
	}
	logPath := filepath.Join("logs", "wildrig-"+time.Now().Format("20060102-150405")+".log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	arguments := r.arguments()
	command := exec.Command(executable, arguments...)
	command.Dir = filepath.Dir(executable)
	command.Stdout = logFile
	command.Stderr = logFile
	configureProcess(command)
	if err := command.Start(); err != nil {
		logFile.Close()
		return err
	}
	r.command = command
	r.logFile = logFile
	go r.wait(command, logFile)
	return nil
}

func (r *Runner) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stopLocked()
}

func (r *Runner) Restart(ctx context.Context) error {
	if err := r.Stop(); err != nil {
		return err
	}
	return r.Start(ctx)
}

func (r *Runner) Update(minerConfig config.Miner, miningConfig config.Mining) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.stopLocked(); err != nil {
		return err
	}
	r.miner = minerConfig
	r.mining = miningConfig
	return nil
}

func (r *Runner) stopLocked() error {
	if r.command == nil || r.command.Process == nil {
		return nil
	}
	command := r.command
	err := terminateProcess(command)
	if errors.Is(err, os.ErrProcessDone) {
		err = nil
	}
	if err != nil {
		return err
	}
	r.command = nil
	if r.logFile != nil {
		_ = r.logFile.Close()
		r.logFile = nil
	}
	return nil
}

func (r *Runner) wait(command *exec.Cmd, logFile *os.File) {
	_ = command.Wait()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.command == command {
		r.command = nil
	}
	if r.logFile == logFile {
		_ = logFile.Close()
		r.logFile = nil
	}
}

func (r *Runner) arguments() []string {
	arguments := []string{
		"--algo", "pearlhash",
		"--url", r.mining.Pool,
		"--user", r.mining.Wallet,
		"--worker", r.mining.Worker,
		"--pass", r.mining.Password,
		"--api-port", strconv.Itoa(r.mining.APIPort),
		"--api-worker-id", r.mining.Worker,
		"--print-time", "10",
		"--no-color",
	}
	profiles := append([]config.GPUProfile(nil), r.mining.GPUProfiles...)
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].Index < profiles[j].Index })
	if len(profiles) > 0 {
		indexes := make([]string, 0, len(profiles))
		for _, profile := range profiles {
			indexes = append(indexes, strconv.Itoa(profile.Index))
		}
		arguments = append(arguments, "--gpu-list", strings.Join(indexes, ","))
	} else if r.mining.GPUList != "" {
		arguments = append(arguments, "--gpu-list", r.mining.GPUList)
	}
	if r.mining.ResetOCOnStart {
		arguments = append(arguments, "--gpu-reset-oc")
	}
	if r.mining.Watchdog {
		arguments = append(arguments, "--watchdog")
	}
	if values, ok := profileValues(profiles, func(profile config.GPUProfile) int { return profile.PowerLimit }); ok {
		arguments = append(arguments, "--gpu-powerlimit", values)
	} else if r.mining.PowerLimit > 0 {
		arguments = append(arguments, "--gpu-powerlimit", strconv.Itoa(r.mining.PowerLimit))
	}
	appendProfileArgument := func(flag string, getter func(config.GPUProfile) int) {
		if values, ok := profileValues(profiles, getter); ok {
			arguments = append(arguments, flag, values)
		}
	}
	appendProfileArgument("--gpu-core-clock", func(profile config.GPUProfile) int { return profile.CoreClock })
	appendProfileArgument("--gpu-core-offset", func(profile config.GPUProfile) int { return profile.CoreOffset })
	appendProfileArgument("--gpu-memory-clock", func(profile config.GPUProfile) int { return profile.MemoryClock })
	appendProfileArgument("--gpu-memory-offset", func(profile config.GPUProfile) int { return profile.MemoryOffset })
	appendProfileArgument("--gpu-fan-speed", func(profile config.GPUProfile) int { return profile.FanSpeed })
	if r.mining.TemperatureLimit > 0 {
		arguments = append(arguments, "--gpu-temp-limit", strconv.Itoa(r.mining.TemperatureLimit))
	}
	if r.mining.TemperatureResume > 0 {
		arguments = append(arguments, "--gpu-temp-resume", strconv.Itoa(r.mining.TemperatureResume))
	}
	return arguments
}

func profileValues(profiles []config.GPUProfile, getter func(config.GPUProfile) int) (string, bool) {
	if len(profiles) == 0 {
		return "", false
	}
	values := make([]string, len(profiles))
	configured := false
	for index, profile := range profiles {
		value := getter(profile)
		if value == 0 {
			values[index] = "*"
			continue
		}
		configured = true
		values[index] = strconv.Itoa(value)
	}
	return strings.Join(values, ","), configured
}
