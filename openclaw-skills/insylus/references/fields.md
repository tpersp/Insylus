# Insylus Field Reference

Complete reference for all fields returned by Insylus CLI and API.

## Device views

### Compact (`--compact`, default for `devices --json`)

```bash
insylusctl devices --json
curl http://127.0.0.1:8080/api/devices
```

Fields: `name`, `hostname`, `ips`, `purpose`

### Info (`--info`, default for `devices --find` and `--id`)

```bash
insylusctl devices --find <value> --json
curl "http://127.0.0.1:8080/api/devices/find?q=<value>"
curl http://127.0.0.1:8080/api/devices/<device-id>
```

Adds: `id`, `device_mode`, `managed_account_enabled`, `managed_user`, `access_mode`, `enforcement_succeeded`, `ssh_alias`, `ssh_command`, `agent_version`, `last_seen_at`, `device_type`, `purpose`, `parent_state`, `parent_name`, `children`, `wol`

### Full (`--full`)

```bash
insylusctl devices --find <value> --json --full
curl "http://127.0.0.1:8080/api/devices/find?q=<value>&view=full"
curl http://127.0.0.1:8080/api/devices/<device-id>?view=full
```

Adds: `workloads`, `discovery_warnings`, `health` (uptime, load, memory, disk), `agent_auto_update`

---

## Device fields

### `id`
Stable internal device identifier. Use `--id` or `--find` for lookup.

### `name`
Operator-assigned friendly name.

### `hostname`
Reported hostname from the remote agent.

### `ips`
Reported IP list. First item is typically the primary LAN address.

### `device_mode`
- `inventory-only` — Insylus observes only; does not manage SSH, users, keys, or sudoers.
- `access-managed` — Insylus enforces managed SSH accounts.

### `managed_account_enabled`
Whether the managed account should be enabled on the device.

### `managed_user`
Linux account name currently managed by the controller policy. New installs default to `bob`; existing accounts are linked and missing accounts are created by the agent when access is enabled.

### `managed_groups`
Internal policy groups derived from `access_mode`; operators choose an access level instead of choosing Linux groups directly.

### `managed_password`
Agent policy only. When configured, Insylus sets the managed user's local password on enabled `access-managed` devices. It is not exposed in inventory output.

### `access_mode`
- `disabled` — managed account is locked or unavailable.
- `audit` — SSH works with read-only audit/log access.
- `docker` — audit access plus Docker group access.
- `sudo_prompted` — sudo requires a password prompt.
- `sudo_passwordless` — sudo does not require a password prompt.

### `ssh_alias`
Preferred SSH alias for the controller host. Use: `ssh <ssh_alias>`. Empty when `device_mode=inventory-only` or `access_mode=disabled`.

### `ssh_command`
Ready-to-run command, usually `ssh <ssh_alias>`. Empty when SSH is not available.

### `enforcement_succeeded`
Whether the last policy apply cycle succeeded. `false` means the device is not yet in the desired state.

### `error_message`
Last reported enforcement error, if any.

### `agent_version`
Installed Insylus agent version on the device.

### `agent_auto_update`
Nested object: `enabled`, `override`, `update_available`, `server_agent_version`, `status`, `error`.

### `last_seen_at`
Last agent check-in time. Devices are considered online if within the last 45 seconds.

### `device_type`
Primary topology type: `vm`, `bare-metal`, `lxc`, `container`, etc.

### `purpose`
Secondary role label. Examples: `docker-host`, `proxmox-node`, `OpenClaw host`, `Coding server`.

### `parent_state`
- `linked` — device has a linked parent
- `unknown` — parent relationship unknown
- `none` — no parent

### `parent_name`
Human-friendly parent name when `parent_state=linked`.

### `children`
List of linked enrolled child devices. Present in info and full views.

### `workloads`
Discovered services, containers, VMs, or LXCs running on this device. Present in full view.

### `discovery_warnings`
Non-fatal issues from discovery collectors. Present in full view.

### `health`
Nested object in info/full view. Includes `hostname`, `os_name`, `ips`, `agent_version`. In full view also includes `uptime`, `load_average`, `memory_used`, `disk_used`.

### `wol`
Wake-on-LAN object: `enabled` (device supports WoL), `ready` (recently seen so skip WoL). WoL is only sent when `wol.enabled=true` and the device has not been seen recently.

---

## Service views

### Compact (`--compact`, default for `services --json`)

```bash
insylusctl services --json
curl http://127.0.0.1:8080/api/services
```

Groups duplicate service names. Fields: `name`, `count`, `healthy`, `unhealthy`, `missing`, `kinds`, `last_seen_at`

### Info (`--info`, default for `services --find` and `services --list --device`)

```bash
insylusctl services --find <name> --json
insylusctl services --list --device <device> --json
curl "http://127.0.0.1:8080/api/services/find?q=<value>"
curl "http://127.0.0.1:8080/api/services?device=<device>"
```

One record per service instance. Fields: `name`, `device`, `kind`, `state`, `health`, `image`, `endpoints`, `last_seen_at`, `missing_since`

### Full (`--full`)

```bash
insylusctl services --find <name> --json --full
insylusctl services --list --device <device> --json --full
curl "http://127.0.0.1:8080/api/services/find?q=<value>&view=full"
```

Adds: `id`, `normalized_name`, `discovered_state`, `first_seen_at`, `last_reported_at`

---

## Service fields

### `name`
Service display name. Grouped by name in compact view.

### `device`
Device name hosting this instance.

### `kind`
Service kind: `systemd`, `docker`, `proxmox-vm`, `proxmox-lxc`, etc.

### `state`
Instance state. Values: `running`, `stopped`, `exited`, `dead`, `failed`, `missing`.

### `health`
Health classification: `healthy`, `unhealthy`, `missing`, `unknown`.

### `image`
Container image or service binary (for docker and some systemd services).

### `endpoints`
Network endpoints reported by discovery (e.g., IP:port pairs).

### `last_seen_at`
Last report time from the hosting device.

### `missing_since`
When this instance became `missing` (absent from latest device report). Absent when state is not `missing`.

### `count`
In compact view: number of instances with this name across all devices.

### `kinds`
In compact view: list of kinds present for this service name.

---

## Service state meanings

| State | Meaning |
|-------|---------|
| `healthy` | Running/up/active, or reported with no state |
| `unhealthy` | Stopped/exited/dead/failed/error |
| `missing` | Previously discovered, absent from latest device report |
| `unknown` | Reported but Insylus cannot classify the state |

- Stopped Docker containers: `unhealthy`
- Deleted Docker containers: `missing` (prune in web UI at `/services`)
- Previously seen services remain `missing` until re-discovered or deliberately pruned

---

## Wake

```bash
insylusctl wake DEVICE [--json]
```

Sends a Wake-on-LAN magic packet for the device. Only sends when `wol.enabled=true`. Returns `already online` for recently seen devices.

JSON output: `{"status":"already_online"}` or `{"status":"sent"}`.

---

## API endpoints summary

```
GET  /api/devices?view=compact|info|full
GET  /api/devices/find?q=<value>&view=compact|info|full
GET  /api/devices/<device-id>?view=compact|info|full
POST /api/devices/<device-id>/wake
GET  /api/services?view=compact|info|full
GET  /api/services/find?q=<value>&view=compact|info|full
GET  /api/services?device=<device>&view=compact|info|full
POST /api/devices/<device-id>/wake
```
