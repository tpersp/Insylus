# How To Build An Insylus Plugin

This guide explains how to add a feature plugin to Insylus. It is written for both humans and AI coding agents.

Insylus plugins are in-process Go source plugins with runtime enablement. They are not Go `.so` plugins, external processes, or separately installed packages. A plugin is a normal Go package under `/opt/insylus/plugins/<plugin-id>/` that exposes:

```go
func New() pluginhost.Plugin
```

After adding or removing a plugin folder, regenerate the registry and rebuild the app. The rebuilt app knows the plugin exists, and the server registers compiled plugin surfaces at startup so runtime enablement can expose them immediately.

Runtime plugin management:

```bash
insylusctl plugins list
insylusctl plugins enable docker
insylusctl plugins disable access
insylusctl plugins profiles
insylusctl plugins apply-profile homelab
```

Disabling a plugin takes effect immediately for already-registered web routes, API routes, navigation items, and plugin static assets: those surfaces return `404` or disappear from navigation without a restart. Enabling a compiled plugin also takes effect immediately without a restart.

Core target APIs are always available at `/api/targets`. Feature plugins should attach to neutral targets rather than requiring the Devices plugin to be enabled.

Topology is the example of a web-only plugin: it lives under `plugins/topology`, but it must not register a CLI command and must not expose a public `/api/topology` API.

## Quick Start

Use this flow when adding a new plugin:

```bash
cd /opt/insylus
mkdir -p plugins/example
$EDITOR plugins/example/plugin.go
bash scripts/generate-plugin-registry.sh
go test ./...
go build -o /opt/insylus/dist/insylus-server ./cmd/insylus-server
go build -o /opt/insylus/dist/insylusctl ./cmd/insylusctl
```

If the change affects the running local app, also rebuild the normal release artifacts and redeploy as described in `AGENTS.md`.

Do not use `sudo` for `go build`.

## Plugin Folder Shape

A plugin should live at:

```text
plugins/<plugin-id>/
  plugin.go
  handlers.go
  store.go
  client.go
  types.go
  templates/
    *.html
  static/
    *
  formatter.go
  skill.md
  ...
```

Only `plugin.go` is required.

Common optional files:

- `skill.md`: OpenClaw skill contribution — content is injected into the insylus SKILL.md by `scripts/deploy.sh`.
- `templates/*.html`: plugin-owned web templates.
- `static/*`: plugin-owned browser assets.
- `formatter.go`: CLI table or text formatting helpers.
- `handlers.go`: plugin-owned HTTP handlers.
- `store.go`: plugin-owned DB access for plugin tables.
- `client.go`: plugin-owned external API client.
- `types.go`: plugin-owned request/response/storage types.
- `*_test.go`: plugin tests.

Plugin IDs should be short, lowercase, and URL/package friendly:

- Good: `proxmox`, `backups`, `dns`, `certificates`
- Avoid: `Proxmox`, `prox-mox!`, `my cool plugin`

The package name normally matches the folder name.

Self-contained means the plugin brings its own feature implementation and can start when its optional peers are absent. A new feature should not require new feature-specific files in `internal/server` or new feature-specific DTOs in `internal/shared`; use `host.Targets()` for neutral inventory and `host.Capabilities()` for optional cooperation.

## The Plugin Contract

Every plugin implements `pluginhost.Plugin` from `internal/pluginhost`:

```go
type Plugin interface {
	ID() string
	Name() string
	Register(Host) error
}
```

Minimal plugin:

```go
package example

import "insylus/internal/pluginhost"

type Plugin struct{}

func New() pluginhost.Plugin {
	return Plugin{}
}

func (Plugin) ID() string {
	return "example"
}

func (Plugin) Name() string {
	return "Example"
}

func (Plugin) Register(host pluginhost.Host) error {
	return nil
}
```

`ID()` is the stable machine name. Use it for registry identity, migration ownership, asset namespace, and any plugin-owned route namespace.

`Name()` is the human-facing name shown in plugin listings where relevant.

