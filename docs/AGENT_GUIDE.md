# Insylus Guide For Agents

This document is for AI use.

It describes:

- what Insylus is responsible for
- how to discover available devices
- how to connect to devices over SSH
- which commands are safe to use for lookup
- how to interpret common states and failures

## Purpose

Insylus is a runtime-configurable plugin control plane. Core Insylus owns plugin settings and neutral targets; enabled plugins add inventory, agent, access, Docker, Proxmox, Jellyfin, services, topology, and wake behavior.

On the controller host, it provides:

- `/api/plugins` and `insylusctl plugins` for runtime plugin selection
- `/api/targets` for neutral target lookup
- optional plugin surfaces such as Devices, Agent, Access, Docker, Proxmox, Jellyfin, Services, Topology, and Wake

Disabling a plugin gates its already-registered web routes, API routes, and plugin static assets immediately; they return `404` without waiting for a restart. Enabling a plugin that was disabled at startup requires restarting `insylus.service` before its routes and assets are available.

When the Agent plugin is enabled, enrolled devices report inventory data:

- host health and last check-in state
- topology, parent/child relationships, and workloads
- agent version and auto-update state

Only when both Agent and Access are enabled can Insylus emit managed-account access policy:

- whether the managed account exists
- whether the managed account is disabled, audit-only, or passwordless sudo
- whether the assigned SSH public key is installed

## Important facts

- Insylus does not SSH for you.
- Enabled plugins may vary by install. Check `insylusctl plugins list` before assuming a command or route exists.
- Targets are neutral. A Docker-only or Proxmox-only install may not have the Devices or Access plugin enabled.
- Managed SSH is an Access plugin feature, not a property of every target.
- Insylus does not provide an interactive shell in the web UI.
- The web UI landing page `/` is the Dashboard plugin. Device inventory lives at `/devices`.
- To connect, use normal `ssh`.
- Managed SSH aliases are maintained automatically when SSH sync is installed.
- Friendly device names may contain uppercase letters, but the preferred SSH alias is lowercase.
- Example:
  - friendly name: `MiscServer`
  - SSH alias: `miscserver`

## Primary lookup methods

Use either the local CLI on the controller host or the HTTP API.

Preferred local command:

```bash
insylusctl devices --json
```

Preferred service lookup:

```bash
insylusctl services --find <service-name> --json
```

Short alias:

```bash
insylus devices --json
```

Remote API:

```bash
curl http://127.0.0.1:8080/api/devices
```

Single device:

```bash
insylusctl devices --find MiscServer --json
curl "http://127.0.0.1:8080/api/devices/find?q=MiscServer"
```

Find a service:

```bash
insylusctl services --find jellyfin --json
curl "http://127.0.0.1:8080/api/services/find?q=jellyfin"
```

## CLI reference

Top-level help:

```bash
insylusctl --help
insylusctl help
insylusctl help devices
insylusctl plugins
```

List devices as a table:

```bash
insylusctl devices
```

List devices as JSON:

```bash
insylusctl devices --json
```

List discovered services:

```bash
insylusctl services
insylusctl services --list --json
```

Find service instances:

```bash
insylusctl services --find jellyfin
insylusctl services --find jellyfin --json
insylusctl services --find jellyfin --json --full
```

Configure, inspect, and operate Docker containers over SSH:

```bash
insylusctl docker set-host --name docker01 --host docker01.local --ssh-user operator
insylusctl docker list-hosts
insylusctl docker --host docker01 --containers --json
insylusctl docker --host docker01 --logs <container> --tail 100
insylusctl docker --host docker01 --restart <container>
```

List services on one device:

```bash
insylusctl services --list --device docker01
insylusctl services --list --device docker01 --json
```

Read one device by find value:

```bash
insylusctl devices --find MiscServer
insylusctl devices --find MiscServer --json
insylusctl devices --find MiscServer --json --full
insylusctl devices --id <device-id>
insylusctl devices --id <device-id> --json
```

Wake a device when its inventory reports `wol.enabled=true`:

