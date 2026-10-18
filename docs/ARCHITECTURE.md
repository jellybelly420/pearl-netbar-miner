# Architecture

## Scope

The production slice remains PRL-specific:

- `pearl-agent`: one Windows process per seat;
- `pearl-control`: a private fleet API and dashboard;
- WildRig: an independently downloaded PearlHash worker.

The controller does not implement the PearlHash kernel. It treats the miner as
an external process with a pinned distribution hash.

## Data flow

```text
viewer/operator/admin -> fleet API -> persisted command queue + audit
                                          |
                              HTTPS response to HMAC poll
                                          v
all processes + Windows input + NVIDIA GPU -> policy -> pearl-agent
                                                        |       |
                                             verified miner     | HMAC heartbeat
                                                        v       v
                                                     WildRig  fleet service
                                                        |
                                                     PRL pool
```

## Trust boundaries

1. Fleet API: distinct viewer/operator/admin bearer tokens; HTTPS required.
2. Agent API: per-seat HMAC key, timestamp window, nonce replay cache.
3. Miner supply chain: pinned hashes or an owner-signed Ed25519 manifest;
   archive traversal blocked and stale manifests rejected.
4. Process execution: deterministic arguments; no remote arbitrary command.
5. Windows power actions: a local per-seat flag must permit them.

## Persistence

The control service uses an atomically replaced JSON state file. This is a
practical choice for a single café and avoids a database dependency. Revisit
SQLite/PostgreSQL when there are multiple sites, concurrent administrators, or
more than a few hundred seats.

## Reliability

- Agent decisions remain local if the control service is unreachable.
- Smart policy defaults to stopping rather than starting when sensing fails.
- Miner start is idempotent; downloads are verified and installed atomically.
- Commands remain queued until acknowledged.
- Command acknowledgements contain an explicit success/error result.
- Hash/share health checks use bounded restart budgets and a cooldown.
- Signed miner updates use deterministic canaries and retain one rollback target.

## Growth path

Revisit these choices when the fleet grows:

- persistent command expiry and per-command signatures;
- SQLite/PostgreSQL plus append-only metrics storage;
- multi-site tenancy and external identity provider integration;
- Windows-code-signed agent self-update;
- diskless-image enrollment with short-lived provisioning credentials;
- native AMD/Intel process and telemetry providers.

