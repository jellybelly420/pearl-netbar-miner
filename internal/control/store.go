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

	"github.com/jellybelly420/pearl-netbar-miner/internal/config"
	"github.com/jellybelly420/pearl-netbar-miner/internal/fsutil"
	"github.com/jellybelly420/pearl-netbar-miner/internal/policy"
)

type Command struct {
	Sequence  uint64            `json:"sequence"`
	Type      string            `json:"type"`
	Payload   map[string]string `json:"payload,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	ExpiresAt time.Time         `json:"expires_at"`
	AckedAt   *time.Time        `json:"acked_at,omitempty"`
	Result    string            `json:"result,omitempty"`
	Message   string            `json:"message,omitempty"`
}

type CommandAck struct {
	Result  string `json:"result"`
	Message string `json:"message,omitempty"`
}

type GPUStatus struct {
	Index       int     `json:"index"`
	Name        string  `json:"name"`
	Utilization int     `json:"utilization_percent"`
	Temperature int     `json:"temperature_c"`
	PowerWatts  float64 `json:"power_watts"`
}

type Heartbeat struct {
	Time             time.Time   `json:"time"`
	MinerRunning     bool        `json:"miner_running"`
	PolicyMode       string      `json:"policy_mode"`
	Decision         string      `json:"decision"`
	Reason           string      `json:"reason"`
	Foreground       string      `json:"foreground"`
	Fullscreen       bool        `json:"fullscreen"`
	IdleSeconds      int64       `json:"idle_seconds"`
	ProcessScanKnown bool        `json:"process_scan_available"`
	OtherGPUProcess  bool        `json:"other_gpu_process"`
	GPUProcessKnown  bool        `json:"gpu_process_sensor_available"`
	GPUCount         int         `json:"gpu_count"`
	GPUMetricsKnown  bool        `json:"gpu_metrics_available"`
	GPUs             []GPUStatus `json:"gpus,omitempty"`
	Hashrate10s      float64     `json:"hashrate_10s"`
	AcceptedShares   int64       `json:"accepted_shares"`
	RejectedShares   int64       `json:"rejected_shares"`
	Pool             string      `json:"pool"`
	Version          string      `json:"version"`
	Health           string      `json:"health"`
}

type Agent struct {
	ID               string      `json:"id"`
	Secret           string      `json:"secret,omitempty"`
	EnrolledAt       time.Time   `json:"enrolled_at"`
	LastHeartbeat    *Heartbeat  `json:"last_heartbeat,omitempty"`
	NextSequence     uint64      `json:"next_sequence"`
	Commands         []Command   `json:"commands,omitempty"`
	HeartbeatHistory []Heartbeat `json:"heartbeat_history,omitempty"`
}

type AuditEvent struct {
	Time     time.Time         `json:"time"`
	Action   string            `json:"action"`
	AgentID  string            `json:"agent_id,omitempty"`
	Sequence uint64            `json:"sequence,omitempty"`
	Details  map[string]string `json:"details,omitempty"`
}

type state struct {
	Agents map[string]*Agent `json:"agents"`
	Audit  []AuditEvent      `json:"audit,omitempty"`
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
	auditLength := len(s.state.Audit)
	s.state.Agents[id] = &Agent{ID: id, Secret: secret, EnrolledAt: time.Now().UTC(), NextSequence: 1}
	s.recordAuditLocked(AuditEvent{Action: "agent.enrolled", AgentID: id})
	if err := s.saveLocked(); err != nil {
		delete(s.state.Agents, id)
		s.state.Audit = s.state.Audit[:auditLength]
		return "", err
	}
	return secret, nil
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
	previous := cloneAgent(agent)
	heartbeat.Time = time.Now().UTC()
	agent.LastHeartbeat = &heartbeat
	agent.HeartbeatHistory = append(agent.HeartbeatHistory, heartbeat)
	if len(agent.HeartbeatHistory) > 240 {
		agent.HeartbeatHistory = append([]Heartbeat(nil), agent.HeartbeatHistory[len(agent.HeartbeatHistory)-240:]...)
	}
	if err := s.saveLocked(); err != nil {
		s.state.Agents[id] = previous
		return err
	}
	return nil
}

func (s *Store) Queue(id, commandType string, payload map[string]string) (Command, error) {
	if err := validateCommand(commandType, payload); err != nil {
		return Command{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	agent, ok := s.state.Agents[id]
	if !ok {
		return Command{}, os.ErrNotExist
	}
	previous := cloneAgent(agent)
	auditLength := len(s.state.Audit)
	now := time.Now().UTC()
	command := Command{Sequence: agent.NextSequence, Type: commandType, Payload: payload, CreatedAt: now, ExpiresAt: now.Add(commandTTL(commandType))}
	agent.NextSequence++
	agent.Commands = append(agent.Commands, command)
	pruneCommands(agent)
	s.recordAuditLocked(AuditEvent{Action: "command.queued", AgentID: id, Sequence: command.Sequence, Details: map[string]string{"type": commandType}})
	if err := s.saveLocked(); err != nil {
		s.state.Agents[id] = previous
		s.state.Audit = s.state.Audit[:auditLength]
		return Command{}, err
	}
	return command, nil
}

func (s *Store) QueueBatch(ids []string, commandType string, payload map[string]string) ([]Command, error) {
	if len(ids) == 0 || len(ids) > 500 {
		return nil, errors.New("batch must contain 1-500 agent ids")
	}
	if err := validateCommand(commandType, payload); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	previousAgents := make(map[string]*Agent, len(ids))
	auditLength := len(s.state.Audit)
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if _, duplicate := seen[id]; duplicate {
			return nil, fmt.Errorf("agent %s appears more than once", id)
		}
		seen[id] = struct{}{}
		if _, exists := s.state.Agents[id]; !exists {
			return nil, fmt.Errorf("agent %s: %w", id, os.ErrNotExist)
		}
		previousAgents[id] = cloneAgent(s.state.Agents[id])
	}
	commands := make([]Command, 0, len(ids))
	for _, id := range ids {
		agent := s.state.Agents[id]
		now := time.Now().UTC()
		command := Command{Sequence: agent.NextSequence, Type: commandType, Payload: payload, CreatedAt: now, ExpiresAt: now.Add(commandTTL(commandType))}
		agent.NextSequence++
		agent.Commands = append(agent.Commands, command)
		pruneCommands(agent)
		s.recordAuditLocked(AuditEvent{Action: "command.queued", AgentID: id, Sequence: command.Sequence, Details: map[string]string{"type": commandType, "batch": "true"}})
		commands = append(commands, command)
	}
	if err := s.saveLocked(); err != nil {
		for id, previous := range previousAgents {
			s.state.Agents[id] = previous
		}
		s.state.Audit = s.state.Audit[:auditLength]
		return nil, err
	}
	return commands, nil
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
		if command.Sequence > after && command.AckedAt == nil && (command.ExpiresAt.IsZero() || command.ExpiresAt.After(time.Now().UTC())) {
			commands = append(commands, command)
		}
	}
	return commands, nil
}

func (s *Store) Ack(id string, sequence uint64, acknowledgement CommandAck) error {
	if acknowledgement.Result != "ok" && acknowledgement.Result != "error" {
		return errors.New("command result must be ok or error")
	}
	if len(acknowledgement.Message) > 512 {
		return errors.New("command result message is too long")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	agent, ok := s.state.Agents[id]
	if !ok {
		return os.ErrNotExist
	}
	previous := cloneAgent(agent)
	auditLength := len(s.state.Audit)
	for index := range agent.Commands {
		if agent.Commands[index].Sequence == sequence {
			now := time.Now().UTC()
			agent.Commands[index].AckedAt = &now
			agent.Commands[index].Result = acknowledgement.Result
			agent.Commands[index].Message = acknowledgement.Message
			s.recordAuditLocked(AuditEvent{Action: "command.acknowledged", AgentID: id, Sequence: sequence, Details: map[string]string{"result": acknowledgement.Result}})
			if err := s.saveLocked(); err != nil {
				s.state.Agents[id] = previous
				s.state.Audit = s.state.Audit[:auditLength]
				return err
			}
			return nil
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
		copy.HeartbeatHistory = nil
		result = append(result, copy)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func (s *Store) History(id string) ([]Heartbeat, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	agent, ok := s.state.Agents[id]
	if !ok {
		return nil, os.ErrNotExist
	}
	return append([]Heartbeat(nil), agent.HeartbeatHistory...), nil
}

func (s *Store) Audit(limit int) []AuditEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	start := len(s.state.Audit) - limit
	if start < 0 {
		start = 0
	}
	return append([]AuditEvent(nil), s.state.Audit[start:]...)
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
	case "start", "stop", "resume", "set-mode", "set-policy", "set-mining", "set-gpu-profile", "reset-gpu-profiles", "reboot", "shutdown":
		return true
	default:
		return false
	}
}

func validateCommand(commandType string, payload map[string]string) error {
	if !allowedCommand(commandType) {
		return errors.New("unsupported command type")
	}
	if commandType == "set-mode" {
		mode := payload["mode"]
		if len(payload) != 1 || (mode != "full" && mode != "avoid" && mode != "smart" && mode != "manual") {
			return errors.New("invalid policy mode")
		}
	}
	if commandType == "set-policy" {
		base := policy.Config{Mode: policy.ModeSmart, ScheduleStart: "00:00", ScheduleStop: "00:00"}
		if _, err := policy.ApplyOverride(base, payload); err != nil {
			return err
		}
	}
	if commandType == "set-mining" {
		base := config.Mining{
			Wallet: "prl1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq", Worker: "validation",
			Pool: "pool.pearlhash.xyz:9000", APIPort: 4002,
		}
		if _, err := config.ApplyMiningOverride(base, payload); err != nil {
			return err
		}
	}
	if commandType == "set-gpu-profile" {
		base := config.Mining{
			Wallet: "prl1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq", Worker: "validation",
			Pool: "pool.pearlhash.xyz:9000", APIPort: 4002,
		}
		if _, err := config.ApplyGPUProfile(base, payload); err != nil {
			return err
		}
	}
	if commandType == "reset-gpu-profiles" && len(payload) != 0 {
		return errors.New("reset-gpu-profiles does not accept payload fields")
	}
	if (commandType == "reboot" || commandType == "shutdown") && len(payload) != 0 {
		return errors.New("power commands do not accept payload fields")
	}
	if (commandType == "start" || commandType == "stop" || commandType == "resume") && len(payload) != 0 {
		return errors.New("lifecycle commands do not accept payload fields")
	}
	return nil
}

func commandTTL(commandType string) time.Duration {
	switch commandType {
	case "reboot", "shutdown":
		return 5 * time.Minute
	case "start", "stop", "resume", "set-mode":
		return 10 * time.Minute
	default:
		return 24 * time.Hour
	}
}

func pruneCommands(agent *Agent) {
	if len(agent.Commands) <= 200 {
		return
	}
	kept := make([]Command, 0, 200)
	for _, command := range agent.Commands {
		if command.AckedAt == nil {
			kept = append(kept, command)
		}
	}
	for index := len(agent.Commands) - 1; index >= 0 && len(kept) < 200; index-- {
		if agent.Commands[index].AckedAt != nil {
			kept = append(kept, agent.Commands[index])
		}
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].Sequence < kept[j].Sequence })
	agent.Commands = kept
}

func (s *Store) recordAuditLocked(event AuditEvent) {
	event.Time = time.Now().UTC()
	s.state.Audit = append(s.state.Audit, event)
	if len(s.state.Audit) > 1000 {
		s.state.Audit = append([]AuditEvent(nil), s.state.Audit[len(s.state.Audit)-1000:]...)
	}
}

func cloneAgent(source *Agent) *Agent {
	clone := *source
	if source.LastHeartbeat != nil {
		heartbeat := *source.LastHeartbeat
		heartbeat.GPUs = append([]GPUStatus(nil), source.LastHeartbeat.GPUs...)
		clone.LastHeartbeat = &heartbeat
	}
	clone.HeartbeatHistory = append([]Heartbeat(nil), source.HeartbeatHistory...)
	for index := range clone.HeartbeatHistory {
		clone.HeartbeatHistory[index].GPUs = append([]GPUStatus(nil), source.HeartbeatHistory[index].GPUs...)
	}
	clone.Commands = append([]Command(nil), source.Commands...)
	for index := range clone.Commands {
		if source.Commands[index].Payload != nil {
			clone.Commands[index].Payload = make(map[string]string, len(source.Commands[index].Payload))
			for key, value := range source.Commands[index].Payload {
				clone.Commands[index].Payload[key] = value
			}
		}
	}
	return &clone
}