```bash
insylusctl devices --find MiscServer --json --full
insylusctl wake MiscServer
```

If the device has reported recently, `insylusctl wake` returns that it is already online. If a packet is sent, the command reports the targets used.

Query Proxmox when a node has a user-provided API token registered:

```bash
insylusctl proxmox --node beta-pve --list
insylusctl proxmox --node beta-pve --info jellyfin --json
insylusctl proxmox --node beta-pve --node-status
```

Start, stop, or restart a VM/LXC through the Proxmox API:

```bash
insylusctl proxmox --node beta-pve --start 201
insylusctl proxmox --node beta-pve --stop jellyfin
insylusctl proxmox --node beta-pve --restart jellyfin
```

Stop and restart are non-interactive commands. Use them only when the requested power action is intentional.

Register a Proxmox token only after the human has created it in Proxmox:

```bash
insylusctl proxmox set-token --node beta-pve \
  --token-id "user@pam!insylus" \
  --token-secret "your-token-secret" \
  --tls-insecure
```

Insylus never creates Proxmox API tokens. Tokens should be created by the user in Proxmox under `Datacenter -> Permissions -> API Tokens`.

Query a non-default server URL:

```bash
insylusctl devices --server http://127.0.0.1:8080 --json
```

## API reference

List all devices:

```bash
curl http://127.0.0.1:8080/api/devices
```

Read one device:

```bash
curl "http://127.0.0.1:8080/api/devices/find?q=MiscServer"
curl http://127.0.0.1:8080/api/devices/<device-id>
```

Wake one device:

```bash
curl -X POST http://127.0.0.1:8080/api/devices/<device-id>/wake
```

Wake responses include `status` and `message`. `status="already_online"` means no packet was needed. `status="sent"` means the controller accepted the wake command and sent the magic packet.

Read services:

```bash
curl http://127.0.0.1:8080/api/services
curl "http://127.0.0.1:8080/api/services/find?q=jellyfin"
curl "http://127.0.0.1:8080/api/services?device=docker01"
```

Read and operate Proxmox nodes:

```bash
curl http://127.0.0.1:8080/api/proxmox/nodes
curl http://127.0.0.1:8080/api/proxmox/<device-id>/guests
curl http://127.0.0.1:8080/api/proxmox/<device-id>/node-status
curl -X POST http://127.0.0.1:8080/api/proxmox/<device-id>/start/<name-or-vmid>
curl -X POST http://127.0.0.1:8080/api/proxmox/<device-id>/stop/<name-or-vmid>
```

The Proxmox token registration API stores user-provided tokens; it does not create tokens in Proxmox.

The topology map is a human-facing web UI feature at `/topology`. It is not part of the public CLI or API surface. Manual graph nodes, links, labels, notes, and saved layout positions are operator-provided context and should be treated as authoritative hints, not discovery output.

Service cleanup is handled in the web UI at `/services`. The CLI and JSON API are read-oriented; use the web UI prune actions when a missing service was intentionally deleted.

Default JSON views:

- `GET /api/devices` and `insylusctl devices --json` default to an ultra-compact list view:
  - `name`
  - `hostname`
  - `ips`
- `GET /api/devices/find?q=...` and `insylusctl devices --find ... --json` default to an `info` view
- add `view=full` or `--full` when workloads, warnings, and full health/policy detail are needed
- `info` and `full` include `wol`; wake only when `wol.enabled` is `true`

Service JSON views:

- `GET /api/services` and `insylusctl services --json` default to a compact grouped index with `name`, `count`, `healthy`, `unhealthy`, `missing`, `kinds`, and `last_seen_at`
- `GET /api/services/find?q=...` and `insylusctl services --find ... --json` default to an `info` view with each matching service instance and its hosting device
- `GET /api/services?device=...` and `insylusctl services --list --device ... --json` default to an `info` view for that device
- `view=full` or `--full` adds stable service IDs, normalized names, and first/last report timestamps
- previously seen services that disappear from later discovery reports remain visible with `health="missing"` until they reappear or are pruned
- stopped Docker containers are reported as unhealthy; deleted Docker containers become missing and can be pruned from the `/services` web UI
- `/history` shows service discovered, missing, restored, and pruned events

