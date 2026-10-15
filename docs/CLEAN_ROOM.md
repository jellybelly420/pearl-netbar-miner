# Clean-room boundary

This repository was implemented from public product behavior, public protocol
documentation, and independently observed file metadata.

It intentionally excludes:

- Vminer source code or decompiled source reproduction;
- Vminer credentials, tokens, certificates, signatures, icons, or UI assets;
- patches that disable or bypass fees in Vminer or another miner;
- redistribution of third-party miner binaries.

The agent downloads an official upstream miner release and verifies its
published digest. Third-party license and fee terms remain the responsibility
of the deployer and must be reviewed on every version change.

