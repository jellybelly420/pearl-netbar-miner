# Test plan

## Automated

| Area | Evidence |
|---|---|
| Policy | Idle, background game, full-screen, overnight schedule, periodic rest, GPU start threshold |
| Health | Startup grace, zero-hash recovery, no-share tracking, restart budget |
| Miner supply chain | Archive hash, executable hash, safe ZIP extraction, pinned-artifact integration |
| Miner launch | Deterministic PearlHash and per-GPU WildRig arguments |
| Fleet API | Enrollment, HMAC replay rejection, command/ack, batch, history, audit, RBAC |
| Signed updates | Ed25519 verification, tamper rejection, deterministic canary assignment |
| Builds | Native control binaries and Windows amd64 agent/manifest binaries |

Run:

```bash
go test -race ./...
go vet ./...
GOOS=windows GOARCH=amd64 go build ./cmd/pearl-agent
```

## Required physical Windows tests

Automated cross-builds cannot prove these behaviors:

1. WildRig JSON API schema on the pinned Windows build.
2. Accepted shares and worker visibility at the selected pool.
3. NVIDIA process enumeration while DirectX games start, minimize and exit.
4. Temperature/power readings across installed driver versions.
5. Per-GPU power, clock, memory and fan application/restoration.
6. Driver-crash recovery and optional Windows reboot authorization.
7. Windows Defender behavior and any site-specific endpoint security policy.

Each failure must preserve the user experience: stop mining, keep the last known
good signed miner, and avoid repeated reboot loops.