`Register(host)` is where the plugin attaches itself to available app surfaces.

## Host Surfaces

The `pluginhost.Host` exposes generic backbone surfaces:

```go
type Host interface {
	CLI() CLIHost
	API() RouteHost
	Web() WebHost
	Data() DataHost
	Migrations() MigrationHost
	DB() DBHost
	Secrets() SecretHost
}
```

Each surface can be enabled or disabled depending on where the plugin is being registered.

When `insylusctl` starts, only the CLI surface is enabled.

When `insylus-server` starts, API, web, data, migrations, DB, and secrets are enabled, while CLI is disabled.

Always check `Enabled()` before registering against a surface:

```go
func (Plugin) Register(host pluginhost.Host) error {
	if host.CLI().Enabled() {
		host.CLI().AddCommand(command())
	}
	if host.API().Enabled() {
		host.API().HandleFunc("GET /api/example", handleAPI)
	}
	if host.Web().Enabled() {
		host.Web().HandleFunc("GET /example", handlePage)
	}
	if host.Migrations().Enabled() {
		host.Migrations().Add(pluginhost.Migration{
			PluginID: "example",
			Version:  1,
			Name:     "create example table",
			SQL:      `create table if not exists example_items (id text primary key);`,
		})
	}
	return nil
}
```

Checking `Enabled()` keeps web-only plugins from leaking into CLI behavior and keeps CLI-only code from running during server startup.

The base app must stay feature-neutral. Do not add feature-specific host methods such as `host.Data().Proxmox()` or `host.Data().Backups()`. A plugin that needs backend behavior should bring its own handlers, client code, data types, and store wrapper inside `plugins/<plugin-id>`, using only generic host services.

## Registry Generation

The generated registry lives at:

```text
plugins/registry/registry_gen.go
```

Do not edit it by hand.

Regenerate it with:

```bash
bash scripts/generate-plugin-registry.sh
```

The generator scans:

```text
plugins/*/plugin.go
```

For each plugin folder, it expects the package to expose:

```go
func New() pluginhost.Plugin
```

After generation, both server and CLI load:

```go
registry.Plugins()
```

Removing a plugin from the registry removes that plugin from the rebuilt app. Runtime enablement controls whether a compiled plugin is visible and reachable at runtime.

## CLI Plugins

Register CLI commands only when the plugin has an actual command to expose.

Example:

```go
func (Plugin) Register(host pluginhost.Host) error {
	if host.CLI().Enabled() {
		host.CLI().AddCommand(command())
	}
	return nil
}
```

Command shape:

```go
func command() ctl.Command {
	return ctl.Command{
		Name:  "example",
		Usage: "example [--server URL] [--json]",
		Short: "Do the example thing",
		Long:  "Longer help text for the example command.",
		Examples: []string{
			"example",
			"example --json",
		},
		Help: PrintHelp,
		Run:  Run,
	}
}
```

CLI commands usually live in the same plugin package as the rest of the feature.

Use the existing CLI helpers where possible:

- `internal/api`: HTTP client helpers, JSON printing, error handling.
- `internal/ctl`: command model and command-name handling.
- `internal/finder`: device lookup helper.
- Existing formatter patterns in `plugins/devices` and `plugins/services`.

Rules for CLI plugins:

- Keep command output stable and agent-friendly.
- Use `--json` for structured output when the command returns machine-readable data.
- Keep compact JSON compact. Do not casually add fields to existing compact output.
- Prefer `--find VALUE` for device lookup instead of forcing users or agents to know opaque IDs.
- Do not register a CLI command for web-only plugins.

The command `insylusctl plugins` lists CLI-capable plugins only. A plugin that only registers web/API behavior should not appear there.

## API Plugins

Register public API routes through:

```go
host.API().HandleFunc("GET /api/example", handler)
```

Insylus uses Go's `http.ServeMux` method-pattern syntax, so include the HTTP method in the route pattern:

