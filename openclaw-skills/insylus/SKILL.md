---
name: insylus
description: Use Insylus to discover homelab devices, find where services run, check device readiness for SSH access, and manage devices.
metadata: {"openclaw":{"os":["linux"],"requires":{"anyBins":["insylusctl","insylus"]},"emoji":"🖧"}}
---

# Insylus

Use this skill when you need to find a device, locate a running service, check if a device is ready for SSH, or manage homelab infrastructure.

## Core rules

- Query Insylus first. Never guess hostnames or IPs.
- Use `--find VALUE` for lookup by name, hostname, IP, or ID.
- Use `--server URL` to target a remote Insylus server (defaults to local).
- Prefer `--json` for machine-readable output.
- Check enabled plugins before assuming a command or route exists.
- Disabled plugin web routes, API routes, navigation items, and static assets return `404` or disappear immediately; enabling a compiled plugin makes it available immediately.
- Managed SSH requires the Access plugin and a successfully applied access policy.
- Insylus never creates API tokens for external services — a human must create and register them.
- Topology is a web map only; not available via CLI or API.
- `insylus` is installed as a shorter alias for `insylusctl` — both commands work identically.

## Commands

```bash
# Device inventory
insylusctl devices [--server URL] [--find VALUE | --id ID] [--json] [--compact|--info|--full]
insylusctl wake [--server URL] DEVICE [--json]

# Service inventory
insylusctl services [--server URL] [--json] [--find NAME | --list [--device DEVICE]] [--compact|--info|--full]

# Proxmox
insylusctl proxmox [--server URL] --node NODE [--list|--vms|--lxcs|--info GUEST|--status GUEST|--start GUEST|--stop GUEST|--restart GUEST|--node-status|--cluster-status] [--json]
insylusctl proxmox set-token --node DEVICE [--device-id ID] --token-id ID --token-secret SECRET [--node-name NAME] [--api-url URL] [--role ROLE] [--tls-insecure]
insylusctl proxmox list-tokens [--json]
insylusctl proxmox remove-token --node DEVICE [--device-id ID]

# Jellyfin
insylusctl jellyfin [--server URL] --device DEVICE [--list|--movies|--series|--episodes|--latest|--resume] [--json]
insylusctl jellyfin set-token --device DEVICE --api-key KEY [--server-name NAME] [--api-url URL] [--tls-insecure]
insylusctl jellyfin list-tokens [--json]
insylusctl jellyfin remove-token --device DEVICE

# Docker
insylusctl docker set-host --host HOST [--name NAME] [--ssh-user USER] [--device-id ID]
insylusctl docker list-hosts [--json]
insylusctl docker remove-host --host HOST
insylusctl docker --host HOST [--list|--containers|--images] [--json]
insylusctl docker --host HOST [--inspect NAME|--logs NAME|--stats NAME] [--tail N] [--timestamps] [--json]
insylusctl docker --host HOST [--start NAME|--stop NAME|--restart NAME|--pause NAME|--unpause NAME]

# Plugin management
insylusctl plugins list [--json]
insylusctl plugins enable PLUGIN
insylusctl plugins disable PLUGIN
insylusctl plugins purge PLUGIN
insylusctl plugins profiles [--json]
insylusctl plugins apply-profile PROFILE

# Help
insylusctl help [COMMAND]
```

## Device interpretation

| Field | Meaning |
|-------|---------|
| `device_mode=inventory-only` | SSH access not managed by Insylus |
| `device_mode=access-managed` | SSH access managed by Insylus |
| `access_mode=disabled` | Managed account locked/unavailable |
| `access_mode=audit` | SSH works, no passwordless sudo |
| `access_mode=sudo` | SSH works with passwordless sudo for the managed user |
| `enforcement_succeeded=false` | Not ready — policy not applied |
| `ssh_alias` | Preferred SSH alias |
| `last_seen_at` | Device last check-in time |

A device is ready for SSH when: `device_mode=access-managed` AND `managed_account_enabled=true` AND `access_mode != disabled` AND `enforcement_succeeded=true` AND `last_seen_at` is recent.

