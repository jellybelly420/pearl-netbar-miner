package miner

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	if r.command == nil || r.command.Process == nil {
		return nil
	}
	err := terminateProcess(r.command)
	if errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	return err
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
	if r.mining.GPUList != "" {
		arguments = append(arguments, "--gpu-list", r.mining.GPUList)
	}
	if r.mining.PowerLimit > 0 {
		arguments = append(arguments, "--gpu-powerlimit", strconv.Itoa(r.mining.PowerLimit))
	}
	if r.mining.TemperatureLimit > 0 {
		arguments = append(arguments, "--gpu-temp-limit", strconv.Itoa(r.mining.TemperatureLimit))
	}
	if r.mining.TemperatureResume > 0 {
		arguments = append(arguments, "--gpu-temp-resume", strconv.Itoa(r.mining.TemperatureResume))
	}
	return arguments
}
