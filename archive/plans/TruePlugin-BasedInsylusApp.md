# True Plugin-Based Insylus App

---

**IMPLEMENTED** — `internal/pluginhost` contract, `plugins/registry/registry_gen.go`, all plugin folders (`devices`, `services`, `access`, `agent`, `wake`, `topology`, `help`, `jellyfin`, `proxmox`) using `host.CLI()`, `host.API()`, `host.Web()`, `host.Migrations()`. Server startup loads plugins via registry. Template/static asset serving under `/plugin-assets/<plugin-id>/`. Topology is web-only (`/topology`, `/topology/graph`) with `/api/topology` returning 404.

## Summary

Refactor Insylus into a compile-time, folder-based plugin app. Each feature lives under top-level `/plugins/<feature>/` and may register CLI commands, public API routes, web UI routes, navigation items, templates, static assets, migrations, and shared feature services. Adding/removing a plugin folder plus regenerating the registry and rebuilding changes the CLI command list, API routes, and web UI automatically.

Topology remains a **web-only plugin**: no `insylusctl topology` and no public `/api/topology`.

## Key Architecture

- Add `internal/pluginhost` as the only cross-surface plugin contract:
  - `Plugin { ID() string; Name() string; Register(Host) error }`
  - `Host` exposes `CLI()`, `API()`, `Web()`, `Data()`, and `Migrations()`.
  - `APIHost` and `WebHost` wrap `http.ServeMux` route registration.
  - `WebHost` also supports `NavItem`, plugin-owned templates, and namespaced static assets.
  - `Data()` exposes narrow service interfaces, not the concrete server `Store`.

- Create top-level plugin folders:
  - `/plugins/devices`: device CLI, `/api/devices*`, home/device web pages, device note/topology override UI hooks.
  - `/plugins/services`: services CLI, `/api/services*`, services/history web pages.
  - `/plugins/access`: keys/settings/policy/device mode/agent auto-update web/API behavior.
  - `/plugins/agent`: bootstrap, checkin, policy fetch, report, install page/script, agent downloads.
  - `/plugins/wake`: wake CLI plus web/API wake actions.
  - `/plugins/topology`: `/topology` web page and UI-private `/topology/graph` only.
  - `/plugins/help`: CLI help and plugin list commands.
  - `/plugins/proxmox` now owns Proxmox CLI/API/web integration and user-provided token storage.

- Add generated registry:
  - Generator scans `/plugins/*` for plugin packages exposing `New() pluginhost.Plugin`.
  - Generator writes `plugins/registry/registry_gen.go`.
  - `cmd/insylusctl` and `internal/server` both load `registry.Plugins()`.
  - Plugins are always enabled if compiled into the registry; no runtime enable/disable config in v1.

## Implementation Changes

- Replace `cmd/plugins` with top-level `/plugins`.
  - Move current CLI plugin code into the matching `/plugins/<feature>` package.
  - Remove hardcoded CLI usage text from `cmd/insylusctl/main.go`; root help is generated from registered CLI commands.
  - `insylusctl plugins` lists registered plugins, not only commands.

- Refactor server startup around the plugin host.
  - `internal/server.App` creates a plugin host, registers all plugins, applies migrations, parses shared and plugin templates, then builds the HTTP handler.
  - Replace `internal/server/route_plugins.go` with plugin registration through `/plugins/...`.
  - Keep `/static/*` for shared assets; serve plugin assets under `/plugin-assets/<plugin-id>/...`.

- Split data access behind host services.
  - Define focused interfaces such as `DeviceStore`, `ServiceStore`, `AccessStore`, `AgentStore`, and `TopologyStore`.
  - Existing `Store` implements these interfaces.
  - Plugins use interfaces from `internal/pluginhost` or `internal/server/services`, not the concrete app internals.

- Plugin-owned web UI.
  - Shared layout remains in the base server.
  - Header nav is generated from registered `WebNavItem`s.
  - Plugin templates are embedded in each plugin and registered during plugin startup.
  - Topology plugin owns `topology.html`, `topology.js`, and `/topology/graph`.

- Plugin migrations.
  - Plugins may register ordered migrations.
  - Core schema migration runner executes base migrations first, then plugin migrations sorted by plugin ID and migration version.
  - Existing schema statements move into base/core plus feature-owned migrations where practical; do not rewrite historical tables unnecessarily.

- Update plans/docs.
  - Rewrite `plans/PluginArchitecturePlan.md` to describe the true `/plugins/<feature>` model.
  - Keep `AGENT_GUIDE.md` and OpenClaw docs aligned: topology is web-only; structured lookup remains devices/services.

## Test Plan

- Unit/build:
  - `go test ./...`
  - `go build ./cmd/insylusctl`
  - `go build ./cmd/insylus-server`

- Registry:
  - Generator includes every valid `/plugins/*` folder.
  - Removing a plugin from the registry removes its CLI commands, nav items, routes, templates, and migrations from the built app.
  - Invalid plugin folder fails generation with a clear error.

- CLI:
  - `insylusctl --help` is generated from registered CLI commands.
  - `insylusctl plugins` lists CLI-capable plugins only; web/API-only plugins are not shown there.
  - Existing `devices`, `services`, and `wake` behavior remains compatible.
  - `insylusctl topology` remains unknown.

- API/web:
  - `/api/devices*`, `/api/services*`, wake API, agent endpoints, install/download endpoints still pass existing tests.
  - `/topology` still loads.
  - `/topology/graph` feeds the web map.
  - `/api/topology` returns 404.
  - Header nav reflects registered web plugins.

- Deployment verification:
  - Rebuild release artifacts as normal user.
  - Run installer and restart `insylus.service`.
  - Verify `insylusctl devices`, `insylusctl services`, `insylusctl plugins`, `/`, `/services`, `/topology`, and `/api/topology` behavior.
  - Confirm no root-owned files under `/opt/insylus`.

## Assumptions

- Plugins are compile-time Go source plugins, not runtime-loaded `.so` or external-process plugins.
- A rebuild is acceptable after adding/removing plugin folders.
- Plugins are always enabled when present in the generated registry.
- Topology is web UI only.
- Proxmox is implemented as `/plugins/proxmox`; Proxmox API tokens remain user-created and user-provided.