```go
host.API().HandleFunc("GET /api/example", handlers.List)
host.API().HandleFunc("POST /api/example", handlers.Create)
host.API().HandleFunc("GET /api/example/{id}", handlers.Detail)
```

API routes should be public API contracts. If a route is only for the web UI, register it on `host.Web()`, not `host.API()`.

Example: topology uses `/topology/graph` as a UI-private web route and intentionally does not expose `/api/topology`.

API rules:

- Namespace feature APIs under `/api/<feature>`.
- Return deterministic JSON where practical.
- Preserve compatibility for existing routes unless the plan explicitly says to change them.
- Implement feature handlers inside the plugin package.
- Use generic backbone services such as `host.DB()`, `host.Secrets()`, and `host.Data().Inventory()` when the handler needs app data.
- Add tests for new API behavior when possible.

## Web Plugins

Web plugins can register:

- Web routes.
- Header navigation items.
- Plugin-owned templates.
- Plugin-owned static assets.

Example:

```go
package example

import (
	"embed"
	"io/fs"

	"insylus/internal/pluginhost"
)

//go:embed templates/*.html static/*
var files embed.FS

type Plugin struct{}

func New() pluginhost.Plugin { return Plugin{} }
func (Plugin) ID() string    { return "example" }
func (Plugin) Name() string  { return "Example" }

func (Plugin) Register(host pluginhost.Host) error {
	if !host.Web().Enabled() {
		return nil
	}

	templateFS, err := fs.Sub(files, ".")
	if err != nil {
		return err
	}
	staticFS, err := fs.Sub(files, "static")
	if err != nil {
		return err
	}

	host.Web().NavItem(pluginhost.NavItem{
		Label: "Example",
		Href:  "/example",
		Order: 50,
	})
	host.Web().Templates(templateFS, "templates/*.html")
	host.Web().Static("/plugin-assets/example/", staticFS)
	host.Web().HandleFunc("GET /example", handlePage)
	return nil
}
```

Template files are parsed into the shared server template set. The base layout remains owned by the server. Plugin pages should define their content templates in their own `templates/*.html` files.

Static assets must be namespaced:

```text
/plugin-assets/<plugin-id>/...
```

Do not use `/static/*` for plugin-owned files. `/static/*` is for shared base app assets.

Navigation item ordering should leave room for future plugins. Current rough order:

- `10`: Devices
- `20`: Services
- `30`: Access Settings and Agent Settings
- `40`: Topology
- `50+`: New feature plugins

Web-only plugin checklist:

- Register only through `host.Web()`.
- Do not call `host.CLI().AddCommand`.
- Do not register public `/api/...` routes unless the feature genuinely has a public API.
- Use `/plugin-assets/<plugin-id>/...` for plugin JS/CSS/images.
- Verify the page loads after a server rebuild.

If the plugin page needs shared layout rendering, register templates with `host.Web().Templates(...)` and call `host.Web().Render(...)` from the plugin-owned handler.

## Data Access And Handlers

Plugins should not reach into concrete server internals directly.

Use:

```go
host.Targets()
host.Capabilities()
host.DB()
host.Secrets()
```

The only data lookup surface exposed through `host.Data()` is generic inventory lookup for plugins that need a compact target/device resolver:

```go
host.Data().Inventory()
```

Feature-specific handlers belong inside the plugin package that owns the feature.

For a new feature, keep the feature implementation in the plugin folder.

Use:

```go
host.DB()
host.Secrets()
host.Data().Inventory()
host.Web().Render(...)
```

`host.DB()` gives access to SQL execution for plugin-owned tables.

`host.Secrets()` encrypts and decrypts plugin-owned secrets using the controller's local plugin secret key.

`host.Data().Inventory()` gives device lookup without importing `internal/server` or depending on the concrete `Store`.

Avoid passing the concrete server `Store` into plugins. Avoid importing `internal/server` from plugins. The goal is for plugins to depend on stable backbone contracts, not app internals.

The current reference implementation is `plugins/proxmox`: its routes, handlers, SQL wrapper, external API client, templates, CLI command, and response types all live inside the plugin folder.

