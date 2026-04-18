# Insylus v2 Plan: Topology, Discovery, and Operator Notes

---

**IMPLEMENTED** — Device discovery snapshots, workloads, child candidates, parent-child inference, effective type/parent resolution with override precedence, device notes, platform class, and all core v2 features are implemented in the store and API. All observations/concerns items at the bottom are checked off and implemented.

## Summary

Insylus v2 expands the product from access control plus health reporting into homelab inventory with relationships. The v2 scope is the core homelab feature set: automatic discovery of device type, `systemd` services, Docker containers, and Proxmox VMs/LXCs; server-side inference of parent-child relationships; and a manual per-device note field in the web UI.

The implementation should use a hybrid authority model:
- discovery is authoritative for raw observations
- server inference is authoritative for computed relationships unless overridden
- manual overrides for `type` and `parent` are authoritative until cleared
- note is always manual and never touched by discovery

The product model should distinguish `parent=none` from `parent=unknown`:
- `none`: a device type that normally has no parent in this topology model
- `unknown`: a device type that usually has a parent, but the parent is not enrolled or not matched yet

## Key Changes

### Data model and persistence

Add topology and metadata entities to the server store and shared types.

New persistent device metadata:
- `device_note`: plain-text manual note stored per device
- `platform_class`: `raspberry-pi | generic-linux | unknown` for v2
- `effective_device_type`: resolved value used by API/UI
- `effective_parent_device_id`: resolved value used by API/UI
- `type_source`: `manual | inferred | discovered | unknown`
- `parent_source`: `manual | inferred | none | unknown`
- `type_override`: nullable manual override for type
- `parent_override_device_id`: nullable manual override for enrolled parent
- `parent_override_state`: `inherit | manual_device | manual_unknown | manual_none`

New discovery storage:
- `device_discovery_snapshots`: latest raw discovery report per device
- `discovered_workloads`: normalized rows for local workloads reported by the device
- `discovered_child_candidates`: normalized rows for children reported by parent-capable devices
- `relationship_candidates`: inferred server-side matches with confidence and match reason

New enums and concepts:
- `device_type`: `bare-metal | vm | lxc | container | proxmox-node | docker-host | unknown`
- `platform_class`: includes `raspberry-pi` as a platform/hardware classification, not as the primary topology type
- `parent_state`: effective presentation derived from resolved parent relation: linked device, `unknown`, or `none`

The existing access-control schema remains intact in v2.

### Agent discovery behavior

Extend the agent check-in/report flow to collect and submit topology discovery data in addition to health.

Self classification rules:
- detect virtualization/container facts from local Linux signals
- infer `container`, `lxc`, `vm`, or `bare-metal` from local runtime evidence
- upgrade to `docker-host` if Docker is installed and active
- upgrade to `proxmox-node` if Proxmox host tooling is present and usable
- if multiple roles apply, use the most topology-significant host role for `device_type`
  - `proxmox-node` wins over `docker-host`
  - `docker-host` wins over plain `bare-metal`
  - guest/container types stay `vm`, `lxc`, or `container`

Platform classification rules:
- classify Raspberry Pi from hardware model or device-tree information
- otherwise use `generic-linux` unless a stronger platform class is added later

Local workload discovery scope:
- running `systemd` services considered meaningful for inventory
- running Docker containers with container name, image, and published ports if available
- Proxmox child guests from `qm list` and `pct list` on a Proxmox node

Parent-capable discovery:
- Proxmox nodes report child guest candidates
- Docker hosts report child container candidates
- plain VMs/LXCs/containers do not report parent relationships; they only report self facts

Discovery payload additions:
- resolved self `device_type`
- resolved `platform_class`
- observed workloads
- observed child candidates
- optional published network endpoints for workloads
- discovery timestamp and agent version

The agent must treat all discovery collectors as best-effort:
- failed collectors do not fail policy enforcement
- collector errors are returned as discovery warnings/status, not as fatal access-control failures

### Server inference and matching

Add a topology resolution pass on every discovery update.

Matching policy for parent-child inference:
- first-class v2 match signal is `hostname + IP`
- parent child candidate hostname must case-insensitively match an enrolled device hostname or friendly-name-derived alias candidate
- at least one child candidate IP must match one of the enrolled device IPs for a strong match
- hostname-only matches may be stored as low-confidence candidates but must not auto-link in v2
- ports are not part of device identity matching in v2; they may be stored for workload display only

