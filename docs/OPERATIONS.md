# Operations

## Rollout order

1. One authorized NVIDIA Windows 10/11 seat.
2. Five-seat canary group for at least 24 hours.
3. One row or zone for at least 48 hours.
4. Remaining seats in bounded batches.

Do not start with the entire café. Keep `allow_remote_power_actions` false until
the pilot proves that restart limits and customer-use detection are correct.

## Acceptance evidence

For each hardware family record:

- Windows build, GPU model and driver version;
- agent and WildRig versions plus SHA-256 values;
- pool endpoint and worker name;
- connection, first accepted share and pool-side worker visibility;
- idle-to-start time and input/game-to-stop latency;
- resume behavior after the game exits;
- temperature, power, rejected-share ratio and 24-hour restart count.

The pool dashboard is the source of truth for credited shares. A local hashrate
number alone is not proof of accepted work or earnings.

## Tuning

Begin with no core/memory offsets. Establish a stable temperature and accepted
share baseline first, then tune one hardware family and one field at a time.
Use canary seats. `reset_oc_on_start` resets settings before WildRig applies the
configured profile, but driver-specific restoration after process exit still
requires hardware validation.

## Updates

- Keep the Ed25519 private key offline.
- Review upstream fee terms and release notes before signing a manifest.
- Start at `canary_percent: 5` or `10`.
- Watch accepted shares and restart counts, not only hashrate.
- Increase the percentage only after the canary window passes.

## Antivirus

Run `scripts/diagnose-windows-security.ps1`. It only reports Defender status and
matching detections. The project deliberately does not create silent antivirus
exclusions or restore quarantined files. Review every binary hash before any
administrator chooses a narrowly scoped exception.

## Incident stop

1. Use the dashboard batch `stop` command.
2. Confirm every seat reports `miner_running: false`.
3. Disable the scheduled task on affected seats if the control service is
   unavailable.
4. Preserve logs and control state before changing configuration.
