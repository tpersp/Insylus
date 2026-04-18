# Insylus True Plugin Architecture Plan

---

**IMPLEMENTED** — Compile-time folder-based plugin architecture with `plugins/registry/registry_gen.go`, `pluginhost` contract, and all listed plugins (devices, services, wake, access, agent, topology, help, jellyfin, proxmox). Topology is web-only with `/api/topology` returning 404 and `insylusctl topology` unknown.

## Summary

Insylus is moving to a compile-time, folder-based plugin app. Each feature lives under top-level `/plugins/<feature>/` and may register CLI commands, public API routes, web UI routes, navigation items, templates, static assets, migrations, and feature services.

Adding or removing a plugin means adding/removing its folder, regenerating `plugins/registry/registry_gen.go`, rebuilding, and redeploying.

Topology is web UI only. Do not add `insylusctl topology` or `/api/topology`.

## Current Shape

Core plugin contract:

```text
internal/pluginhost/
  pluginhost.go     # Plugin, Host, CLI/API/Web/Data/Migration contracts
  hosts.go          # CLI-only and disabled host helpers
```

Plugin registry:

```text
plugins/registry/registry_gen.go
scripts/generate-plugin-registry.sh
```

Feature plugins:

```text
plugins/
  devices/          # device CLI, API routes, home/device web templates
  services/         # service CLI, API routes, services/history templates
  wake/             # wake CLI, web wake, API wake
  access/           # keys, settings, policy/mode/update web actions
  agent/            # bootstrap/checkin/policy/report/install/download routes
  topology/         # /topology web page and UI-private /topology/graph
  help/             # CLI help and plugin listing
```

Shared app shell:

- `cmd/insylusctl` loads `registry.Plugins()` and registers CLI-capable plugins.
- `internal/server.App` loads `registry.Plugins()` and registers web/API/static/template surfaces.
- Shared web layout remains in `internal/server/templates/layout.html`.
- Plugin nav is dynamic through `pluginNavItems`.
- Shared static assets stay under `/static/*`.
- Plugin assets are served under `/plugin-assets/<plugin-id>/*`.

## Plugin Contract

Each plugin package exposes:

```go
func New() pluginhost.Plugin
```

Each plugin implements:

```go
type Plugin interface {
    ID() string
    Name() string
    Register(pluginhost.Host) error
}
```

Available host surfaces:

- `CLI()`: register commands and plugin metadata.
- `API()`: register public API routes.
- `Web()`: register web routes, nav, templates, and static assets.
- `Data()`: access narrow handler/service sets exposed by the base app.
- `Migrations()`: register plugin migrations.

Plugins must not use `init()` self-registration. The registry is generated.

## Adding A Plugin

1. Create `/plugins/<id>/plugin.go`.
2. Implement `New() pluginhost.Plugin`.
3. Register only the surfaces the feature needs:
   - CLI command through `host.CLI()`
   - API routes through `host.API()`
   - Web routes/nav/templates/static through `host.Web()`
   - migrations through `host.Migrations()`
4. Run:

```bash
scripts/generate-plugin-registry.sh
gofmt -w plugins/registry/registry_gen.go
go test ./...
```

5. Rebuild and redeploy through the normal Insylus flow.

Proxmox is implemented as `/plugins/proxmox`, not under `cmd/` or `internal/server/plugins/`. The plugin owns the CLI command, API routes, `/proxmox` setup page, and token-table migration. Proxmox API tokens are always created by the user in Proxmox and then registered with Insylus.

## Invariants

- Topology stays web-only:
  - `/topology` renders the page.
  - `/topology/graph` is UI-private for the page JavaScript.
  - `/api/topology` returns 404.
  - `insylusctl topology` is unknown.
- Public structured lookup remains `devices` and `services`.
- Plugins are compile-time Go source plugins, not runtime `.so` plugins or external processes.
- Plugins are always enabled when compiled into the generated registry.
- Existing SQLite schema remains compatibility-preserving; plugin migrations are additive.

## Verification

Primary checks:

```bash
go test ./...
go build ./cmd/insylusctl
go build ./cmd/insylus-server
insylusctl plugins
insylusctl devices --json
insylusctl services --json
curl -fsS http://127.0.0.1:8080/topology
curl -fsS http://127.0.0.1:8080/topology/graph
curl -sS -o /tmp/topology-api.txt -w '%{http_code}\n' http://127.0.0.1:8080/api/topology
```

Expected:

- `insylusctl plugins` lists only plugins that register CLI commands; web/API-only plugins such as `topology`, `access`, and `agent` are compiled app plugins but are hidden from CLI plugin listing.
- `insylusctl topology` exits as an unknown command.
- `/api/topology` returns `404`.
- `/topology` and `/topology/graph` continue to support the web map.