## Service interpretation

| State | Meaning |
|-------|---------|
| `healthy` | Running/up/active or reported with no state |
| `unhealthy` | Stopped/exited/dead/failed/error |
| `missing` | Previously seen, absent from latest report |
| `unknown` | Reported but unclassified |

## Proxmox token setup

Insylus never creates Proxmox tokens. Create one in Proxmox Dashboard > API Tokens, then register it:

```bash
insylusctl proxmox set-token --node <name> --token-id <id> --token-secret <secret> [--api-url URL] [--tls-insecure]
```

Suggested role for read-only: `PVEAuditor`. Power actions need `VM.PowerMgmt`.

## Web UI

- `/` — Dashboard: fleet health, service signal, recent events, and quick actions
- `/devices` — device inventory and targets
- `/discovery` — manual subnet scan and discovery review queue
- `/monitor` — active device and endpoint reachability checks
- `/services` — services and prune missing records
- `/history` — service discovery events
- `/keys` — SSH key management (Access plugin)
- `/agent/settings` — agent configuration (Agent plugin)
- `/proxmox` — Proxmox token management
- `/jellyfin` — Jellyfin token management
- `/docker` — Docker host configuration and container control
- `/topology` — topology map (web only, not CLI/API)
- `/update` — controller update management

## Troubleshooting

```bash
# If insylusctl not found
hash -r
/usr/local/bin/insylusctl --help
/opt/insylus/bin/insylusctl --help
```

If `ssh <alias>` fails: query inventory again, check `enforcement_succeeded`, and verify `access_mode != disabled`.

## Field reference

For detailed field documentation, read: `references/fields.md`

## Plugin commands

`insylusctl plugins` lists and manages plugins:

```bash
insylusctl plugins list [--json]           # List all registered plugins
insylusctl plugins enable PLUGIN          # Enable a disabled plugin
insylusctl plugins disable PLUGIN        # Disable a plugin
insylusctl plugins purge PLUGIN          # Remove plugin configuration
insylusctl plugins profiles [--json]      # List available profiles
insylusctl plugins apply-profile PROFILE  # Apply a plugin profile
```

## Discovery plugin

The Discovery plugin is a manual subnet review tool for checking which IPs are in use and which devices may still be missing from Insylus inventory.

### Rules

- Discovery is web/API-only; there is no dedicated `insylusctl discovery` command yet.
- Scans are manual and target one IPv4 subnet such as `192.168.0.0/24`.
- Presence is based on ping plus the controller's local ARP or neighbor table.
- Reverse DNS may provide a hostname; MAC is available only when the controller can learn it on the local network.
- Matches against existing targets/devices are shown as `known`, not `pending`.
- `known` entries should not be promoted again.

### API endpoints

```bash
GET  /api/discovery
POST /api/discovery/scan
POST /api/discovery/<candidate-id>/promote
POST /api/discovery/<candidate-id>/status
```

### Discovery statuses

- `pending` — discovered and not yet reviewed
- `known` — already matched to an existing target/device
- `ignored` — intentionally skipped
- `promoted` — added to inventory from discovery

### Web UI

- `/discovery` — manual subnet scan and discovery review queue

## Monitor plugin

The Monitor plugin adds active reachability checks for enrolled devices and arbitrary manual endpoints.

### Rules

- `insylusctl monitor` lists current monitor targets and states.
- `insylusctl monitor --status TARGET` resolves by key, device ID, name, or host.
- `insylusctl monitor --history TARGET --window 30m|1h|24h` returns recent samples.
- Enrolled devices are checked with ping against the best-known address.
- Manual targets use ping unless a TCP port is configured.
- The web page includes recent latency sparklines and 24-hour availability.

### API endpoints

```bash
GET  /api/monitor
GET  /api/monitor/<target-key>/history?window=1h
POST /api/monitor/check
POST /api/monitor/settings
POST /api/monitor/targets
POST /api/monitor/targets/<id>/delete
```

### Web UI

