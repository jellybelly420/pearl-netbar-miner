package health

import (
	"time"

	"github.com/jellybelly420/pearl-netbar-miner/internal/config"
	"github.com/jellybelly420/pearl-netbar-miner/internal/miner"
)

type Action string

const (
	ActionNone    Action = "none"
	ActionRestart Action = "restart"
	ActionPause   Action = "pause"
	ActionReboot  Action = "reboot"
)

type Result struct {
	Action     Action
	Reason     string
	PauseUntil time.Time
}

type Monitor struct {
	config           config.Health
	runningSince     time.Time
	lastPositiveHash time.Time
	lastAccepted     time.Time
	acceptedShares   int64
	restarts         []time.Time
	lastRestart      time.Time
}

func New(configuration config.Health) *Monitor {
	return &Monitor{config: configuration}
}

func (m *Monitor) Observe(now time.Time, running bool, statistics *miner.Statistics) Result {
	m.pruneRestarts(now)
	if !running {
		m.resetRun(time.Time{})
		return Result{Action: ActionNone}
	}
	if m.runningSince.IsZero() {
		m.resetRun(now)
	}
	if statistics != nil {
		if statistics.Hashrate10s > 0 {
			m.lastPositiveHash = now
		}
		if statistics.Accepted > m.acceptedShares {
			m.acceptedShares = statistics.Accepted
			m.lastAccepted = now
		}
	}
	if now.Sub(m.runningSince) < time.Duration(m.config.StartupGraceSeconds)*time.Second {
		return Result{Action: ActionNone}
	}
	if m.config.RestartCooldownSeconds > 0 && !m.lastRestart.IsZero() &&
		now.Sub(m.lastRestart) < time.Duration(m.config.RestartCooldownSeconds)*time.Second {
		return Result{Action: ActionNone}
	}
	if m.config.ZeroHashTimeoutSeconds > 0 {
		lastHash := m.lastPositiveHash
		if lastHash.IsZero() {
			lastHash = m.runningSince
		}
		if now.Sub(lastHash) >= time.Duration(m.config.ZeroHashTimeoutSeconds)*time.Second {
			return m.recovery(now, "hashrate remained zero or unavailable")
		}
	}
	if m.config.NoShareTimeoutSeconds > 0 && statistics != nil && statistics.Hashrate10s > 0 {
		lastShare := m.lastAccepted
		if lastShare.IsZero() {
			lastShare = m.runningSince
		}
		if now.Sub(lastShare) >= time.Duration(m.config.NoShareTimeoutSeconds)*time.Second {
			return m.recovery(now, "no accepted shares within the configured timeout")
		}
	}
	return Result{Action: ActionNone}
}

func (m *Monitor) recovery(now time.Time, reason string) Result {
	if m.config.MaxRestarts > 0 && len(m.restarts) >= m.config.MaxRestarts {
		until := now.Add(time.Duration(m.config.RestartWindowSeconds) * time.Second)
		if len(m.restarts) > 0 {
			until = m.restarts[0].Add(time.Duration(m.config.RestartWindowSeconds) * time.Second)
			if until.Before(now) {
				until = now.Add(time.Duration(m.config.RestartCooldownSeconds) * time.Second)
			}
		}
		if m.config.RebootOnExhausted {
			return Result{Action: ActionReboot, Reason: reason + "; restart budget exhausted"}
		}
		return Result{Action: ActionPause, Reason: reason + "; restart budget exhausted", PauseUntil: until}
	}
	m.restarts = append(m.restarts, now)
	m.lastRestart = now
	m.resetRun(now)
	return Result{Action: ActionRestart, Reason: reason}
}

func (m *Monitor) resetRun(started time.Time) {
	m.runningSince = started
	m.lastPositiveHash = time.Time{}
	m.lastAccepted = time.Time{}
	m.acceptedShares = 0
}

func (m *Monitor) pruneRestarts(now time.Time) {
	if m.config.RestartWindowSeconds <= 0 || len(m.restarts) == 0 {
		return
	}
	cutoff := now.Add(-time.Duration(m.config.RestartWindowSeconds) * time.Second)
	kept := m.restarts[:0]
	for _, restarted := range m.restarts {
		if !restarted.Before(cutoff) {
			kept = append(kept, restarted)
		}
	}
	m.restarts = kept
}
