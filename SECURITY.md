# Security policy

## Supported versions

Only the latest release receives security fixes. The host updater installs new releases
automatically, so staying current is the default.

## Reporting a vulnerability

Please report vulnerabilities privately through GitHub:
**Security → Report a vulnerability** on this repository. Do not open a public issue,
pull request or discussion for a security problem.

Include what you found, how to reproduce it, the affected version (`dzm version`) and
whether it needs an account, an API key or access to the game server. You will get an
answer as soon as possible; please give a reasonable time to ship a fix before disclosing.

Areas of particular interest: authentication and 2FA, account roles and route permissions,
API keys and the MCP endpoint, the `@DZMBridge` ingest and command channel, and the updater.