- `/monitor` — active device and endpoint reachability checks

## Devices plugin

The devices plugin is Insylus's core device inventory. It lists all enrolled devices, shows their status, and provides device lookup.

### Rules

- `--find VALUE` matches by device name, hostname, exact IP, or ID — use it instead of `--id` when possible.
- Without `--json`, output is a human-readable table. With `--json`, output is structured JSON.
- `--compact`, `--info`, and `--full` select the JSON view depth and are mutually exclusive.
- Devices without `ssh_alias` are not ready for Insylus-managed SSH access.

### CLI commands

```bash
insylusctl devices [--server URL] [--json] [--find VALUE | --id DEVICE_ID] [--compact|--info|--full]
```

- `insylusctl devices` prints the human table (no `--json` needed).
- `insylusctl devices --json` returns compact JSON: `name`, `hostname`, `ips`, `purpose`.
- `insylusctl devices --find <value> --json` returns info JSON for one matching device.
- `insylusctl devices --find <value> --json --full` adds workloads and health details.
- `insylusctl devices --id <device-id> --json` returns one device by stable ID.

### API endpoints

```bash
GET /api/devices?view=compact|info|full
GET /api/devices/find?q=<value>&view=compact|info|full
GET /api/devices/<device-id>?view=compact|info|full
POST /api/devices/<device-id>/wake
```

### Table columns

NAME, MODE, TYPE, PURPOSE, PARENT, AGENT, SSH, ACCESS, STATUS, LAST SEEN, IP

- **MODE**: `inventory-only` or `access-managed`
- **SSH**: `ssh_alias` value or `-` if not available
- **ACCESS**: `disabled`, `audit`, or `sudo`; `sudo` means passwordless sudo for the managed user
- **STATUS**: `enforcement_succeeded` and `managed_account_enabled` combined

### Interpretation

- `inventory-only` devices are enrolled for observability only — Insylus does not manage their SSH access.
- `access-managed` devices have Insylus-managed SSH accounts. If the managed user already exists, the agent links it; otherwise the agent creates it. Check `access_mode` and `enforcement_succeeded`.
- A device is SSH-ready when: `device_mode=access-managed` AND `managed_account_enabled=true` AND `access_mode != disabled` AND `enforcement_succeeded=true` AND `ssh_alias` is non-empty AND `last_seen_at` is recent.

### Web UI

- `/devices` — device list with full inventory and status

## Jellyfin plugin

Jellyfin is a free software media system. Insylus queries it through user-provided API keys to show library items, watched status, and resume points.

### Rules

- Jellyfin API keys are not permission-scoped — a key grants full access to the server.
- Library queries always use the default user ID stored in the Jellyfin token configuration; there is no per-command user override.

### CLI commands

Jellyfin query (device is Jellyfin server name, hostname, IP, or device ID):

```bash
insylusctl jellyfin [--server URL] --device <name-or-host-or-ip> --list [--json]
insylusctl jellyfin [--server URL] --device <name-or-host-or-ip> --movies [--json]
insylusctl jellyfin [--server URL] --device <name-or-host-or-ip> --series [--json]
insylusctl jellyfin [--server URL] --device <name-or-host-or-ip> --episodes [--json]
insylusctl jellyfin [--server URL] --device <name-or-host-or-ip> --latest [--json]
insylusctl jellyfin [--server URL] --device <name-or-host-or-ip> --resume [--json]
```

Token management:

```bash
insylusctl jellyfin set-token --device <device> --api-key <key> [--server-name <name>] [--api-url <url>] [--tls-insecure]
insylusctl jellyfin list-tokens [--json]
insylusctl jellyfin remove-token --device <device>
```

### API endpoints

```bash
GET  /api/jellyfin/servers
POST /api/jellyfin/tokens
POST /api/jellyfin/tokens/delete/<device_id>
GET  /api/jellyfin/<device_id>/libraries
GET  /api/jellyfin/<device_id>/items
GET  /api/jellyfin/<device_id>/movies
GET  /api/jellyfin/<device_id>/series
GET  /api/jellyfin/<device_id>/episodes
GET  /api/jellyfin/<device_id>/latest
GET  /api/jellyfin/<device_id>/resume
GET  /api/jellyfin/<device_id>/items/<item_id>
```

