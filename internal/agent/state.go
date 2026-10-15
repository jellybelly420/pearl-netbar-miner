package agent

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/jellybelly420/pearl-netbar-miner/internal/fsutil"
)

type State struct {
	ManualPaused bool   `json:"manual_paused"`
	ForceRun     bool   `json:"force_run"`
	PolicyMode   string `json:"policy_mode,omitempty"`
	LastCommand  uint64 `json:"last_command"`
}

func LoadState(path string) (State, error) {
	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return State{}, nil
	}
	if err != nil {
		return State{}, err
	}
	var state State
	if err := json.Unmarshal(contents, &state); err != nil {
		return State{}, err
	}
	return state, nil
}

func SaveState(path string, state State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	contents, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".agent-state-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(contents); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return fsutil.Replace(temporaryPath, path)
}
