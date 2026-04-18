# Insylus v2 Output Shape Plan: Ultra-Compact Defaults with `--find` Lookup

---

**IMPLEMENTED** — The tiered output profile system (compact/info/full views), the `view` query parameter, `find` endpoint, `wake` endpoint, and service view profiles are all implemented.

## Summary

Insylus should shrink default structured output to the minimum needed for scan-and-select workflows, while still offering richer tiers for inspection when explicitly requested. The goal is to make both the inventory API and CLI safe for AI agents and manageable for operators working with 20 to 30 devices.

The chosen model is:
- ultra-compact defaults
- tiered presets instead of many low-level toggles
- one shared output-profile system used by both CLI and API
- `--find` becomes the preferred lookup path
- `--id` remains for compatibility
- table output remains human-oriented and does not become JSON-like

Compact mode should exclude heavy or noisy fields such as `workloads`, `children`, `discovery_warnings`, full health stats, most access/policy details, and most topology detail unless explicitly requested.

Whole-homelab topology is exposed through the `/topology` web UI only. Do not add graph-wide topology fields to compact device inventory or expose topology as a public CLI/API lookup surface.

Proxmox output is feature-scoped under the Proxmox plugin and does not change compact device inventory. Use `insylusctl proxmox --json` or `/api/proxmox/...` for Proxmox-specific VM/LXC, node, cluster, and token metadata.

## Key Changes

### Output profiles

Define three response profiles for inventory responses:

- `compact`
  - Intended default for list/scan workflows
  - Default for:
    - `GET /api/devices`
    - `insylusctl devices --json`
  - Includes only:
    - `name`
    - `hostname`
    - `ips`
    - `purpose`
  - Excludes everything else, including:
    - `id`
    - `ssh_alias`
    - `ssh_command`
    - `last_seen_at`
    - `agent_version`
    - topology fields
    - access/policy fields
    - health
    - `workloads`
    - `children`
    - `discovery_warnings`
    - `note`

- `info`
  - Mid-detail profile for normal single-device inspection
  - Default for:
    - `GET /api/devices/{id}`
    - `GET /api/devices/find?q=...`
    - `insylusctl devices --find VALUE --json`
    - `insylusctl devices --id DEVICE_ID --json`
  - Includes:
    - `id`
    - `name`
    - `hostname`
    - `ips`
    - `ssh_alias`
    - `ssh_command`
    - `os_name`
    - `last_seen_at`
    - `agent_version`
    - `device_type`
    - `device_type_source`
    - `purpose`
    - `purpose_source`
    - `platform_class`
    - `parent_device_id`
    - `parent_name`
    - `parent_state`
    - `parent_source`
    - `note`
    - `enforcement_succeeded`
    - `error_message`
    - `access_mode`
    - `aia_enabled`
    - `assigned_key_name`
    - `assigned_fingerprint`
    - `policy_revision`
    - `applied_revision`
    - `topology_last_updated_at`
    - `children`
  - Includes a reduced health summary object:
    - `hostname`
    - `os_name`
    - `ips`
    - `agent_version`
  - Excludes:
    - `workloads`
    - `discovery_warnings`
    - uptime/load/memory/disk health details

- `full`
  - Intended for deep inspection
  - Includes the current full `DeviceInventoryItem` behavior
  - Includes:
    - all existing fields
    - full `health`
    - `workloads`
    - `children`
    - `discovery_warnings`

### API changes

Add a shared `view` query parameter for inventory endpoints:

- `GET /api/devices?view=compact|info|full`
- `GET /api/devices/{id}?view=compact|info|full`

Add a preferred find endpoint:
- `GET /api/devices/find?q=<value>&view=compact|info|full`

Add a wake endpoint:
- `POST /api/devices/{id}/wake`

Proxmox plugin endpoints are separate from the device inventory profile system:

- `GET /api/proxmox/nodes`
- `POST /api/proxmox/tokens`
- `POST /api/proxmox/tokens/delete/<device-id>`
- `GET /api/proxmox/<device-id>/guests`
- `GET /api/proxmox/<device-id>/vms`
- `GET /api/proxmox/<device-id>/lxcs`
- `GET /api/proxmox/<device-id>/status/<name-or-vmid>`
- `POST /api/proxmox/<device-id>/start/<name-or-vmid>`
- `POST /api/proxmox/<device-id>/stop/<name-or-vmid>`
- `POST /api/proxmox/<device-id>/restart/<name-or-vmid>`
- `GET /api/proxmox/<device-id>/node-status`
- `GET /api/proxmox/<device-id>/cluster-status`

Behavior:
- `/api/devices` default `view=compact`
- `/api/devices/{id}` default `view=info`
- `/api/devices/find` default `view=info`
- `/api/devices/{id}/wake` returns `status="already_online"` for recently seen devices; otherwise it sends a Wake-on-LAN magic packet only when inventory reports `wol.enabled=true`
- invalid `view` returns `400`
- omit query param uses the endpoint default
- API response shape must be deterministic per view
- omitted fields should not appear as zero-value placeholders unless naturally part of the reduced struct
- implementation should use dedicated API view structs rather than reusing the full internal struct with ad hoc omission logic

Recommended API response structs:
- `DeviceInventoryCompact`
- `DeviceInventoryInfo`
- existing `DeviceInventoryItem` remains the full internal/full-wire shape
- `DeviceFindConflict` or equivalent for ambiguous `find` results
- `DeviceInventoryInfo` and full `DeviceInventoryItem` include `wol`; compact remains limited to `name`, `hostname`, `ips`, and `purpose`

Topology map:
- `/topology` is a human-facing web UI feature
- topology JSON used by the page should remain UI-private, not part of the public `/api` surface
- manual topology data must not alter `/api/devices` compact/info/full output

Separate service index endpoints:
- `GET /api/services?view=compact|info|full`
- `GET /api/services/find?q=<value>&view=compact|info|full`
- `GET /api/services?device=<device-find-value>&view=compact|info|full`
- `/api/services` default `view=compact`
- `/api/services/find` default `view=info`
- `/api/services?device=...` default `view=info`
- includes discovered systemd services, Docker containers, Proxmox VMs, and Proxmox LXCs
- previously discovered services remain persisted after they disappear and are marked `missing`

Service output profiles:
- `compact` groups instances by normalized service name and returns `name`, `count`, `healthy`, `unhealthy`, `missing`, `kinds`, and `last_seen_at`
- `info` returns matching service instances with hosting device summary, kind, state, health, image, endpoints, last seen, and missing-since state
- `full` adds stable service IDs, normalized names, discovered state, first seen, and last reported timestamps

Find behavior:
- `q` may match by:
  - exact case-insensitive `name`
  - exact case-insensitive `hostname`
  - exact IP match against any reported IP
  - exact ID match
- ambiguous match returns `409 Conflict`
- conflict response should include a short candidate list with enough identity data to disambiguate
- no match returns `404`

### CLI changes

Keep the current human table output for `insylusctl devices` as the default non-JSON behavior.

Add tiered JSON/profile flags:

- `insylusctl devices --json`
  - defaults to `compact`
- `insylusctl devices --find VALUE --json`
  - defaults to `info`
- `insylusctl devices --id DEVICE_ID --json`
  - defaults to `info`
- `insylusctl devices --view compact|info|full`
- `insylusctl wake VALUE`
  - resolves VALUE with the same device find semantics
  - reports when the device is already online
  - sends WOL only when `wol.enabled=true`
- convenience aliases:
  - `--compact`
  - `--info`
  - `--full`

Rules:
- `--find` and `--id` are mutually exclusive
- `--compact`, `--info`, and `--full` are mutually exclusive aliases for `--view`
- `--json` is required for structured JSON output profiles
- table mode ignores `--view` and still renders the existing compact human table
- `--find` becomes the preferred lookup interface in help text and docs
- `--id` remains supported for compatibility

CLI help text should clearly state:
- list JSON defaults to compact
- `--find` supports name, hostname, IP, or ID
- single-device JSON defaults to info
- richer output requires `--full`