### Web UI

- `/jellyfin` — configure Jellyfin API tokens and view configured servers.

### Interpretation

- `--list` shows movies and series together, sorted by name.
- `--movies` and `--series` show items of that type only.
- `--episodes` shows all episodes sorted by series, season, episode number.
- `--latest` shows the 20 most recently added items.
- `--resume` shows in-progress items for the configured default user.
- JSON output includes `series_name` for episodes and resume items.
- Progress percentage is calculated from playback position vs. total runtime.
- User data (watched status, resume points) is always sourced from the configured default user ID.

## Proxmox plugin

Proxmox VE is a virtualization platform. Insylus queries it through user-provided API tokens to list VMs, LXCs, node status, and manage power state.

### Rules

- Insylus never creates Proxmox API tokens; a human must create one in Proxmox Dashboard > API Tokens and register it with Insylus.
- Create a Proxmox token with appropriate permissions, then register it with `insylusctl proxmox set-token`.
- For read-only access (list, info, status), use role `PVEAuditor`.
- For power actions (start/stop/restart), the token needs `VM.PowerMgmt`. A broad `PVEAdmin` token works but grants more permission than typically needed.
- Use `--tls-insecure` only for Proxmox nodes using the default self-signed certificate.

### CLI commands

Node lookup:

```bash
insylusctl proxmox [--server URL] --node <name-or-host-or-ip> --list [--json]
insylusctl proxmox [--server URL] --node <name-or-host-or-ip> --vms [--json]
insylusctl proxmox [--server URL] --node <name-or-host-or-ip> --lxcs [--json]
insylusctl proxmox [--server URL] --node <name-or-host-or-ip> --info <name-or-vmid> [--json]
insylusctl proxmox [--server URL] --node <name-or-host-or-ip> --status <name-or-vmid> [--json]
```

Power actions:

```bash
insylusctl proxmox [--server URL] --node <name-or-host-or-ip> --start <name-or-vmid> [--json]
insylusctl proxmox [--server URL] --node <name-or-host-or-ip> --stop <name-or-vmid> [--json]
insylusctl proxmox [--server URL] --node <name-or-host-or-ip> --restart <name-or-vmid> [--json]
```

Node status:

```bash
insylusctl proxmox [--server URL] --node <name-or-host-or-ip> --node-status [--json]
insylusctl proxmox [--server URL] --node <name-or-host-or-ip> --cluster-status [--json]
```

Token management:

```bash
insylusctl proxmox set-token --node <device> [--device-id <id>] --token-id <id> --token-secret <secret> [--node-name <name>] [--api-url <url>] [--role <role>] [--tls-insecure]
insylusctl proxmox list-tokens [--json]
insylusctl proxmox remove-token --node <device> [--device-id <id>]
```

### API endpoints

```bash
GET  /api/proxmox/nodes
POST /api/proxmox/tokens
POST /api/proxmox/tokens/delete/<device_id>
GET  /api/proxmox/<device_id>/guests
GET  /api/proxmox/<device_id>/vms
GET  /api/proxmox/<device_id>/lxcs
GET  /api/proxmox/<device_id>/status/<name-or-vmid>
POST /api/proxmox/<device_id>/start/<name-or-vmid>
POST /api/proxmox/<device_id>/stop/<name-or-vmid>
POST /api/proxmox/<device_id>/restart/<name-or-vmid>
GET  /api/proxmox/<device_id>/node-status
GET  /api/proxmox/<device_id>/cluster-status
```

### Web UI

- `/proxmox` — store and manage Proxmox API tokens

### Interpretation

