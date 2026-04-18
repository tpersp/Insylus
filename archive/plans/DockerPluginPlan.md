# Docker Manager Plugin — Archived Implementation Plan

Archived 2026-04-18. The Docker plugin has been implemented under
`plugins/docker`, registered in the generated plugin registry, documented in
`plugins/docker/skill.md`, and verified with `go test ./...`.

## Context

Docker hosts are already discovered and reported via the services discovery system, but Insylus currently offers no container-level control. Operators must SSH manually to run `docker` commands. This plugin adds container lifecycle control, inspection, and image management as a first-class Insylus feature.

This is a new feature. The rough idea was captured in `plans/PluginIdeas.md`.

---

## What the Plugin Owns

1. **Container lifecycle** — start, stop, restart, pause, unpause
2. **Container inspection** — logs, stats, port mappings, environment variables, mounts
3. **Image listing** — `docker images` with disk usage
4. **Compose awareness** — detect compose project name and linked containers when `docker-compose` is present

The plugin **does not** own discovery — the services discovery already reports running containers. This plugin adds *control and inspection*, not discovery.

---

## Plugin Structure

```
plugins/docker/
  plugin.go          — Plugin struct, New(), ID(), Name(), Register()
  commands.go        — CLI command definitions and Run handlers
  handlers.go        — HTTP handlers (API and web)
  client.go          — Docker CLI wrapper (runs commands over SSH)
  store.go           — DB operations (optional, for future state caching)
  types.go           — Request/response types
  formatter.go       — CLI table output helpers
  skill.md           — OpenClaw skill contribution
  templates/
    docker.html      — Container list and control page
    container.html   — Container detail page
  static/
    docker.js        — Browser interactivity
```

---

## Implementation Details

### Communication Model

Unlike the Proxmox plugin which calls a REST API directly, Docker management is done via **SSH** to the enrolled device, running local `docker` and `docker-compose` commands. This means:

- The plugin connects to the device via Insylus-managed SSH (using the `ssh_command` or `ssh_alias` already known from inventory)
- Each Docker operation runs a short-lived SSH session, executes the command, and returns
- No Docker daemon API port needs to be exposed; the plugin uses the same CLI an operator would

This aligns with how Insylus already handles device access — via the managed account and SSH key.

### CLI Commands

```
insylusctl docker --host <device> --list
insylusctl docker --host <device> --containers [--json]
insylusctl docker --host <device> --images [--json]
insylusctl docker --host <device> --inspect <container> [--json]
insylusctl docker --host <device> --logs <container> [--tail N] [--json]
insylusctl docker --host <device> --stats <container> [--json]
insylusctl docker --host <device> --start <container>
insylusctl docker --host <device> --stop <container>
insylusctl docker --host <device> --restart <container>
insylusctl docker --host <device> --pause <container>
insylusctl docker --host <device> --unpause <container>
```

Flags:
- `--host <device>` — device name, hostname, IP, or ID (resolved via `host.Data().Inventory()`)
- `--json` — emit JSON instead of human-readable table
- `--tail N` — for logs, number of lines to show (default 100)
- `--follow` — for logs, stream output (blocks like `docker logs -f`)

### API Routes

```
GET  /api/docker/nodes                          — list Docker hosts (enrolled devices with docker CLI)
GET  /api/docker/{device_id}/containers        — list containers
GET  /api/docker/{device_id}/containers/{name}/logs
GET  /api/docker/{device_id}/containers/{name}/stats
GET  /api/docker/{device_id}/containers/{name}/inspect
POST /api/docker/{device_id}/containers/{name}/start
POST /api/docker/{device_id}/containers/{name}/stop
POST /api/docker/{device_id}/containers/{name}/restart
POST /api/docker/{device_id}/containers/{name}/pause
POST /api/docker/{device_id}/containers/{name}/unpause
GET  /api/docker/{device_id}/images
```

### Web UI

- Nav item: **Docker** at `/docker` (order ~50)
- Container list per device with start/stop/restart/pause/unpause buttons
- Container detail page with logs viewer, stats, port mappings, env vars
- Image list with disk usage

### Docker Client Wrapper (client.go)

The `DockerClient` struct wraps SSH command execution:

```go
type DockerClient struct {
    sshRunner func(ctx context.Context, cmd string) (string, error)
}

func (c *DockerClient) ListContainers(ctx context.Context) ([]Container, error)
func (c *DockerClient) InspectContainer(ctx context.Context, name string) (*ContainerDetail, error)
func (c *DockerClient) ContainerLogs(ctx context.Context, name string, tail int, follow bool) (io.Reader, error)
func (c *DockerClient) ContainerStats(ctx context.Context, name string) (*ContainerStats, error)
func (c *DockerClient) StartContainer(ctx context.Context, name string) error
func (c *DockerClient) StopContainer(ctx context.Context, name string) error
func (c *DockerClient) RestartContainer(ctx context.Context, name string) error
func (c *DockerClient) PauseContainer(ctx context.Context, name string) error
func (c *DockerClient) UnpauseContainer(ctx context.Context, name string) error
func (c *DockerClient) ListImages(ctx context.Context) ([]Image, error)
```

SSH execution uses the same approach as running commands on managed devices — resolve the device first, then execute over SSH. The `ssh` command output is parsed to construct typed responses.

### Type Shapes

```go
type Container struct {
    Name      string
    Image     string
    Status    string
    State     string // running, exited, paused
    Ports     string
    CreatedAt string
}

type ContainerDetail struct {
    Container
    ID       string
    Env      []string
    Cmd      []string
    Mounts   []string
    Networks []string
    Ports    []PortMapping
}

type PortMapping struct {
    HostIP   string
    HostPort string
    ContPort string
}

type ContainerStats struct {
    CPU    float64
    Memory MemoryStats
}

type MemoryStats struct {
    Used    uint64
    Limit   uint64
    Percent float64
}

type Image struct {
    Repository string
    Tag         string
    ID          string
    Size        uint64
    CreatedAt   string
}
```

---

## Discovery of Docker Hosts

The plugin must discover which enrolled devices run Docker. Two approaches:

1. **Lazy detection** — when a Docker command targets a device, check if `docker version` succeeds over SSH. If it fails, return an appropriate error.

2. **Pre-seeded list** — use `host.Data().Inventory()` to list all devices, then probe each one for Docker availability on first access.

For the MVP, approach 1 is sufficient: try `docker version` when a device is first queried and cache the result in memory for the session.

---

## Error Handling

- SSH connection failures → `http.StatusBadGateway` with descriptive message
- Container not found → `http.StatusNotFound`
- Docker not installed on device → clear error message suggesting the device may not have Docker
- Permission denied → `http.StatusForbidden`
- Action already in correct state (e.g., start a running container) → return success with informational message

---

## Migrations

The plugin does not require database migrations for the MVP. All state is fetched live from devices. Future enhancements (scheduled container restarts, config persistence) may add migrations later.

---

## Dependency on Existing Systems

- Uses `host.Data().Inventory()` for device lookup
- Uses existing SSH access infrastructure (same as Proxmox plugin)
- Does **not** add new feature-specific internals to `internal/server`
- Follows the plugin contract strictly via `pluginhost.Host`

---

## Verification

```bash
# Rebuild with new plugin
bash scripts/generate-plugin-registry.sh
go test ./...
go build -o /opt/insylus/dist/insylus-server ./cmd/insylus-server
go build -o /opt/insylus/dist/insylusctl ./cmd/insylusctl

# CLI verification
insylusctl docker --host docker01 --list
insylusctl docker --host docker01 --containers --json
insylusctl docker --host docker01 --logs jellyfin --tail 20

# API verification
curl http://127.0.0.1:8080/api/docker/nodes
curl http://127.0.0.1:8080/api/docker/<device-id>/containers
```

---

## Out of Scope

- Building and pushing images
- Multi-arch builds
- Registry authentication
- Container resource limits configuration
- Auto-restart policies
- Docker Swarm management
- Health check configuration
- Secrets management (use Docker configs/secrets directly)

These are advanced features that belong in a CI/CD pipeline or dedicated Docker management tool, not in an inventory-and-access control plane.

---

## Completion Checklist

- [x] Create `plugins/docker/` directory structure
- [x] Implement `plugin.go` with `New()`, `ID()`, `Name()`, `Register()`
- [x] Implement `types.go` with all request/response types
- [x] Implement `client.go` with `DockerClient` and SSH command execution
- [x] Implement `handlers.go` with API and web route handlers
- [x] Implement `commands.go` with CLI command definitions
- [x] Implement `formatter.go` with CLI table output
- [x] Add Docker web templates
- [x] Add `static/docker.js` for browser interactivity
- [x] Add `skill.md` for OpenClaw contribution
- [x] Register plugin in `plugins/registry/registry_gen.go`
- [x] Run `go fmt`, `go test ./...`