## Migrations

Plugins may register migrations:

```go
host.Migrations().Add(pluginhost.Migration{
	PluginID: "example",
	Version:  1,
	Name:     "create example tables",
	SQL: `
create table if not exists example_items (
	id text primary key,
	name text not null,
	created_at text not null
);
`,
})
```

Migration behavior:

- Base server schema migrations run first.
- Plugin migrations run after base migrations.
- Plugin migrations are sorted by `PluginID`, then `Version`.
- Applied plugin migrations are tracked in the `plugin_migrations` table.
- A migration runs once per `(plugin_id, version)`.

Migration rules:

- Set `PluginID` to the plugin's exact `ID()`.
- Start at version `1`.
- Increment by one for each new migration.
- Never edit an already-applied migration casually.
- Prefer additive migrations.
- Do not delete or reset the production database.
- Do not remove legacy migration compatibility unless explicitly asked.

## Static Assets

Use embedded files for plugin assets:

```go
//go:embed static/*
var files embed.FS
```

Then register the `static` subtree:

```go
staticFS, err := fs.Sub(files, "static")
if err != nil {
	return err
}
host.Web().Static("/plugin-assets/example/", staticFS)
```

In templates, reference assets with the registered prefix:

```html
<script src="/plugin-assets/example/example.js" defer></script>
```

Rules:

- Keep plugin assets under the plugin folder.
- Use one asset namespace per plugin.
- Do not reuse `/static/*` for plugin-specific files.
- Do not reference local development paths from browser templates.

## Templates

A plugin with web pages should embed and register templates:

```go
//go:embed templates/*.html
var templateFiles embed.FS

func (Plugin) Register(host pluginhost.Host) error {
	if host.Web().Enabled() {
		templateFS, err := fs.Sub(templateFiles, ".")
		if err != nil {
			return err
		}
		host.Web().Templates(templateFS, "templates/*.html")
	}
	return nil
}
```

Template rules:

- Keep feature templates in `plugins/<plugin-id>/templates`.
- Use the shared layout instead of duplicating page chrome.
- Keep template names unique enough to avoid accidental collisions.
- Avoid placing feature-specific templates in `internal/server/templates`.

## Route Ownership

A plugin should own the routes for its feature.

Good route ownership examples:

- `plugins/dashboard`: `/` landing dashboard
- `plugins/devices`: `/devices`, `/devices/{id}`, `/api/devices`, `/api/devices/find`
- `plugins/services`: `/services`, `/services/history`, `/api/services`
- `plugins/wake`: `/devices/{id}/wake`, `/api/devices/{id}/wake`
- `plugins/topology`: `/topology`, `/topology/graph`, topology edit form routes

Avoid scattering one feature's routes across multiple plugins unless there is a strong product reason.

If a plugin adds a UI action to another feature's page, it may register a route under that page's namespace. Example: topology registers `POST /devices/{id}/topology` because the device detail page can update topology metadata.

## Adding A New Plugin

Use this checklist for a new plugin such as `backups`.

1. Create the folder:

```bash
mkdir -p plugins/backups
```

2. Add `plugins/backups/plugin.go`.

3. Implement:

```go
func New() pluginhost.Plugin
func (Plugin) ID() string
func (Plugin) Name() string
func (Plugin) Register(pluginhost.Host) error
```

4. Add CLI command registration if the feature needs `insylusctl backups`.

5. Add API route registration if the feature needs public `/api/backups...` routes.

6. Add web route, nav, templates, and assets if the feature needs pages.

7. Add migrations if the feature needs tables or columns.

8. If the plugin needs storage, add plugin-owned migrations and a plugin-owned `store.go` that uses `host.DB()`.

9. If the plugin needs secrets, use `host.Secrets()` from plugin-owned code.

10. If the plugin needs device lookup, use `host.Data().Inventory()`.

11. Regenerate the registry:

```bash
bash scripts/generate-plugin-registry.sh
```

12. Format and test:

