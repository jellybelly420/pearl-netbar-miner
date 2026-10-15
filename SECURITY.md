# Security policy

## Production requirements

1. Use HTTPS for every non-loopback control connection.
2. Generate a different agent secret for every seat.
3. Keep the admin token in a secret manager or protected environment variable.
4. Run the control service behind a firewall or private VPN.
5. Test miner upgrades on a canary seat and update the pinned SHA-256 manually.
6. Do not add arbitrary-shell execution to the fleet command protocol.
7. Keep a visible operator stop control and uninstall path.

## Secrets

Never commit PRL wallet signing keys, seed phrases, pool account passwords,
agent secrets, or administrator tokens. A payout address is not a signing key,
but deployments may still treat it as operationally sensitive.

## Reporting

Use GitHub's private vulnerability reporting for security issues. Do not place
live credentials, wallet secrets, or café network details in public issues.