Proxmox token permissions:

- Use `PVEAuditor` for read-only inventory, status, node, and cluster queries.
- Start, stop, and restart require `VM.PowerMgmt` on the target VM/LXC path. A broad `PVEAdmin` token works but is usually more permission than Insylus needs.
- Keep privilege separation enabled for Proxmox API tokens unless the human explicitly chooses otherwise.
- Insylus stores the supplied token secret encrypted with a local key file next to the controller SQLite database.
- If a node uses the normal self-signed Proxmox certificate, register it with `--tls-insecure` or set the same option in `/proxmox`.

The `info` view includes:

- `id`
- `name`
- `ssh_alias`
  Empty unless Insylus-managed SSH access is available for this device.
- `ssh_command`
  Empty unless Insylus-managed SSH access is available for this device.
- `hostname`
- `os_name`
- `ips`
- `last_seen_at`
- `agent_version`
- `agent_auto_update`
  Auto-update state for the Insylus agent. Important nested fields:
  - `enabled`
  - `override`
  - `update_available`
  - `server_agent_version`
  - `status`
  - `error`
- `wol`
  Wake-on-LAN detection from the agent. Important nested fields:
  - `enabled`
  - `supported`
  - `active`
  - `mac_address`
  - `interface`
  - `broadcast`
  - `reason`
- `managed_account_enabled`
  Indicates whether the managed account should be enabled.
- `managed_user`
  The Linux account name currently managed by the controller policy. New installs default to `bob` so the remote managed account is clearly a regular user account.
- `managed_groups`
  Linux groups granted in `audit` mode. Existing installs default to `adm` and `systemd-journal`; new/systemd installs can set `INSYLUS_MANAGED_GROUPS` before running the installer.
- `device_mode`
  Either `inventory-only` or `access-managed`. In `inventory-only`, Insylus does not modify users, SSH keys, sudoers, or groups.
- `access_mode`
- `assigned_key_name`
- `assigned_fingerprint`
- `policy_revision`
- `applied_revision`
- `enforcement_succeeded`
- `error_message`
- `health`
  In `info` view this is reduced to:
  - `hostname`
  - `os_name`
  - `ips`
  - `agent_version`

## How to decide whether a device is usable

A device is usually ready for use if all of these are true:

- `device_mode` is `access-managed`
- `managed_account_enabled` is `true`
- `access_mode` is not `disabled`
- `enforcement_succeeded` is `true`
- `ssh_alias` is not empty
- `last_seen_at` is recent enough for the task

If `applied_revision` is lower than `policy_revision`, the desired policy may not have been fully applied yet.

If `error_message` is not empty, treat the device as needing attention before relying on it.

If `agent_auto_update.status` is `failed`, the device is still running the previous agent binary. Treat this as maintenance-needed, but not necessarily an access-control failure.

Agents check in every 15 seconds. Insylus treats a device as online when `last_seen_at` is within the last 45 seconds.

## How to SSH to a device

Preferred command from the controller host:

```bash
ssh <ssh_alias>
```

Example:

```bash
ssh miscserver
```

Why this works:

- the controller host manages `/etc/ssh/ssh_config.d/insylus.conf`
- only access-managed, enabled devices are included
- the alias resolves to the first reported device IP
- user is set to the managed account
- identity file is set to the configured controller-host key
- known-host handling is configured automatically

Do not assume the friendly name and the SSH alias have identical casing.
Prefer `ssh_alias` from the API or CLI output.

## How to interpret access mode

First check `device_mode`.

`inventory-only`

- the device participates in inventory, health, topology, workloads, and agent auto-update
- the agent should not modify users, SSH keys, sudoers, or groups
- do not assume managed SSH access is available

`access-managed`

- Insylus may enforce managed-account policy according to `access_mode`