- `--list` shows all VMs and LXCs together. `--vms` and `--lxcs` filter by type.
- `--node` accepts the Proxmox node name, Insylus device name, hostname, IP, or device ID.
- `--info` and `--status` show full guest details by name (substring match) or VMID.
- `--node-status` shows CPU, memory, disk, and uptime for the node.
- `--cluster-status` shows resource usage across the cluster.
- Start/stop/restart are non-blocking — they initiate the action and return.
- Guest names in output are the display names from Proxmox, not the hostname of the VM/LXC itself.

## Services plugin

The services plugin is Insylus's service inventory. It discovers and tracks systemd services, Docker containers, Proxmox VMs, and Proxmox LXCs across all enrolled devices.

### Rules

- `--find` matches by service name or image (case-insensitive substring). `--list` shows all.
- `--device` filters to one hosting device — accepts device name, hostname, IP, or ID.
- `--find` and `--device` are mutually exclusive — cannot combine `--list` with `--device`.
- Compact view groups duplicates by name. Info and full views show one record per instance.

### CLI commands

```bash
insylusctl services [--server URL] [--json] [--compact|--info|--full]
insylusctl services [--server URL] --list [--json] [--device DEVICE] [--compact|--info|--full]
insylusctl services [--server URL] --find <name-or-image> [--json] [--compact|--info|--full]
```

Table columns (list mode): SERVICE, COUNT, HEALTH, KINDS, LAST SEEN

Table columns (find/list with device): SERVICE, KIND, DEVICE, HEALTH, STATE, LAST SEEN, ENDPOINTS

### API endpoints

```bash
GET /api/services?view=compact|info|full
GET /api/services/find?q=<value>&view=compact|info|full
GET /api/services?device=<device>&view=compact|info|full
```

### Service health states

| State | Meaning |
|-------|---------|
| `healthy` | Running/up/active, or reported with no state |
| `unhealthy` | Stopped/exited/dead/failed/error |
| `missing` | Previously discovered, absent from latest device report |
| `unknown` | Reported but cannot be classified |

- Stopped Docker containers: `unhealthy`
- Deleted Docker containers: `missing` — prune at `/services` in the web UI
- Previously seen services remain listed as `missing` until re-discovered or pruned

### Interpretation

- Use `services --find <name>` to locate which device(s) run a specific service.
- Use `services --list --device <device>` to see everything running on a known device.
- Compact output groups duplicates — e.g., `jellyfin` with `count: 2` means 2 instances across devices.
- `missing_since` in the full view shows when the service disappeared from the device report.

### Web UI

- `/services` — service list with prune controls for missing records
- `/history` — discovery events (discovered, missing, restored, pruned)

## Wake plugin

The wake plugin sends Wake-on-LAN magic packets to devices that support it.

### Rules

- WoL is only sent when the device has `wol.enabled=true` in inventory.
- Devices seen recently (within the last 45 seconds) are reported as `already online` — no packet is sent.
- WoL packets are broadcast to the device's MAC address, not sent to a specific IP.

### CLI commands

```bash
insylusctl wake [--server URL] DEVICE [--json]
```

`DEVICE` is a required positional argument — device name, hostname, IP, or ID.

Without `--json`: prints a human-readable sentence ("Sent WoL magic packet to device" or "device is already online").

With `--json`: prints `{"status":"already_online"}` or `{"status":"sent"}`.

### API endpoint

```bash
POST /api/devices/<device-id>/wake
```

Returns: `{"status":"already_online"}` or `{"status":"sent"}`

## Docker plugin

The Docker plugin provides container lifecycle control and inspection for configured Docker hosts. Commands are executed over SSH using system SSH configuration, with an optional SSH user stored in the plugin.

### Rules

- Docker management is performed over SSH to the configured host using the `docker` CLI. The host must be reachable by normal SSH from the controller.
- Docker hosts can be standalone plugin targets or linked to an existing Insylus target.
- Discovery of running containers is handled separately by the services discovery system. This plugin adds control and inspection.
- If Docker is not installed on a host, operations will return an error indicating the Docker CLI is unavailable.
- Start, stop, restart, pause, and unpause are non-blocking — they initiate the action and return immediately.

### CLI commands

Host configuration:

```bash
insylusctl docker set-host --host <ssh-host> [--name <name>] [--ssh-user <user>] [--device-id <target-id>]
insylusctl docker list-hosts [--json]
insylusctl docker remove-host --host <name-or-host-or-id>
```

Container listing:

```bash
insylusctl docker --host <host> --list [--json]
insylusctl docker --host <host> --containers [--json]
```

Container inspection:

```bash
insylusctl docker --host <host> --inspect <name> [--json]
insylusctl docker --host <host> --logs <name> [--tail N] [--timestamps] [--json]
insylusctl docker --host <host> --stats <name> [--json]
```

Container lifecycle:

```bash
insylusctl docker --host <host> --start <name>
insylusctl docker --host <host> --stop <name>
insylusctl docker --host <host> --restart <name>
insylusctl docker --host <host> --pause <name>
insylusctl docker --host <host> --unpause <name>
```

Images:

```bash
insylusctl docker --host <host> --images [--json]
```

### API endpoints

```bash
GET  /api/docker/nodes
GET  /api/docker/config
GET  /api/docker/config/<target_id>
POST /api/docker/config
POST /api/docker/config/<target_id>/delete
GET  /api/docker/containers/<target_id>?all=0|1
GET  /api/docker/containers/<target_id>/<name>/inspect
GET  /api/docker/containers/<target_id>/<name>/logs?tail=100&timestamps=false
GET  /api/docker/containers/<target_id>/<name>/stats
POST /api/docker/containers/<target_id>/<name>/start
POST /api/docker/containers/<target_id>/<name>/stop
POST /api/docker/containers/<target_id>/<name>/restart
POST /api/docker/containers/<target_id>/<name>/pause
POST /api/docker/containers/<target_id>/<name>/unpause
GET  /api/docker/images/<target_id>
```

### Web UI

- `/docker` — configure Docker hosts and list configured hosts
- `/docker/devices/<target_id>` — container list and image list for a configured Docker host

### Interpretation

- `--host` first matches a configured Docker host by target ID, name, hostname, or Docker SSH host. If no config matches, legacy device lookup is used.
- `--list` shows only running containers. `--containers` shows all containers including stopped.
- `--logs --tail N` limits output to the last N lines (default 100).
- `--logs --timestamps` prefixes each line with the RFC3339 timestamp.
- Container names in Docker are usually the container-friendly name set at `docker run --name`, not the image name.
- `--inspect` returns detailed information including environment variables, command, mounts, networks, and port mappings.
- `--stats` returns a one-shot CPU percentage and memory usage snapshot.

## Dashboard plugin

The Dashboard plugin provides the main landing page at `/` showing fleet health overview.

### Web UI

- `/` — Dashboard: fleet health summary, service signal status, recent events, and quick actions

### Notes

- Dashboard is a read-only view; no CLI or API commands.
- It aggregates data from all enrolled devices and active plugins.

## Access plugin

The Access plugin manages SSH keys and access policies for managed devices.

### Web UI

- `/keys` — SSH key management for device access

### Notes

- Managed SSH requires the Access plugin and a successfully applied access policy.
- SSH keys must be configured before devices can be accessed via Insylus.

## Agent plugin

The Agent plugin provides configuration interface for the Insylus agent running on devices.

### Web UI

- `/agent/settings` — agent configuration and status

### Notes

- Agent settings control how devices report inventory and services.
- Changes to agent settings take effect on the device's next check-in.

## Topology plugin

The Topology plugin provides a visual map of the homelab network.

### Web UI

- `/topology` — interactive topology map (web only, not CLI or API)

### Notes

- Topology is visualization only; no interactive controls via CLI.
- The map shows relationships between devices but does not support direct management actions.

## Update plugin

The Update plugin manages Insylus controller updates.

### Web UI

- `/update` — controller update management interface

### API endpoints

```bash
GET  /api/update/status
POST /api/update/check
POST /api/update/apply
```

### Notes

- Update management is available via both web UI and API.
- The update plugin requires sufficient permissions to manage server software.
