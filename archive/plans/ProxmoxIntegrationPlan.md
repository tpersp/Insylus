# Insylus Proxmox Integration Plan

---

**IMPLEMENTED** — All 5 phases implemented:
- Phase 1: ProxmoxClient with ListVMs/ListLXCs/GetVMStatus/GetLXCStatus, `/api/proxmox/{device-id}/vms` and `/lxcs`
- Phase 2: StartVM/StopVM/StartLXC/StopLXC, `/api/proxmox/{device-id}/start/{vmid}` and `/stop/{vmid}`
- Phase 3: GetNodeStatus/GetClusterResources, `insylusctl proxmox --node-status` and `--cluster-status`
- Phase 4: Token management UI (`/proxmox`), CLI (`set-token`, `list-tokens`, `remove-token`), and API endpoints (`/api/proxmox/tokens`)
- Phase 5: Token auto-provisioning not done (explicit user-created tokens only), Proxmox node detection via discovery

---

## Summary

Add first-class Proxmox cluster management to Insylus via the REST API. This enables querying and controlling VMs/LXCs on Proxmox nodes without requiring `pct`/`qm` CLI access or `www-data` group membership — the core issue that blocked CLI-based access earlier.

**Use case:** Aia needs to check VM status, start/stop containers, get resource usage, and perform maintenance tasks on Proxmox nodes — all via `insylusctl proxmox` commands that Insylus routes through the Proxmox API.

## Why Proxmox API Instead of CLI

The `pct`/`qm` commands communicate with `pmxcfs` (the Proxmox cluster filesystem) via IPC sockets. This requires:
- The `www-data` group membership (UID 1000 is rejected: `connection from bad user 1000! - rejected`)
- Direct filesystem access to `/var/run/pve`

The Proxmox REST API (port 8006) avoids all of this:
- Token-based authentication — no OS user permissions needed
- Accessible from any device with network access to the node
- Returns the same information as CLI tools
- Well-documented at `https://pve.proxmox.com/wiki/API_Documentation`

## Architecture

```
Aia (CLI)
    ↓
insylusctl proxmox --node beta-pve --list
    ↓
Insylus Server (routes_api.go)
    ↓
Proxmox REST API (https://10.10.10.30:8006/api2/json/)
    ↓
Proxmox Node (beta-pve, omega-pve, alpha-pve)
```

## What Needs to Be Built

### 1. Proxmox Device Enrollment

Currently Proxmox nodes (Alpha-pve, Beta-pve, Omega-pve) are enrolled in Insylus as `proxmox-node` device type but:
- They don't have a Proxmox API token stored
- Insylus doesn't know their Proxmox node names (e.g., `beta-pve` in Proxmox terms)
- There's no way to target them for API calls

**What to add:**
- A new `proxmox_node_name` field on device records (the Proxmox-internal node name, e.g., `beta-pve`)
- A new `proxmox_api_token_id` field for the token user@pam!tokenname
- A new `proxmox_api_token_secret` field for the token value
- API endpoints to set/manage these per device

### 2. Proxmox API Client (`internal/server/proxmox.go`)

A new Go package that:
- Connects to a Proxmox node's REST API using stored credentials
- Handles authentication via API tokens
- Makes API calls: list VMs/LXCs, get status, start/stop, etc.
- Returns parsed Go structs

**Key API endpoints to implement:**
```
GET  /api2/json/nodes/{node}/qemu              → List VMs
GET  /api2/json/nodes/{node}/lxc              → List LXCs
GET  /api2/json/nodes/{node}/qemu/{vmid}/status/current  → VM status
GET  /api2/json/nodes/{node}/lxc/{vmid}/status/current   → LXC status
POST /api2/json/nodes/{node}/qemu/{vmid}/status/start    → Start VM
POST /api2/json/nodes/{node}/qemu/{vmid}/status/stop     → Stop VM
POST /api2/json/nodes/{node}/qemu/{vmid}/status/reboot   → Reboot VM
POST /api2/json/nodes/{node}/lxc/{vmid}/status/start    → Start LXC
POST /api2/json/nodes/{node}/lxc/{vmid}/status/stop     → Stop LXC
POST /api2/json/nodes/{node}/lxc/{vmid}/status/reboot   → Reboot LXC
GET  /api2/json/nodes/{node}/status           → Node status (CPU, RAM, etc.)
GET  /api2/json/cluster/resources             → Cluster-wide resource usage
```

### 3. New insylusctl Command (`cmd/insylusctl/main.go`)

Add a `proxmox` subcommand with the following structure:

```bash
insylusctl proxmox --node <node-name> --list                    # List all VMs and LXCs
insylusctl proxmox --node <node-name> --vms                     # List only VMs
insylusctl proxmox --node <node-name> --lxcs                    # List only LXCs
insylusctl proxmox --node <node-name> --info <name-or-vmid>     # Get VM/LXC details
insylusctl proxmox --node <node-name> --status <name-or-vmid>  # Get status (running/stopped)
insylusctl proxmox --node <node-name> --start <name-or-vmid>   # Start VM/LXC
insylusctl proxmox --node <node-name> --stop <name-or-vmid>    # Stop VM/LXC
insylusctl proxmox --node <node-name> --restart <name-or-vmid> # Restart VM/LXC
insylusctl proxmox --node <node-name> --node-status            # Node CPU/RAM/disk status
insylusctl proxmox --node <node-name> --cluster-status         # Cluster-wide resources
insylusctl proxmox --node <node-name> --json                    # JSON output
insylusctl proxmox --node <node-name> --full                   # Full details
```

**Finding VMs/LXCs by name:**
- Insylus can look up the device to find its Proxmox context
- If user says `--info jellyfin`, Insylus queries `insylusctl services --find jellyfin` first to find which device it's on, then queries that node's Proxmox API

### 4. New API Routes (`internal/server/routes_api.go`)

Add Proxmox-specific endpoints:

```
GET  /api/proxmox/nodes                         # List enrolled Proxmox nodes
GET  /api/proxmox/{device-id}/vms              # List VMs on a node
GET  /api/proxmox/{device-id}/lxcs             # List LXCs on a node
GET  /api/proxmox/{device-id}/status/{vmid}    # Get VM or LXC status
POST /api/proxmox/{device-id}/start/{vmid}     # Start VM/LXC
POST /api/proxmox/{device-id}/stop/{vmid}     # Stop VM/LXC
GET  /api/proxmox/{device-id}/node-status      # Node resource status
GET  /api/proxmox/{device-id}/cluster-status   # Cluster resources
```

### 5. Token Management

**Setup workflow:**
1. User creates a Proxmox API token in the web UI: **Datacenter → Permissions → API Tokens**
   - User: `aia@pam` (or an existing user)
   - Token ID: e.g., `insylus-agent`
   - Privilege separation: assign a role with minimal permissions (PVEAuditor for read-only, PVEAdmin for full)
2. User registers the token in Insylus via new UI or CLI:
   ```bash
   insylusctl proxmox set-token --node beta-pve \
     --token-id "aia@pam!insylus-agent" \
     --token-secret "xxxx-xxxx-xxxx"
   ```
3. Insylus stores the token securely (see Secret Storage below)

**Minimal recommended token permissions:**
- **Read-only:** Role `PVEAuditor` — can list VMs/LXCs, view status, node info
- **Full control:** Role `PVEAdmin` — also start/stop/create/delete VMs

For Aia's use case (monitoring + occasional start/stop), `PVEAuditor` + separate start/stop role is ideal.

### 6. Secret Storage

Insylus currently manages SSH keys via:
- `/etc/ssh/ssh_config.d/insylus.conf` (managed SSH config)
- `/var/lib/insylus/insylus.db` (stores key metadata, not raw secrets)

For Proxmox tokens, there are two options:

**Option A: Extend the existing SSH key store**
- Add columns to the existing `api_tokens` table
- Store encrypted token secrets alongside SSH key metadata
- Use the same access control (sudo access to Insylus)

**Option B: Store in the Insylus SQLite database directly**
- New `proxmox_tokens` table with: `device_id`, `token_id`, `token_secret` (encrypted), `role`, `created_at`
- Encryption key derived from the Insylus server's local configuration
- Simpler but requires care with the encryption key

**Recommendation:** Option A, extending existing secrets infrastructure, for consistency. The managed SSH pattern in `managed_ssh.go` shows how Insylus handles external service credentials — follow that pattern.

## Data Model Changes

### New Fields on Device Record

In `internal/shared/types.go` or the store:

```go
type ProxmoxToken struct {
    DeviceID       string    // Which device this token is for
    TokenID        string    // e.g., "aia@pam!insylus-agent"
    TokenSecret    string    // Encrypted token value
    Role           string    // e.g., "PVEAuditor", "PVEAdmin"
    NodeName       string    // Proxmox-internal node name (e.g., "beta-pve")
    CreatedAt      time.Time
}

type DeviceProxmoxInfo struct {
    NodeName string `json:"node_name,omitempty"`
    TokenID  string `json:"token_id,omitempty"`
    HasToken bool   `json:"has_token"`
}
```