Inference rules:
- if a Proxmox node reports child `miscserver` with IP `10.0.0.5` and an enrolled device reports hostname `miscserver` plus IP `10.0.0.5`, auto-link parent-child
- if a Docker host reports containers, those children are shown as workloads on the host; they are not promoted to enrolled-device relationships unless Insylus later supports enrolling containers as first-class devices
- if a device self-identifies as `vm`, `lxc`, or `container` and no enrolled parent matches, effective parent state is `unknown`
- if a device self-identifies as `bare-metal`, `docker-host`, or `proxmox-node` and no parent evidence exists, effective parent state is `none`

Authority and precedence:
- manual type override wins over discovered type
- manual parent override wins over inferred parent
- `manual_unknown` and `manual_none` explicitly block inference updates
- clearing an override returns the field to discovery/inference control
- note is always manual
- inferred children are recomputed from authoritative effective parent links; there is no manual child editing in v2

Resolution metadata exposed by API/UI:
- effective type
- type source
- effective parent label/value
- parent source
- last discovery time
- match confidence/reason when inferred

### API, CLI, and UI surfaces

Expand the shared inventory model and API responses.

Add to device inventory/detail responses:
- `device_type`
- `device_type_source`
- `platform_class`
- `parent_device_id`
- `parent_name`
- `parent_state`
- `parent_source`
- `note`
- `workloads`
- `discovery_status` or `discovery_warnings`
- `children` for detail view
- `topology_last_updated_at`

New API endpoints or POST actions:
- update device note
- set/clear type override
- set parent override to an enrolled device
- set parent override to `unknown`
- set parent override to `none`
- clear parent override
- trigger re-detection for topology fields on next agent sync or clear overrides immediately and wait for fresh discovery

CLI behavior:
- `insylusctl devices` table gains compact topology columns such as `TYPE` and `PARENT`
- `insylusctl devices --json` includes full topology payload
- no topology mutation commands are required in v2; mutation remains web-UI driven

Web UI updates:
- home page device table adds `Type` and `Parent`
- device detail page adds a topology panel with:
  - effective type and source
  - platform class
  - effective parent and source
  - child devices if any
  - discovered workloads
  - note editor
- device detail page adds override controls:
  - set type override
  - set parent override
  - mark parent as `Unknown`
  - mark parent as `None`
  - clear overrides / return to auto mode
- UI copy must clearly distinguish:
  - `Parent: Alpha-pve`
  - `Parent: Unknown`
  - `Parent: None`
- child workload examples should render cleanly:
  - `Children: Jellyseerr, qBittorrent`
  - `Runs: docker, media stack, nginx` as applicable

## Test Plan

Server/store tests:
- migration creates new topology and metadata tables
- effective type/parent resolution honors precedence rules
- manual overrides survive later discovery updates
- clearing overrides returns fields to inferred/discovered values
- strong match requires hostname plus IP
- hostname-only candidate is stored but not auto-linked
- bare-metal-like types resolve to `parent=none`
- guest-like types resolve to `parent=unknown` when unmatched

API tests:
- inventory list and detail include new topology fields
- note update persists and returns correctly
- override endpoints update effective values and source metadata
- clear/re-detect flow resets manual authority correctly

Agent tests:
- self type detection for bare metal, VM, LXC/containerized host where feasible with mocks
- Raspberry Pi platform classification
- Docker host discovery returns running containers
- Proxmox node discovery returns VM/LXC child candidates
- collector failures do not break policy application/reporting

UI tests or acceptance scenarios:
- enrolled VM with no enrolled parent shows `Type: VM`, `Parent: Unknown`
- enrolled Proxmox node with enrolled child auto-links and child shows `Parent: Alpha-pve`
- bare-metal Raspberry Pi host shows `Type: bare-metal` or `docker-host` as applicable and `Platform: raspberry-pi`
- operator sets manual parent override and later discovery does not overwrite it
- operator clears override and next discovery restores inferred parent
- note can be edited without affecting topology state

## Assumptions and Defaults

