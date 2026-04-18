# Insylus v1 Plan

---

**IMPLEMENTED** — This is the foundational v1 plan. Core server (devices, ssh_keys, bootstrap, policy), agent (managed account, SSH key enforcement, sudo/audit modes), and web UI are all implemented.

## Summary
- A Go server running on the controller host, serving both the web UI and the agent API on the LAN
- A small Go agent installed via a one-liner on each Linux host as a `systemd` service
- The controller host as the source of truth for managed account access policy, SSH key assignment, and device inventory
- A web UI focused on device inventory, host health, and access-state control for the managed account

The first version is single-account in behavior, but uses an extendable schema so multi-user support can be added later without reworking the core model.

## Key Changes
### Server application
- Implement a Go monolith with:
  - Web UI
  - JSON API for device registration, check-in, and policy retrieval
  - SQLite persistence for v1
- Core entities:
  - `devices`: identity, hostname, OS, IPs, last seen, health snapshot, agent version
  - `ssh_keys`: named public-key registry stored on the controller host with fingerprint and display name
  - `device_access_policy`: desired state for the managed account per device: enabled/disabled, sudo mode, audit-only mode, assigned key
  - `device_report`: last applied state, drift/errors, discovered remote facts
- Provide an install-command generator in the UI that emits a one-liner containing:
  - controller-host server URL or LAN address
  - one-time bootstrap token
  - agent version/channel
- Expose web actions for:
  - add/register device
  - assign/change SSH key
  - enable or disable the managed account
  - set `sudo=passwordless` or `audit-only`
  - view actual vs desired state
  - view health snapshot and last check-in

### Agent behavior
- One-liner installs a single static agent binary and registers a `systemd` service.
- Agent performs outbound polling to the controller host over LAN HTTP for v1.
  - Since deployment is LAN-only, do not require public-TLS setup in the initial build.
  - Authenticate every request with device credentials issued at bootstrap.
- On each sync, the agent:
  - ensures the managed account exists when policy says enabled
  - ensures the managed account is absent or disabled when policy says disabled
  - ensures the assigned SSH public key is present in the managed account `authorized_keys`
  - enforces either:
    - passwordless sudo via a dedicated file in `/etc/sudoers.d/`
    - audit-only access via read-log permissions
  - reports success, drift, and failures back to the controller host
- Health data reported:
  - hostname
  - OS/distribution
  - primary IPs
  - uptime
  - CPU load
  - memory usage
  - disk usage
  - last check-in
  - agent version

### Access and audit model
- `sudo` mode:
  - create the managed account if needed
  - install assigned key
  - grant passwordless sudo via managed sudoers snippet
- `audit-only` mode:
  - create the managed account if needed
  - install assigned key
  - do not grant sudo
  - grant read access to system logs using a managed group/permission strategy suited to systemd hosts
- The UI must always show:
  - desired access mode
  - last applied mode
  - assigned SSH key name
  - assigned SSH key fingerprint
  - whether enforcement succeeded

### Public interfaces and types
- HTTP UI routes:
  - device inventory page
  - device detail page
  - SSH key registry page
  - install-command generation page
- Agent API endpoints:
  - bootstrap/register
  - heartbeat/check-in
  - fetch desired policy
  - submit apply result
- Policy payload fields:
  - device id
  - `aia_enabled` (retained as a compatibility field name in v1)
  - `access_mode` as `disabled | audit | sudo`
  - assigned key id
  - assigned public key
  - policy revision
- Agent report fields:
  - policy revision applied
  - user presence/status
  - sudo status
  - audit status
  - authorized key fingerprint(s)
  - health snapshot
  - error message if apply failed

## Test Plan
- Server tests:
  - device registration and bootstrap token flow
  - policy CRUD and revisioning
  - SSH key registry validation and fingerprinting
  - API auth for registered agents
- Agent tests:
  - install/register flow on a fresh host
  - create the managed account and add key
  - switch `audit -> sudo -> audit -> disabled`
  - idempotent re-apply of unchanged policy
  - report drift when manual changes are made on the host
  - recover cleanly after controller-host restart or temporary network loss
- End-to-end scenarios:
  - add device from UI, run one-liner, host appears online
  - assign key and enable the managed account in audit mode
  - upgrade same host to passwordless sudo
  - disable the managed account and verify remote state is removed/disabled
  - UI shows key name from the registry and actual enforcement status
  - offline host is marked stale after missed check-ins

## Assumptions and Defaults
- v1 targets systemd-based Linux hosts on the home-lab LAN.
- SQLite is the default database for the first implementation.
- The controller host stores the authoritative public keys in an internal registry rather than scanning arbitrary filesystem key folders.
- The first release does not include browser-based shell access, command execution, or embedded log viewing.
- The first release optimizes for Debian/Ubuntu-style hosts first; any distro-specific audit-permission differences are handled with explicit best-effort support and surfaced in the agent status if unsupported.
- Security is LAN/home-lab grade for v1: authenticated agent-server communication, bootstrap tokens, and no internet exposure assumed.