```bash
gofmt -w plugins/backups
go test ./...
go build -o /opt/insylus/dist/insylus-server ./cmd/insylus-server
go build -o /opt/insylus/dist/insylusctl ./cmd/insylusctl
```

13. If the running local app should include the plugin, follow the redeploy flow in `AGENTS.md`.

## Removing A Plugin

To remove a plugin from the built app:

1. Remove or move `plugins/<plugin-id>/plugin.go` and `plugins/<plugin-id>/skill.md`.
2. Regenerate the registry:

```bash
bash scripts/generate-plugin-registry.sh
```

3. Rebuild the binaries.

Removing a plugin from the registry removes its registered CLI commands, web nav items, routes, templates, static assets, skill contribution, and future migrations from the rebuilt app.

Do not automatically drop plugin database tables when removing a plugin. Data removal should be a deliberate migration or operator action.

## OpenClaw Skill Contribution

Plugins can contribute to the OpenClaw skill by adding a `skill.md` file:

```text
plugins/<plugin-id>/skill.md
```

When `scripts/deploy.sh` runs, it collects all `skill.md` files from plugin directories and injects them into `openclaw-skills/insylus/SKILL.md` under a marker. This keeps the skill in sync with available plugins automatically.

The skill content should include:

- A brief description of what the plugin does.
- Rules and constraints for the feature.
- CLI commands and examples.
- API endpoints and their purpose.
- Web UI paths if applicable.
- Interpretation notes (how to read output, what fields mean, etc.).

Example `skill.md`:

```markdown
## Example plugin

The example plugin does X and Y.

### Rules

- Rule one.
- Rule two.

### CLI commands

```bash
insylusctl example --action foo
```

### API endpoints

```bash
GET /api/example
```

### Interpretation

- `--action foo` returns items sorted by name.
```

The deploy script inserts each `skill.md` between `<!-- JELLYFIN_PLUGIN_SECTION -->` markers. If you remove a plugin, delete its `skill.md` and the next deploy will remove it from the skill. You can also regenerate the skill at any time by running:

```bash
bash /opt/insylus/scripts/deploy.sh
```

## AI Agent Implementation Checklist

When an AI agent is asked to add or change a plugin, follow this order:

1. Read `AGENTS.md`.
2. Read this file.
3. Inspect `internal/pluginhost`.
4. Inspect `plugins/proxmox` when you need a self-contained plugin example.
5. Inspect the closest existing plugin under `plugins/*`, but remember that some older built-ins still use legacy server-owned handlers.
6. Decide which surfaces the plugin needs: CLI, API, web, DB, secrets, inventory, migrations.
7. Make the smallest plugin-scoped code change that implements the requested feature.
8. Do not add feature-specific internals to the base app.
9. Regenerate `plugins/registry/registry_gen.go` if plugin folders changed.
10. Run `gofmt`.
11. Run `go test ./...`.
12. Build affected binaries without `sudo`.
13. Redeploy only when the request affects the running local app or `AGENTS.md` says to.
14. Verify the exact behavior the user asked for.

Do not:

- Build with `sudo`.
- Chown `/opt/insylus` to `root`.
- Reach into the concrete server `Store` from plugin code.
- Add feature-specific backbone surfaces such as `host.Data().MyFeature()`.
- Put feature handlers, clients, stores, or DTOs in `internal/server` or `internal/shared` when they belong to one plugin.
- Add `insylusctl topology`.
- Add public `/api/topology`.
- Put plugin templates in `internal/server/templates`.
- Put plugin assets in shared `/static/*`.
- Change compact JSON output casually.
- Delete production data.

## Common Patterns

### CLI Plus API

Use this for commands that call plugin-owned server routes:

```go
func (Plugin) Register(host pluginhost.Host) error {
	if host.CLI().Enabled() {
		host.CLI().AddCommand(command())
	}
	if host.API().Enabled() {
		rt := runtime{store: newStore(host)}
		host.API().HandleFunc("GET /api/example", rt.handleList)
	}
	return nil
}
```

