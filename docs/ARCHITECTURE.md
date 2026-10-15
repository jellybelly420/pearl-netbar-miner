# Architecture

## Scope

The first production slice is deliberately small:

- `pearl-agent`: one Windows process per seat;
- `pearl-control`: a private fleet API and dashboard;
- WildRig: an independently downloaded PearlHash worker.

The controller does not implement the PearlHash kernel. It treats the miner as
an external process with a pinned distribution hash.

## Data flow

```text
operator -> admin API -> persisted command queue
                               |
                         signed polling
                               v
Windows signals -> policy -> pearl-agent -> WildRig -> PRL pool
                         |
                     heartbeat
                         v
                 private fleet service
```

## Trust boundaries

1. Admin API: bearer token, HTTPS required in production.
2. Agent API: per-seat HMAC key, timestamp window, nonce replay cache.
3. Miner supply chain: fixed URL plus fixed SHA-256; archive traversal blocked.
4. Process execution: deterministic arguments; no remote arbitrary command.

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

## Growth path

Revisit these choices when the core is validated on real Windows hardware:

- signed release/update metadata and rollback protection;
- persistent command expiry and per-command signatures;
- WildRig hashrate/share API parser;
- per-GPU tuning with health-triggered rollback;
- role-based access control and multi-site tenancy;
- append-only audit log and metrics storage.

