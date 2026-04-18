## Devices plugin

The devices plugin is Insylus's core device inventory. It lists all enrolled devices, shows their status, and provides device lookup.

### Rules

- `--find VALUE` matches by device name, hostname, exact IP, or ID — use it instead of `--id` when possible.
- Without `--json`, output is a human-readable table. With `--json`, output is structured JSON.
- `--compact`, `--info`, and `--full` select the JSON view depth and are mutually exclusive.
- Devices without `ssh_alias` are not ready for Insylus-managed SSH access.

### CLI commands

```bash
insylusctl devices [--json] [--find VALUE | --id DEVICE_ID] [--compact|--info|--full]
insylusctl devices
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

- `/` — device list
