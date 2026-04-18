# Insylus Services Index

---

**IMPLEMENTED** — `service_instances` table, HTTP endpoints (`GET /api/services`, `/api/services/find`, device filtering), service health tracking, plugin-based Services UI page, and `insylusctl services` CLI command with `--list`, `--find`, `--device`, `--compact`, `--info`, `--full` flags are all implemented.

## Summary

Add a first-class `services` command and API backed by persisted workload history. In this feature, “service” means any discovered workload: systemd service, Docker container, Proxmox VM, or Proxmox LXC.

`insylusctl services` and `insylusctl services --list` will show a compact service index. `insylusctl services --find jellyfin` will show all matching service instances and the devices hosting them. `insylusctl services --list --device docker01` will show services on one device.

Previously seen services stay visible after they disappear from discovery and are marked `missing`/unhealthy until they are seen again.

## Key Changes

- Add a persisted `service_instances` SQLite table populated from `DeviceReport.Topology.Workloads`.
  - Key rows by `device_id + kind + normalized_name`.
  - Store display name, kind, image, state, endpoints JSON, first seen, last seen, missing since, last reported, and health.
  - On every device report, upsert currently reported workloads and mark previously known-but-not-reported workloads for that device as `missing`.
  - Do not delete service rows automatically.

- Add shared output types for compact/info/full service views.
  - Compact list item: `name`, `count`, `healthy`, `unhealthy`, `missing`, `kinds`.
  - Info instance: service identity plus `device` summary, `kind`, `state`, `health`, `image`, `endpoints`, `last_seen_at`, `missing_since`.
  - Full instance: info fields plus stable IDs, first/last report timestamps, raw discovered state, and enough device context for deep inspection.

- Add HTTP endpoints:
  - `GET /api/services?view=compact|info|full`
  - `GET /api/services/find?q=<value>&view=compact|info|full`
  - `GET /api/services?device=<device-find-value>&view=compact|info|full`
  - Defaults: `/api/services` uses `compact`; `/api/services/find` uses `info`; device-filtered lists use `info`.

- Matching behavior:
  - Service search is case-insensitive substring matching against service name and image.
  - Device filter reuses existing device find semantics: name, hostname, IP, or ID; ambiguous device matches return `409` with conflict details.
  - Service grouping for `--list` is case-insensitive by normalized service name, so duplicate `jellyfin` instances display as `jellyfin (2)`.

- Health rules:
  - `healthy`: current report contains the service and state indicates running/up/active, or state is empty for a discovered current workload.
  - `unhealthy`: current report contains the service but state indicates stopped/exited/dead/failed/error.
  - `missing`: service was previously seen on that device but is absent from the latest report.
  - `unknown`: current state is present but cannot be classified.

- Add CLI command:
  - `insylusctl services [--server URL] [--json] [--list] [--find VALUE] [--device VALUE] [--compact|--info|--full]`
  - `insylusctl services` behaves like `--list`.
  - `--find` and `--list` are mutually exclusive.
  - `--device` can combine with `--list` or no explicit mode.
  - Non-JSON tables:
    - index table: `SERVICE COUNT HEALTH KINDS LAST SEEN`
    - instance table: `SERVICE KIND DEVICE HEALTH STATE LAST SEEN ENDPOINTS`
  - JSON prints the selected API view with indentation.

- Update docs:
  - Add service command/API examples to `AGENT_GUIDE.md`.
  - Add service output shape notes to `Insylusv2OutputShapePlan.md`.
  - Update `openclaw-skills/insylus` so agents prefer `insylusctl services --find <name> --json` for service lookup.

## Test Plan

- Store tests:
  - Service rows are created from reported workloads.
  - Re-reporting the same service updates state/endpoints without duplicating rows.
  - Missing services are retained and marked `missing`.
  - A later report containing the service clears `missing_since` and restores current health.

- API tests:
  - `GET /api/services` returns compact grouped output with duplicate counts.
  - `GET /api/services/find?q=jellyfin` returns all matching devices/services.
  - `GET /api/services?device=docker01` returns only that device’s services.
  - Ambiguous device filters return `409`.
  - `view=full` includes persisted timing/history fields.

- CLI/manual verification:
  - `go test ./...`
  - `insylusctl services`
  - `insylusctl services --list --json`
  - `insylusctl services --find jellyfin`
  - `insylusctl services --list --device docker01 --json`
  - After implementation affecting the running app, rebuild/redeploy using the project’s normal non-sudo Go build flow and restart `insylus.service`.

## Assumptions

- This plan adds the services feature to the current codebase; it does not require creating a git branch unless requested separately.
- No web UI changes are included for v1 of services search.
- Existing agent discovery remains the source of truth; the agent does not need a protocol change because `TopologyDiscovery.Workloads` already carries services, containers, VMs, and LXCs.
- Compact device output remains unchanged.