The `runtime`, `store`, handlers, response types, and API client should live inside the plugin package.

### API Plus DB Plus Secrets

Use this for a plugin that owns tables or credentials:

```go
type runtime struct {
	store store
}

type store struct {
	db      pluginhost.DBHost
	secrets pluginhost.SecretHost
}

func newStore(host pluginhost.Host) store {
	return store{
		db:      host.DB(),
		secrets: host.Secrets(),
	}
}

func (Plugin) Register(host pluginhost.Host) error {
	if host.API().Enabled() {
		rt := runtime{store: newStore(host)}
		host.API().HandleFunc("GET /api/example/items", rt.handleItems)
	}
	if host.Migrations().Enabled() {
		host.Migrations().Add(pluginhost.Migration{
			PluginID: "example",
			Version:  1,
			Name:     "create example schema",
			SQL:      `create table if not exists example_items (id text primary key);`,
		})
	}
	return nil
}
```

Keep this `runtime`, `store`, and all example request/response types in `plugins/example`.

### Web Page Only

Use this for a page that has no CLI and no public API:

```go
func (Plugin) Register(host pluginhost.Host) error {
	if !host.Web().Enabled() {
		return nil
	}
	host.Web().NavItem(pluginhost.NavItem{Label: "Example", Href: "/example", Order: 50})
	host.Web().HandleFunc("GET /example", handlePage)
	return nil
}
```

This plugin will not appear in `insylusctl plugins`.

### Web UI Private JSON

Use web routes, not API routes, for JSON that only exists to feed a page:

```go
host.Web().HandleFunc("GET /example/graph", handleGraph)
```

Do not put this under `/api/...` unless it is intended as public API.

### Plugin Migration Only

Use this for a feature that only adds schema support:

```go
func (Plugin) Register(host pluginhost.Host) error {
	if host.Migrations().Enabled() {
		host.Migrations().Add(pluginhost.Migration{
			PluginID: "example",
			Version:  1,
			Name:     "create example schema",
			SQL:      `create table if not exists example_items (id text primary key);`,
		})
	}
	return nil
}
```

## Verification Commands

Basic checks:

```bash
go test ./...
go build -o /opt/insylus/dist/insylusctl ./cmd/insylusctl
go build -o /opt/insylus/dist/insylus-server ./cmd/insylus-server
```

CLI checks:

```bash
insylusctl --help
insylusctl plugins
insylusctl help <command>
```

Web/API checks after redeploy:

```bash
curl -i http://127.0.0.1:8080/
curl -i http://127.0.0.1:8080/services
curl -i http://127.0.0.1:8080/topology
curl -i http://127.0.0.1:8080/api/devices
curl -i http://127.0.0.1:8080/api/topology
```

Expected topology behavior:

- `/topology` returns the topology page.
- `/topology/graph` returns data for the web UI.
- `/api/topology` returns `404`.
- `insylusctl topology` is unknown.
- `topology` does not appear in `insylusctl plugins`.

## Current Built-In Plugins

Current top-level plugins:

- `plugins/access`: access settings, keys, policy, device mode, agent auto-update web/API behavior.
- `plugins/agent`: bootstrap, checkin, policy fetch, report, install page/script, agent downloads.
- `plugins/dashboard`: web-only landing dashboard at `/`.
- `plugins/devices`: device CLI, `/devices`, `/devices/{id}`, and `/api/devices*`.
- `plugins/help`: CLI help and CLI plugin list commands.
- `plugins/proxmox`: Proxmox CLI, `/api/proxmox*`, `/proxmox` token setup page, and user-provided token storage.
- `plugins/services`: services CLI, `/api/services*`, services and history web pages.
- `plugins/topology`: web-only topology page, topology edit routes, `/topology/graph`, and topology assets.
- `plugins/wake`: wake CLI plus web/API wake actions.

The Proxmox plugin is intentionally based on user-created API tokens. Do not add token auto-provisioning unless a future plan explicitly changes that product decision.
