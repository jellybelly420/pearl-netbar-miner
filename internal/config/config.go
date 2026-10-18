package config

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
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
	Wallet            string       `json:"wallet"`
	Worker            string       `json:"worker"`
	Pool              string       `json:"pool"`
	Password          string       `json:"password"`
	APIPort           int          `json:"api_port"`
	GPUList           string       `json:"gpu_list"`
	PowerLimit        int          `json:"power_limit"`
	TemperatureLimit  int          `json:"temperature_limit"`
	TemperatureResume int          `json:"temperature_resume"`
	ResetOCOnStart    bool         `json:"reset_oc_on_start"`
	Watchdog          bool         `json:"watchdog"`
	GPUProfiles       []GPUProfile `json:"gpu_profiles,omitempty"`
}

type GPUProfile struct {
	Index        int `json:"index"`
	PowerLimit   int `json:"power_limit"`
	CoreClock    int `json:"core_clock"`
	CoreOffset   int `json:"core_offset"`
	MemoryClock  int `json:"memory_clock"`
	MemoryOffset int `json:"memory_offset"`
	FanSpeed     int `json:"fan_speed"`
}

type Health struct {
	StartupGraceSeconds    int  `json:"startup_grace_seconds"`
	ZeroHashTimeoutSeconds int  `json:"zero_hash_timeout_seconds"`
	NoShareTimeoutSeconds  int  `json:"no_share_timeout_seconds"`
	RestartCooldownSeconds int  `json:"restart_cooldown_seconds"`
	MaxRestarts            int  `json:"max_restarts"`
	RestartWindowSeconds   int  `json:"restart_window_seconds"`
	RebootOnExhausted      bool `json:"reboot_on_exhausted"`
}

type Config struct {
	AgentID                 string        `json:"agent_id"`
	AgentSecret             string        `json:"agent_secret"`
	ControlURL              string        `json:"control_url"`
	PollSeconds             int           `json:"poll_seconds"`
	HeartbeatSeconds        int           `json:"heartbeat_seconds"`
	StatePath               string        `json:"state_path"`
	Miner                   Miner         `json:"miner"`
	Mining                  Mining        `json:"mining"`
	Policy                  policy.Config `json:"policy"`
	Health                  Health        `json:"health"`
	Update                  Update        `json:"update"`
	AllowRemotePowerActions bool          `json:"allow_remote_power_actions"`
}

