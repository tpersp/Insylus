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
insylusctl proxmox --node <name-or-host-or-ip> --list [--json]
insylusctl proxmox --node <name-or-host-or-ip> --vms [--json]
insylusctl proxmox --node <name-or-host-or-ip> --lxcs [--json]
insylusctl proxmox --node <name-or-host-or-ip> --info <name-or-vmid> [--json]
insylusctl proxmox --node <name-or-host-or-ip> --status <name-or-vmid> [--json]
```

Power actions:

```bash
insylusctl proxmox --node <name-or-host-or-ip> --start <name-or-vmid> [--json]
insylusctl proxmox --node <name-or-host-or-ip> --stop <name-or-vmid> [--json]
insylusctl proxmox --node <name-or-host-or-ip> --restart <name-or-vmid> [--json]
```

Node status:

```bash
insylusctl proxmox --node <name-or-host-or-ip> --node-status [--json]
insylusctl proxmox --node <name-or-host-or-ip> --cluster-status [--json]
```

Token management:

```bash
insylusctl proxmox set-token --node <device> --token-id <id> --token-secret <secret> [--node-name <name>] [--api-url <url>] [--tls-insecure]
insylusctl proxmox list-tokens [--json]
insylusctl proxmox remove-token --node <device>
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