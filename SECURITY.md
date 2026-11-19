# Security Policy

Tellwire is a gateway that bridges browsers to Telnet servers, so security reports are taken seriously.

## Reporting a vulnerability

Please **do not open a public issue** for security problems. Report them privately through GitHub:
**Security → Report a vulnerability** on <https://github.com/espiegel/tellwire/security/advisories/new>.

Include what you found, how to reproduce it, and the version or commit. You can expect an acknowledgement
within a few days. Fixes are coordinated with you, and you are credited unless you prefer otherwise.

## Scope

In scope: the gateway (`gateway/`), the web client (`frontend/`) and the deployment files (`deploy/`).
Examples: Origin-check bypass, reaching an upstream not on the allowlist, SSRF, header or log injection,
credential leakage, resource exhaustion that bypasses the configured limits.

Out of scope: vulnerabilities in a MUD server you point Tellwire at, and issues that need a
misconfigured deployment (for example, `GATEWAY_TRUSTED_PROXIES` set to an untrusted network).

## Supported versions

Tellwire is early software. Only the latest commit on `main` receives fixes.