### Store Changes (`internal/server/store.go`)

- Add `ProxmoxToken` table: `device_id`, `token_id`, `token_secret_encrypted`, `role`, `node_name`, `created_at`
- Add methods: `GetProxmoxToken(deviceID)`, `SetProxmoxToken(deviceID, token)`, `DeleteProxmoxToken(deviceID)`
- Add `ProxmoxNodeName` field to device record

## Implementation Phases

### Phase 1: Read-Only Proxmox Access (MVP)

**Goal:** List VMs/LXCs and view status on any enrolled Proxmox node.

1. Add `proxmox_node_name` field to device records
2. Create `internal/server/proxmox/client.go` with:
   - `NewProxmoxClient(nodeURL, tokenID, tokenSecret) *Client`
   - `ListVMs(node string) ([]VM, error)`
   - `ListLXCs(node string) ([]LXC, error)`
   - `GetVMStatus(node string, vmid int) (*VMStatus, error)`
   - `GetLXCStatus(node string, vmid int) (*LXCStatus, error)`
3. Add `GET /api/proxmox/{device-id}/*` endpoints
4. Add `insylusctl proxmox --list`, `--vms`, `--lxcs`, `--status` commands
5. Document token setup in AGENT_GUIDE.md

### Phase 2: VM/LXC Control

**Goal:** Start, stop, restart VMs and LXCs.

1. Add to Proxmox client:
   - `StartVM(node string, vmid int) error`
   - `StopVM(node string, vmid int) error`
   - `StartLXC(node string, vmid int) error`
   - `StopLXC(node string, vmid int) error`
2. Add `POST /api/proxmox/{device-id}/start/{vmid}`
3. Add `POST /api/proxmox/{device-id}/stop/{vmid}`
4. Add `insylusctl proxmox --start`, `--stop`, `--restart` commands
5. Keep power operations non-interactive so AI agents can use them; command intent and permissions are the safety boundary.

### Phase 3: Node and Cluster Status

**Goal:** Get node CPU/RAM/disk and cluster-wide resource info.

1. Add to Proxmox client:
   - `GetNodeStatus(node string) (*NodeStatus, error)`
   - `GetClusterResources() ([]ClusterResource, error)`
2. Add `insylusctl proxmox --node-status`, `--cluster-status`
3. Add to existing `insylusctl devices --full` output if Proxmox node

### Phase 4: Token Management UI and CLI

**Goal:** Allow setting/getting/listing Proxmox tokens through Insylus.

1. Add CLI commands:
   ```bash
   insylusctl proxmox set-token --node beta-pve --token-id "aia@pam!insylus" --token-secret "xxx"
   insylusctl proxmox list-tokens
   insylusctl proxmox remove-token --node beta-pve
   ```
2. Add web UI page at `/proxmox-tokens` for token management
3. Add `/api/proxmox/tokens` endpoints

### Phase 5: Discovery Context Without Token Auto-Provisioning

**Goal:** Proxmox nodes enrolled via Insylus agent can be recognized as Proxmox nodes, while API tokens remain explicitly user-created and user-provided.

- Agent already reports `device_type: "proxmox-node"`.
- Discovery may help identify likely Proxmox nodes and suggest a default node name.
- Insylus must not create Proxmox API tokens automatically.
- Insylus must not grant itself Proxmox API permissions automatically.
- The `/proxmox` web UI should show clear guidance for token permissions instead of trying to provision a token.
- The user creates the token in Proxmox and then registers the token ID/secret with Insylus.

## Shared Output Types