Service CLI:
- `insylusctl services` behaves like `insylusctl services --list`
- `insylusctl services --json` returns the compact grouped service index
- `insylusctl services --find jellyfin --json` returns matching service instances
- `insylusctl services --list --device docker01 --json` returns services hosted by one device
- `--find` and `--list` are mutually exclusive
- `--device` filters by the same exact device find rules as `insylusctl devices --find`
- service find uses case-insensitive substring matching against service name and image

Topology CLI:
- no `insylusctl topology` command; topology is web UI only

### Server implementation approach

Introduce a small projection layer between stored inventory and wire output.

Implementation behavior:
- server still computes one full internal inventory record per device
- API handlers project that full record into one of the view structs before encoding
- projection logic is centralized in one place so CLI and API stay aligned on profile semantics
- avoid embedding business rules in the CLI; the API should remain authoritative for shaping response profiles

Recommended additions:
- inventory projection helpers such as:
  - `InventoryCompactFromRecord`
  - `InventoryInfoFromRecord`
  - existing full projection remains for `full`
- find/match helpers shared by CLI-backed API lookups
- API handlers read `view`
- CLI appends `?view=` to the API request when `--json` is used

### Compatibility and documentation

Compatibility policy:
- non-JSON CLI table behavior stays recognizable
- JSON/API defaults change intentionally to become smaller
- scripts or tools that need current rich behavior must switch to `view=full` or `--full`
- `--id` remains available, but `--find` is the preferred path going forward

Documentation updates should cover:
- new `view` query param
- new `find` endpoint
- new CLI flags
- matching rules for `--find`
- what each profile includes
- recommendation for AI agents:
  - use compact for selection
  - use `--find` for targeted lookup
  - use info for decision support
  - use full only when workload/detail inspection is needed

## Test Plan

API tests:
- `/api/devices` defaults to compact
- `/api/devices/{id}` defaults to info
- `/api/devices/find?q=MiscServer` defaults to info
- `view=info` returns info fields and excludes heavy fields
- `view=full` preserves workload and full health fields
- invalid `view` returns `400`
- ambiguous find returns `409`
- missing find result returns `404`
- `/api/services` defaults to compact grouped service output with duplicate counts
- `/api/services/find?q=jellyfin` returns all matching instances and devices
- `/api/services?device=docker01` returns only that device's service instances
- `/api/services?view=full` includes service timing/history fields
- missing services remain visible with `health="missing"`

CLI tests:
- `insylusctl devices --json` requests compact view
- `insylusctl devices --find MiscServer --json` requests info view
- `insylusctl devices --find 10.10.10.22 --json` resolves correctly
- `insylusctl devices --find cbYaSh6UZjjEnU0J --json` resolves correctly
- `insylusctl devices --json --full` requests full view
- `--id` works with all three views
- conflicting view flags return a usage error
- `--find` and `--id` together return a usage error
- `insylusctl services` prints the service index table
- `insylusctl services --find jellyfin` prints instance rows
- `insylusctl services --list --device docker01 --json` requests the device-filtered info view

Projection tests:
- compact projection only includes `name`, `hostname`, `ips`, and `purpose`
- info projection includes topology/access summary without workloads
- full projection matches current full behavior
- reduced structs do not leak omitted zero-value fields unintentionally

Acceptance scenarios:
- AI agent can list all devices with a very small payload
- AI agent can look up `MiscServer` directly without first discovering its ID
- operator can inspect one device by name, hostname, IP, or ID with one command
- user can request full topology/workload detail only when needed

## Assumptions and Defaults

- compact list output should be as small as possible and include only `name`, `hostname`, `ips`, and `purpose`
- list defaults to compact, while single-device lookup defaults to info
- tiered presets are preferred over many granular field flags
- `--find` becomes the preferred lookup interface
- `--id` remains for compatibility, not as the primary workflow
- exact matching is preferred over fuzzy matching in the first version of this redesign
- human table output remains default and unchanged in spirit
- full output remains available explicitly and preserves today’s richness
- future fields should be assigned intentionally to compact, info, or full rather than defaulting to every profile
