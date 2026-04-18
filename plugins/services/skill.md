## Services plugin

The services plugin is Insylus's service inventory. It discovers and tracks systemd services, Docker containers, Proxmox VMs, and Proxmox LXCs across all enrolled devices.

### Rules

- `--find` matches by service name or image (case-insensitive substring). `--list` shows all.
- `--device` filters to one hosting device — accepts device name, hostname, IP, or ID.
- `--find` and `--device` cannot be combined with `--list`.
- Compact view groups duplicates by name. Info and full views show one record per instance.

### CLI commands

```bash
insylusctl services [--json] [--compact|--info|--full]
insylusctl services --list [--json] [--device DEVICE] [--compact|--info|--full]
insylusctl services --find <name-or-image> [--json] [--compact|--info|--full]
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