- v2 scope is limited to core homelab discovery: `systemd`, Docker, and Proxmox; Podman and libvirt are deferred
- note is plain text in v2, single per device, intended for concise operator guidance
- parent-child device inference in v2 only auto-links on `hostname + IP`; ports are stored only as workload/network metadata
- Docker containers are workloads in v2, not first-class enrolled child devices
- child relationship presentation is derived from effective parent links, not manually maintained lists
- manual override support in v2 covers `type`, `parent`, and note only
- existing access-control behavior and API compatibility remain intact unless a field extension is required for topology


## Observations and Concerns
- mild concern would be the Proxmox node self-detection — qm list and pct list require Proxmox permissions.
    - Solution:
    Detect Proxmox node purpose from Proxmox host markers such as `/etc/pve`, `pveversion`, or installed `qm`/`pct` tooling, instead of requiring `qm list` or `pct list` to succeed just to classify the host as a Proxmox node. Guest discovery remains best-effort, and permission or command failures should surface as discovery warnings rather than suppressing node detection entirely.
    [x]: Implemented.
- Output gets really cluttered and long when including services like this.. and there's not-useful services mentioned as well, such as default linux services.. 
    - Solution:
    Reduce service noise by filtering out common base OS services such as `systemd-*`, `dbus`, `cron`, `rsyslog`, `polkit`, `getty`, `fwupd`, and similar default background services. Keep the more useful operator-facing services such as custom app services, `docker`, `ssh`, `qemu-guest-agent`, and `insylus-agent`. Also cap the number of discovered services shown so the inventory stays readable.
    [x]: Implemented.
    - Follow-up:
    This can still be refined more. Both linux servers and raspberry pi's have services shown that is not useful at a glance, such as: Pi:accounts-daemon,avahi-daemon,bluetooth,controller,lightdm,NetworkManager,serial-getty@ttyS0,ssh(If it didnt run we would definetly know, no need to show it here.). Linux:ssh,unattended-upgrade,upower,udisks2,containerd(?, I dont know whwat that is). Proxmox:there's a lot, but i dont know what is useful..  .
      - Solution: 
      Extended the default service-noise filter to hide `accounts-daemon`, `avahi-daemon`, `bluetooth`, `containerd`, `controller`, `lightdm`, `NetworkManager`, `serial-getty@*`, `ssh`, `udisks2`, `unattended-upgrades`, and `upower`. Keep `docker`, `insylus-agent`, `qemu-guest-agent`, and custom app services visible.
      [x]: Implemented.
- we need, if possible, to have the agent service on devices auto-update if the agent service release by the server is newer than the running agent service on devices, it is too many devices to ssh into and manually update the agent service... and possibly make it a user input choice like "enable auto-update for agent-service? - y/n?". then surface in a field something like "Agent-service Auto Update: true/false", and surface that in the web ui and CLI/API amongst the rest of device info.
    - Solution:
    Implemented opt-in agent auto-update with:
    - global default plus per-device override
    - disabled by default
    - checksum-verified architecture-specific downloads
    - atomic replacement of `/usr/local/bin/insylus-agent`
    - clean self-restart through the existing `Restart=always` service behavior
    - `agent_auto_update` status in API/CLI info/full output
    - web UI controls for global default and per-device override
    [x]: Implemented.
- My AI agent got stuck because of massive JSON output as the default from the CLI/API commands. We should reduce the default JSON output drastically and make more flags to gradually expand the needed JSON. Hide bulky fields from default output unless a richer view is explicitly requested. Prefer a compact default over a noisy one.
    - Solution:
    Implement ultra-compact default structured output and add graduated expansion paths:
    - `insylusctl devices --json` and `GET /api/devices` default to `compact`, returning only:
      - `name`
      - `hostname`
      - `ips`
    - `insylusctl devices --find <value> --json`, `GET /api/devices/{id}`, and `GET /api/devices/find?q=...` default to `info`
    - `--full` and `view=full` return full health, workloads, warnings, children, and remaining detail
    - add `--find` as the preferred lookup path so the operator or AI can look up a device by name, hostname, IP, or ID without first fetching and extracting an ID
    - keep `--id` for compatibility, but not as the preferred workflow
    - make view shaping server-side so CLI and API stay aligned
    [x]: Implemented.