```go
// ProxmoxVM represents a Proxmox QEMU VM
type ProxmoxVM struct {
    VMID      int    `json:"vmid"`
    Name      string `json:"name"`
    Status    string `json:"status"`    // "running", "stopped", "paused"
    CPU       float  `json:"cpu"`
    Memory    uint64 `json:"memory"`
    MaxMemory uint64 `json:"max_memory"`
    DiskUsed  uint64 `json:"disk_used"`
    DiskTotal uint64 `json:"disk_total"`
    Uptime    int    `json:"uptime"`    // seconds
}

// ProxmoxLXC represents a Proxmox LXC container
type ProxmoxLXC struct {
    VMID      int    `json:"vmid"`
    Name      string `json:"name"`
    Status    string `json:"status"`
    CPU       float  `json:"cpu"`
    Memory    uint64 `json:"memory"`
    MaxMemory uint64 `json:"max_memory"`
    DiskUsed  uint64 `json:"disk_used"`
    DiskTotal uint64 `json:"disk_total"`
    Uptime    int    `json:"uptime"`
}

// ProxmoxNodeStatus represents node-level resource usage
type ProxmoxNodeStatus struct {
    Node       string  `json:"node"`
    CPU        float64 `json:"cpu_usage"`       // 0.0-1.0
    MemoryUsed uint64  `json:"memory_used"`
    MemoryTotal uint64 `json:"memory_total"`
    DiskUsed   uint64  `json:"disk_used"`
    DiskTotal  uint64  `json:"disk_total"`
    Uptime     int     `json:"uptime"`
}

// ProxmoxClusterResource represents a cluster-wide resource
type ProxmoxClusterResource struct {
    Type      string  `json:"type"`      // "node", "vm", "storage"
    ID        string  `json:"id"`
    Node      string  `json:"node"`
    Status    string  `json:"status"`
    CPU       float64 `json:"cpu"`
    Memory    uint64  `json:"memory"`
    MaxMemory uint64  `json:"max_memory"`
    Disk      uint64  `json:"disk"`
    Uptime    int     `json:"uptime"`
}
```

## CLI Command Examples

### List all VMs and LXCs on beta-pve

```bash
insylusctl proxmox --node beta-pve --list
# Output:
# VMID   NAME        TYPE    STATUS    CPU     MEM         DISK        UPTIME
# 200    jellyfin    qemu    running   0.12    4.0 GiB     50 GiB      1234567s
# 201    windows     qemu    stopped   0.00    8.0 GiB     120 GiB     -
# LXCID  NAME        TYPE    STATUS    CPU     MEM         DISK        UPTIME
# 100    jellyseerr  lxc     running   0.08    2.0 GiB     10 GiB      1234567s
# 101    qbittorrent lxc     running   0.15    3.0 GiB     20 GiB      1234567s
```

### Get detailed info on a VM by name

```bash
insylusctl proxmox --node beta-pve --info jellyfin --json
```

### Stop and start a VM

```bash
insylusctl proxmox --node beta-pve --stop jellyfin
insylusctl proxmox --node beta-pve --start jellyfin
```

### Get node resource usage

```bash
insylusctl proxmox --node beta-pve --node-status
# Output:
# NODE      CPU       MEMORY          DISK           UPTIME
# beta-pve  0.23      16.0 / 32.0 GiB 500G / 1.0 TiB  46d 17h
```

## Error Handling

**Token not configured:**
```
Error: No Proxmox API token configured for beta-pve
Hint: Run: insylusctl proxmox set-token --node beta-pve --token-id "aia@pam!insylus" --token-secret "your-secret"
```

**Node not reachable:**
```
Error: Cannot connect to beta-pve (10.10.10.30:8006): connection refused
Hint: Check that the Proxmox web interface is running on port 8006
```

**VM/LXC not found:**
```
Error: No VM or LXC named "nonexistent" found on beta-pve
Hint: Run: insylusctl proxmox --node beta-pve --list
```

**Permission denied:**
```
Error: Proxmox API returned permission denied (403)
Hint: Ensure the API token has appropriate permissions (PVEAuditor or PVEAdmin)
```

## Testing Plan

### Store Tests
- Token insert/retrieve/delete for a device
- Token lookup by device ID
- Encryption/decryption roundtrip

### Proxmox Client Tests (mocked)
- Parse VM list JSON response
- Parse LXC list JSON response  
- Parse status responses (running, stopped)
- Handle API errors (403, 404, 500)
- Token authentication header construction

### API Endpoint Tests
- `GET /api/proxmox/{device-id}/vms` returns VM list
- `POST /api/proxmox/{device-id}/start/{vmid}` returns 200 on success
- `GET /api/proxmox/{device-id}/vms` returns 400 when no token configured
- `GET /api/proxmox/{device-id}/vms` returns 404 when device is not a Proxmox node

### CLI Integration Tests
- `insylusctl proxmox --node beta-pve --list` parses and prints table
- `insylusctl proxmox --node beta-pve --list --json` outputs valid JSON
- `insylusctl proxmox --node beta-pve --start jellyfin` sends correct API call
- `insylusctl proxmox --node beta-pve --info nonexistent` shows helpful error

