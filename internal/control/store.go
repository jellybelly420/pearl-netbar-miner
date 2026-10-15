package control

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/jellybelly420/pearl-netbar-miner/internal/fsutil"
)

type Command struct {
	Sequence  uint64            `json:"sequence"`
	Type      string            `json:"type"`
	Payload   map[string]string `json:"payload,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	AckedAt   *time.Time        `json:"acked_at,omitempty"`
}

type Heartbeat struct {
	Time            time.Time `json:"time"`
	MinerRunning    bool      `json:"miner_running"`
	PolicyMode      string    `json:"policy_mode"`
	Decision        string    `json:"decision"`
	Reason          string    `json:"reason"`
	Foreground      string    `json:"foreground"`
	Fullscreen      bool      `json:"fullscreen"`
	IdleSeconds     int64     `json:"idle_seconds"`
	OtherGPUProcess bool      `json:"other_gpu_process"`
	GPUProcessKnown bool      `json:"gpu_process_sensor_available"`
	GPUCount        int       `json:"gpu_count"`
	Hashrate10s     float64   `json:"hashrate_10s"`
	AcceptedShares  int64     `json:"accepted_shares"`
	RejectedShares  int64     `json:"rejected_shares"`
	Pool            string    `json:"pool"`
	Version         string    `json:"version"`
}

type Agent struct {
	ID            string     `json:"id"`
	Secret        string     `json:"secret,omitempty"`
	EnrolledAt    time.Time  `json:"enrolled_at"`
	LastHeartbeat *Heartbeat `json:"last_heartbeat,omitempty"`
	NextSequence  uint64     `json:"next_sequence"`
	Commands      []Command  `json:"commands,omitempty"`
}

type state struct {
	Agents map[string]*Agent `json:"agents"`
}

type Store struct {
	mu    sync.RWMutex
	path  string
	state state
}

var agentIDPattern = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)

func OpenStore(path string) (*Store, error) {
	store := &Store{path: path, state: state{Agents: make(map[string]*Agent)}}
	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(contents, &store.state); err != nil {
		return nil, err
	}
	if store.state.Agents == nil {
		store.state.Agents = make(map[string]*Agent)
	}
	return store, nil
}

func (s *Store) Enroll(id string) (string, error) {
	if !agentIDPattern.MatchString(id) {
		return "", errors.New("agent id must contain 1-64 letters, digits, or hyphens")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.state.Agents[id]; exists {
		return "", fmt.Errorf("agent %s already exists", id)
	}
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		return "", err
	}
	secret := base64.RawURLEncoding.EncodeToString(secretBytes)
	s.state.Agents[id] = &Agent{ID: id, Secret: secret, EnrolledAt: time.Now().UTC(), NextSequence: 1}
	return secret, s.saveLocked()
}

func (s *Store) Secret(id string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	agent, ok := s.state.Agents[id]
	if !ok {
		return "", false
	}
	return agent.Secret, true
}

func (s *Store) Heartbeat(id string, heartbeat Heartbeat) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	agent, ok := s.state.Agents[id]
	if !ok {
		return os.ErrNotExist
	}
	heartbeat.Time = time.Now().UTC()
	agent.LastHeartbeat = &heartbeat
	return s.saveLocked()
}

func (s *Store) Queue(id, commandType string, payload map[string]string) (Command, error) {
	if !allowedCommand(commandType) {
		return Command{}, errors.New("unsupported command type")
	}
	if commandType == "set-mode" {
		mode := payload["mode"]
		if mode != "full" && mode != "avoid" && mode != "smart" && mode != "manual" {
			return Command{}, errors.New("invalid policy mode")
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	agent, ok := s.state.Agents[id]
	if !ok {
		return Command{}, os.ErrNotExist
	}
	command := Command{Sequence: agent.NextSequence, Type: commandType, Payload: payload, CreatedAt: time.Now().UTC()}
	agent.NextSequence++
	agent.Commands = append(agent.Commands, command)
	return command, s.saveLocked()
}

func (s *Store) Pending(id string, after uint64) ([]Command, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	agent, ok := s.state.Agents[id]
	if !ok {
		return nil, os.ErrNotExist
	}
	commands := make([]Command, 0)
	for _, command := range agent.Commands {
		if command.Sequence > after && command.AckedAt == nil {
			commands = append(commands, command)
		}
	}
	return commands, nil
}

func (s *Store) Ack(id string, sequence uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	agent, ok := s.state.Agents[id]
	if !ok {
		return os.ErrNotExist
	}
	for index := range agent.Commands {
		if agent.Commands[index].Sequence == sequence {
			now := time.Now().UTC()
			agent.Commands[index].AckedAt = &now
			return s.saveLocked()
		}
	}
	return os.ErrNotExist
}

func (s *Store) Agents() []Agent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Agent, 0, len(s.state.Agents))
	for _, stored := range s.state.Agents {
		copy := *stored
		copy.Secret = ""
		result = append(result, copy)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func (s *Store) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	contents, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(s.path), ".control-state-*")
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
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return fsutil.Replace(temporaryPath, s.path)
}

func allowedCommand(value string) bool {
	switch value {
	case "start", "stop", "resume", "set-mode":
		return true
	default:
		return false
	}
}