`disabled`

- the managed account is intended to be unavailable or locked on the device
- do not expect SSH access to be usable

`audit`

- the managed account should be able to SSH in
- the managed account should be able to read logs and inspect the system
- the managed account should not have passwordless sudo

`sudo`

- the managed account should be able to SSH in
- the managed account should have passwordless sudo

## Recommended decision flow

1. Query inventory with `insylusctl devices --json`
2. Select a device by `name`, `hostname`, or `ip`
3. Read the device with `insylusctl devices --find <value> --json`
4. Check `managed_account_enabled`
5. Check `managed_user` when the exact Linux account matters
6. Check `device_mode`
7. Check `access_mode`
8. Check `enforcement_succeeded`
9. Check `last_seen_at`
10. Use `ssh_command` or `ssh <ssh_alias>`

## Example workflow

Get all devices:

```bash
insylusctl devices --json
```

Read one device:

```bash
insylusctl devices --find MiscServer --json
```

Connect:

```bash
ssh miscserver
```

If `access_mode` is `sudo`, escalate:

```bash
sudo -i
```

If `access_mode` is `audit`, inspect logs:

```bash
journalctl -xe
journalctl -u <service-name>
```

## Non-interactive assumptions

Insylus is configured to avoid the first-connection `yes/no` SSH prompt for managed hosts on the controller host by using managed SSH config and known-host handling.

This reduces interactive failures for AI-driven command execution.

However:

- if a host key changes unexpectedly, SSH may still refuse the connection
- if the alias sync has not yet run, a new device alias may not be available immediately

## Troubleshooting

If `insylusctl` is not found:

```bash
hash -r
insylusctl --help
```

If the command is still not found:

```bash
/usr/local/bin/insylusctl --help
/opt/insylus/bin/insylusctl --help
```

Controller install defaults can be changed with `INSYLUS_INSTALL_ROOT`, `INSYLUS_BIN_DIR`, `INSYLUS_DATA_DIR`, `INSYLUS_LISTEN_ADDR`, `INSYLUS_APP_USER`, and `INSYLUS_APP_GROUP` when running `scripts/install-insylus-service.sh`. The controller service account is distinct from the remote managed-access account configured with `INSYLUS_MANAGED_USER`.

Managed user and audit group defaults can be changed from Access Settings at `/access/settings`; the legacy `/settings` path also works while the Access plugin is enabled. The persisted database setting overrides install-time managed-account defaults for future agent policy fetches. When access is enabled on a device, the agent links the account if it already exists or creates it if it is missing. Passwordless sudo is controlled per device by setting access mode to `sudo`.

Agent auto-update defaults can be changed from Agent Settings at `/agent/settings`.

Agent install defaults can be changed on the target host with `INSYLUS_AGENT_BIN_PATH`, `INSYLUS_AGENT_CONFIG_PATH`, `INSYLUS_AGENT_SERVICE_NAME`, and `INSYLUS_AGENT_UNIT_PATH` before running the web UI install command.

If `ssh <alias>` fails because the alias is unknown:

```bash
insylusctl devices --json
sudo systemctl start insylus-ssh-sync.service
```

Then retry:

```bash
ssh <ssh_alias>
```

If Insylus inventory looks stale:

```bash
insylusctl devices
systemctl status --no-pager insylus.service
systemctl status --no-pager insylus-ssh-sync.timer
```

If the remote device agent may be unhealthy:

Use Insylus inventory first. Then, if needed on the remote host:

```bash
systemctl status --no-pager insylus-agent.service
journalctl -u insylus-agent.service -n 50 --no-pager
```

## Safe defaults

- Prefer JSON output for automation.
- Prefer `--find` over `--id` when you know the device name, hostname, or IP.
- Prefer `ssh_alias` over guessing hostnames.
- Prefer `ssh_command` when provided.
- Do not assume a device is usable only because it exists in inventory.
- Treat non-empty `error_message` as a warning.
- Treat `enforcement_succeeded=false` as not ready.