type Update struct {
	Enabled              bool   `json:"enabled"`
	ManifestURL          string `json:"manifest_url"`
	PublicKey            string `json:"public_key"`
	Channel              string `json:"channel"`
	CheckIntervalSeconds int    `json:"check_interval_seconds"`
	CanaryPercent        int    `json:"canary_percent"`
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
	if err := ValidateMining(c.Mining); err != nil {
		return err
	}
	if err := ValidateHealth(c.Health); err != nil {
		return err
	}
	if err := ValidateMiner(c.Miner); err != nil {
		return err
	}
	if err := ValidateUpdate(c.Update); err != nil {
		return err
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

func ValidateMiner(miner Miner) error {
	if miner.Version == "" || miner.URL == "" || miner.Executable == "" || miner.InstallDir == "" {
		return errors.New("complete miner metadata is required")
	}
	if !sha256Pattern.MatchString(strings.ToLower(miner.SHA256)) {
		return errors.New("miner sha256 must be 64 lowercase hexadecimal characters")
	}
	if !sha256Pattern.MatchString(strings.ToLower(miner.ExecutableSHA256)) {
		return errors.New("miner executable_sha256 must be 64 lowercase hexadecimal characters")
	}
	if filepath.Base(miner.Executable) != miner.Executable || miner.Executable == "." || miner.Executable == ".." {
		return errors.New("miner executable must be a filename without a path")
	}
	cleanInstall := filepath.Clean(miner.InstallDir)
	if filepath.IsAbs(cleanInstall) || cleanInstall == "." || cleanInstall == ".." || strings.HasPrefix(cleanInstall, ".."+string(filepath.Separator)) {
		return errors.New("miner install_dir must be a scoped relative directory")
	}
	minerURL, err := url.Parse(miner.URL)
	if err != nil || minerURL.Scheme != "https" || minerURL.Host == "" {
		return errors.New("miner url must be an absolute HTTPS URL")
	}
	return nil
}

func ValidateUpdate(update Update) error {
	if !update.Enabled {
		return nil
	}
	manifestURL, err := url.Parse(update.ManifestURL)
	if err != nil || manifestURL.Scheme != "https" || manifestURL.Host == "" {
		return errors.New("update manifest_url must be an absolute HTTPS URL")
	}
	publicKey, err := base64.RawStdEncoding.DecodeString(strings.TrimSpace(update.PublicKey))
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return errors.New("update public_key must be a base64 Ed25519 public key")
	}
	if !workerPattern.MatchString(update.Channel) {
		return errors.New("update channel must contain 1-64 letters, digits, or hyphens")
	}
	if update.CheckIntervalSeconds < 300 {
		return errors.New("update check_interval_seconds must be at least 300")
	}
	if update.CanaryPercent < 1 || update.CanaryPercent > 100 {
		return errors.New("update canary_percent must be between 1 and 100")
	}
	return nil
}

func ValidateMining(mining Mining) error {
	if !walletPattern.MatchString(strings.ToLower(mining.Wallet)) {
		return errors.New("wallet must be a prl1 address; placeholders are not runnable")
	}
	if !workerPattern.MatchString(mining.Worker) {
		return errors.New("worker must contain 1-64 letters, digits, or hyphens")
	}
	if strings.ContainsAny(mining.Pool, " \t\r\n") || !strings.Contains(mining.Pool, ":") {
		return errors.New("pool must be a host:port value without whitespace")
	}
	if _, _, err := net.SplitHostPort(mining.Pool); err != nil {
		return errors.New("pool must be a valid host:port value")
	}
	if mining.APIPort < 1024 || mining.APIPort > 65535 {
		return errors.New("api_port must be between 1024 and 65535")
	}
	if mining.TemperatureLimit < 0 || mining.TemperatureResume < 0 || mining.PowerLimit < 0 {
		return errors.New("temperature and power settings cannot be negative")
	}
	seen := make(map[int]struct{}, len(mining.GPUProfiles))
	if len(mining.GPUProfiles) > 0 && mining.GPUList != "" {
		return errors.New("use either gpu_list or gpu_profiles, not both")
	}
	for _, profile := range mining.GPUProfiles {
		if profile.Index < 0 || profile.Index > 63 {
			return errors.New("gpu profile index must be between 0 and 63")
		}
		if _, exists := seen[profile.Index]; exists {
			return errors.New("gpu profile indexes must be unique")
		}
		seen[profile.Index] = struct{}{}
		if profile.PowerLimit < 0 || profile.PowerLimit > 1000 || profile.FanSpeed < 0 || profile.FanSpeed > 100 {
			return errors.New("gpu profile power/fan value is outside the safe validation range")
		}
		if profile.CoreClock < 0 || profile.CoreClock > 5000 || profile.MemoryClock < 0 || profile.MemoryClock > 20000 {
			return errors.New("gpu profile clock is outside the safe validation range")
		}
		if profile.CoreOffset < -2000 || profile.CoreOffset > 2000 || profile.MemoryOffset < -5000 || profile.MemoryOffset > 5000 {
			return errors.New("gpu profile offset is outside the safe validation range")
		}
	}
	return nil
}

func ValidateHealth(health Health) error {
	values := []int{health.StartupGraceSeconds, health.ZeroHashTimeoutSeconds, health.NoShareTimeoutSeconds, health.RestartCooldownSeconds, health.MaxRestarts, health.RestartWindowSeconds}
	for _, value := range values {
		if value < 0 {
			return errors.New("health values cannot be negative")
		}
	}
	if health.MaxRestarts > 0 && health.RestartWindowSeconds == 0 {
		return errors.New("restart_window_seconds is required when max_restarts is set")
	}
	return nil
}

func ApplyMiningOverride(base Mining, values map[string]string) (Mining, error) {
	candidate := base
	candidate.GPUProfiles = append([]GPUProfile(nil), base.GPUProfiles...)
	for key, value := range values {
		var err error
		switch key {
		case "wallet":
			candidate.Wallet = value
		case "worker":
			candidate.Worker = value
		case "pool":
			candidate.Pool = value
		case "gpu_list":
			candidate.GPUList = value
		case "power_limit":
			candidate.PowerLimit, err = strconv.Atoi(value)
		case "temperature_limit":
			candidate.TemperatureLimit, err = strconv.Atoi(value)
		case "temperature_resume":
			candidate.TemperatureResume, err = strconv.Atoi(value)
		case "reset_oc_on_start":
			candidate.ResetOCOnStart, err = strconv.ParseBool(value)
		case "watchdog":
			candidate.Watchdog, err = strconv.ParseBool(value)
		default:
			return Mining{}, errors.New("unsupported mining field: " + key)
		}
		if err != nil {
			return Mining{}, errors.New("invalid mining field " + key)
		}
	}
	if err := ValidateMining(candidate); err != nil {
		return Mining{}, err
	}
	sort.Slice(candidate.GPUProfiles, func(i, j int) bool { return candidate.GPUProfiles[i].Index < candidate.GPUProfiles[j].Index })
	return candidate, nil
}

func ApplyGPUProfile(base Mining, values map[string]string) (Mining, error) {
	base.GPUProfiles = append([]GPUProfile(nil), base.GPUProfiles...)
	indexValue, exists := values["index"]
	if !exists {
		return Mining{}, errors.New("gpu profile index is required")
	}
	index, err := strconv.Atoi(indexValue)
	if err != nil {
		return Mining{}, errors.New("invalid gpu profile index")
	}
	profile := GPUProfile{Index: index}
	found := false
	for _, existing := range base.GPUProfiles {
		if existing.Index == index {
			profile = existing
			found = true
			break
		}
	}
	for key, value := range values {
		if key == "index" {
			continue
		}
		var parsed int
		parsed, err = strconv.Atoi(value)
		if err != nil {
			return Mining{}, errors.New("invalid gpu profile field " + key)
		}
		switch key {
		case "power_limit":
			profile.PowerLimit = parsed
		case "core_clock":
			profile.CoreClock = parsed
		case "core_offset":
			profile.CoreOffset = parsed
		case "memory_clock":
			profile.MemoryClock = parsed
		case "memory_offset":
			profile.MemoryOffset = parsed
		case "fan_speed":
			profile.FanSpeed = parsed
		default:
			return Mining{}, errors.New("unsupported gpu profile field: " + key)
		}
	}
	if !found {
		base.GPUProfiles = append(base.GPUProfiles, profile)
	} else {
		for profileIndex := range base.GPUProfiles {
			if base.GPUProfiles[profileIndex].Index == index {
				base.GPUProfiles[profileIndex] = profile
			}
		}
	}
	base.GPUList = ""
	base.ResetOCOnStart = true
	if err := ValidateMining(base); err != nil {
		return Mining{}, err
	}
	sort.Slice(base.GPUProfiles, func(i, j int) bool { return base.GPUProfiles[i].Index < base.GPUProfiles[j].Index })
	return base, nil
}