### Manual Verification
- Configure a token on beta-pve via Proxmox UI
- Register token: `insylusctl proxmox set-token --node beta-pve --token-id "aia@pam!insylus" --token-secret "xxx"`
- List VMs: `insylusctl proxmox --node beta-pve --list`
- Start a stopped VM: `insylusctl proxmox --node beta-pve --start 201`
- Stop a running VM: `insylusctl proxmox --node beta-pve --stop 200`
- Get node status: `insylusctl proxmox --node beta-pve --node-status`

## Documentation Updates

### AGENT_GUIDE.md
Add section on Proxmox integration:

```markdown
## Proxmox Node Management

Insylus can query and control Proxmox VMs and LXCs via the Proxmox REST API.

### Requirements
- Proxmox node enrolled in Insylus inventory
- API token created in Proxmox UI (Datacenter → Permissions → API Tokens)
- Token registered with Insylus via `insylusctl proxmox set-token`

### Commands

List VMs and LXCs on a node:
```bash
insylusctl proxmox --node beta-pve --list
```

Get detailed info on a VM or LXC:
```bash
insylusctl proxmox --node beta-pve --info jellyfin --json
```

Start or stop a VM/LXC:
```bash
insylusctl proxmox --node beta-pve --start jellyfin
insylusctl proxmox --node beta-pve --stop jellyfin
```

Node resource status:
```bash
insylusctl proxmox --node beta-pve --node-status
```

### Token Setup

1. In Proxmox web UI, create an API token:
   - User: aia@pam (or your managed user)
   - Token ID: e.g., insylus-agent
   - Role: PVEAuditor for read-only queries, or a narrower custom role including `VM.PowerMgmt` for start/stop/restart. PVEAdmin works but is broad.
   - Keep privilege separation enabled unless the user intentionally wants the token to inherit the user's full permissions.

2. Register the token with Insylus:
   ```bash
   insylusctl proxmox set-token --node beta-pve \
     --token-id "aia@pam!insylus-agent" \
     --token-secret "your-token-secret"
   ```

3. Verify connectivity:
   ```bash
   insylusctl proxmox --node beta-pve --list
   ```
```

### Insylusv2OutputShapePlan.md
Add Proxmox output shapes to the output shape documentation.

## Open Questions / Future Considerations

1. **Proxmox cluster vs standalone:** Should commands work across the cluster (i.e., ask any node about any other node's VMs) or only query the specific node a VM lives on? The API allows cluster-wide queries via `/api2/json/cluster/...` endpoints.

2. **VM/LXC creation/deletion:** Not in scope for now, but the API supports it. Would need more careful permission design.

3. **Multiple Proxmox clusters:** If Doden ever has separate clusters, the current device-per-node model handles it naturally — just enroll each node.

4. **Snapshot support:** Proxmox has snapshot APIs. Could be a future Phase 6.

5. **Token rotation:** No built-in mechanism. User would need to update the token in Insylus if they regenerate it in Proxmox.

6. **Which node to use for cluster queries:** When querying cluster-wide resources (`/api2/json/cluster/resources`), any node in the cluster can answer. Should Insylus pick the first available, or let the user specify?

## File Changes Summary

| File | Change |
|------|--------|
| `plugins/proxmox/plugin.go` | Own Proxmox CLI command, API routes, web route, and plugin migration |
| `plugins/proxmox/templates/proxmox.html` | Token setup UI and permission guidance |
| `internal/shared/types.go` | Add `ProxmoxGuest`, `ProxmoxNodeStatus`, `ProxmoxClusterResource`, `ProxmoxToken`, `DeviceProxmoxInfo` types |
| `internal/server/proxmox_store.go` | Add encrypted token persistence and Proxmox node lookup methods |
| `internal/server/proxmox_client.go` | Proxmox REST API client |
| `internal/server/proxmox_handlers.go` | Proxmox API/web handlers exposed through plugin host |
| `internal/pluginhost/pluginhost.go` | Add focused Proxmox handler surface |
| `AGENT_GUIDE.md` | Add Proxmox section |
| `Insylusv2OutputShapePlan.md` | Add Proxmox shapes |
| `openclaw-skills/insylus/SKILL.md` | Update to mention Proxmox commands |

## Estimated Effort

- **Phase 1 (Read-only):** ~2-3 days
- **Phase 2 (Control):** ~1-2 days  
- **Phase 3 (Node/Cluster status):** ~1 day
- **Phase 4 (Token management):** ~1-2 days
- **Phase 5 (discovery context, no token auto-provisioning):** ~1-2 days

**Total:** ~6-10 days of development work, spread across the phases.

---

*Plan authored by Aia, 2026-04-13, based on codebase review of Insylus at `/opt/insylus/`*
