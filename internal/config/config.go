package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/jellybelly420/pearl-netbar-miner/internal/policy"
)

type Miner struct {
	Version          string `json:"version"`
	URL              string `json:"url"`
	SHA256           string `json:"sha256"`
	Executable       string `json:"executable"`
	ExecutableSHA256 string `json:"executable_sha256"`
	InstallDir       string `json:"install_dir"`
}

type Mining struct {
	Wallet            string `json:"wallet"`
	Worker            string `json:"worker"`
	Pool              string `json:"pool"`
	Password          string `json:"password"`
	APIPort           int    `json:"api_port"`
	GPUList           string `json:"gpu_list"`
	PowerLimit        int    `json:"power_limit"`
	TemperatureLimit  int    `json:"temperature_limit"`
	TemperatureResume int    `json:"temperature_resume"`
}

type Config struct {
	AgentID          string        `json:"agent_id"`
	AgentSecret      string        `json:"agent_secret"`
	ControlURL       string        `json:"control_url"`
	PollSeconds      int           `json:"poll_seconds"`
	HeartbeatSeconds int           `json:"heartbeat_seconds"`
	StatePath        string        `json:"state_path"`
	Miner            Miner         `json:"miner"`
	Mining           Mining        `json:"mining"`
	Policy           policy.Config `json:"policy"`
}

var (
	sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
	workerPattern = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)
	walletPattern = regexp.MustCompile(`^prl1[023456789acdefghjklmnpqrstuvwxyz]{20,100}$`)
)

func Load(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer file.Close()

	var cfg Config
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if !workerPattern.MatchString(c.AgentID) {
		return errors.New("agent_id must contain 1-64 letters, digits, or hyphens")
	}
	if c.PollSeconds < 2 || c.HeartbeatSeconds < 5 {
		return errors.New("poll_seconds must be >= 2 and heartbeat_seconds >= 5")
	}
	if c.StatePath == "" {
		return errors.New("state_path is required")
	}
	if !c.Policy.Valid() {
		return errors.New("invalid policy configuration")
	}
	if !walletPattern.MatchString(strings.ToLower(c.Mining.Wallet)) {
		return errors.New("wallet must be a prl1 address; placeholders are not runnable")
	}
	if !workerPattern.MatchString(c.Mining.Worker) {
		return errors.New("worker must contain 1-64 letters, digits, or hyphens")
	}
	if strings.ContainsAny(c.Mining.Pool, " \t\r\n") || !strings.Contains(c.Mining.Pool, ":") {
		return errors.New("pool must be a host:port value without whitespace")
	}
	if _, _, err := net.SplitHostPort(c.Mining.Pool); err != nil {
		return errors.New("pool must be a valid host:port value")
	}
	if c.Mining.APIPort < 1024 || c.Mining.APIPort > 65535 {
		return errors.New("api_port must be between 1024 and 65535")
	}
	if c.Mining.TemperatureLimit < 0 || c.Mining.TemperatureResume < 0 || c.Mining.PowerLimit < 0 {
		return errors.New("temperature and power settings cannot be negative")
	}
	if c.Miner.Version == "" || c.Miner.URL == "" || c.Miner.Executable == "" || c.Miner.InstallDir == "" {
		return errors.New("complete miner metadata is required")
	}
	if !sha256Pattern.MatchString(strings.ToLower(c.Miner.SHA256)) {
		return errors.New("miner sha256 must be 64 lowercase hexadecimal characters")
	}
	if !sha256Pattern.MatchString(strings.ToLower(c.Miner.ExecutableSHA256)) {
		return errors.New("miner executable_sha256 must be 64 lowercase hexadecimal characters")
	}
	minerURL, err := url.Parse(c.Miner.URL)
	if err != nil || minerURL.Scheme != "https" || minerURL.Host == "" {
		return errors.New("miner url must be an absolute HTTPS URL")
	}
	if c.ControlURL != "" {
		controlURL, err := url.Parse(c.ControlURL)
		if err != nil || controlURL.Host == "" {
			return errors.New("control_url must be absolute")
		}
		loopback := controlURL.Hostname() == "127.0.0.1" || controlURL.Hostname() == "localhost" || controlURL.Hostname() == "::1"
		if controlURL.Scheme != "https" && !(loopback && controlURL.Scheme == "http") {
			return errors.New("control_url must use HTTPS except for loopback development")
		}
		if len(c.AgentSecret) < 32 {
			return errors.New("agent_secret must contain at least 32 characters")
		}
	}
	return nil
}
