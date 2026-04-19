package server

import (
	"context"
	"database/sql"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"sort"
	"strings"

	"insylus/internal/ctl"
	"insylus/internal/pluginhost"
)

type routeDef struct {
	Pattern string
	Handler http.HandlerFunc
}

type serverPluginHost struct {
	app *App
}

type serverRouteHost struct {
	enabled bool
	routes  *[]routeDef
}

type serverWebHost struct {
	serverRouteHost
	app *App
}

type serverMigrationHost struct {
	app *App
}

type serverDBHost struct {
	app *App
}

type serverSecretHost struct {
	app *App
}

type disabledCLIHost struct{}

func (disabledCLIHost) Enabled() bool                      { return false }
func (disabledCLIHost) AddCommand(ctl.Command)             {}
func (disabledCLIHost) AddPlugin(ctl.PluginInfo)           {}
func (disabledCLIHost) Commands() []ctl.Command            { return nil }
func (disabledCLIHost) Command(string) (ctl.Command, bool) { return ctl.Command{}, false }
func (disabledCLIHost) Plugins() []ctl.PluginInfo          { return nil }
func (disabledCLIHost) PrintUsage(io.Writer)               {}

func newServerPluginHost(app *App) serverPluginHost {
	return serverPluginHost{app: app}
}

func (h serverPluginHost) CLI() pluginhost.CLIHost {
	return disabledCLIHost{}
}

func (h serverPluginHost) API() pluginhost.RouteHost {
	return serverRouteHost{enabled: true, routes: &h.app.apiRoutes}
}

func (h serverPluginHost) Web() pluginhost.WebHost {
	return serverWebHost{
		serverRouteHost: serverRouteHost{enabled: true, routes: &h.app.webRoutes},
		app:             h.app,
	}
}

func (h serverPluginHost) Data() pluginhost.DataHost {
	return serverDataHost{app: h.app}
}

func (h serverPluginHost) Migrations() pluginhost.MigrationHost {
	return serverMigrationHost{app: h.app}
}

func (h serverPluginHost) DB() pluginhost.DBHost {
	return serverDBHost{app: h.app}
}

func (h serverPluginHost) Secrets() pluginhost.SecretHost {
	return serverSecretHost{app: h.app}
}

func (h serverPluginHost) Targets() pluginhost.TargetService {
	return h.app.store.targetService()
}

func (h serverPluginHost) Capabilities() pluginhost.CapabilityRegistry {
	return h.app.capabilities
}

func (h serverPluginHost) Plugins() pluginhost.PluginRegistry {
	return h.app.plugins
}

func (h serverRouteHost) Enabled() bool {
	return h.enabled
}

func (h serverRouteHost) HandleFunc(pattern string, handler http.HandlerFunc) {
	if !h.enabled || handler == nil {
		return
	}
	*h.routes = append(*h.routes, routeDef{Pattern: pattern, Handler: handler})
}

func (h serverWebHost) NavItem(item pluginhost.NavItem) {
	h.app.navItems = append(h.app.navItems, item)
}

func (h serverWebHost) Templates(fsys fs.FS, patterns ...string) {
	h.app.templateSets = append(h.app.templateSets, pluginhost.TemplateSet{FS: fsys, Patterns: patterns})
}

func (h serverWebHost) Static(prefix string, fsys fs.FS) {
	h.app.staticMounts = append(h.app.staticMounts, pluginhost.StaticMount{Prefix: prefix, FS: fsys})
}

func (h serverWebHost) Render(w http.ResponseWriter, name string, data any) {
	h.app.render(w, name, data)
}

func (h serverMigrationHost) Enabled() bool {
	return true
}

func (h serverMigrationHost) Add(migration pluginhost.Migration) {
	h.app.pluginMigrations = append(h.app.pluginMigrations, migration)
}

func (h serverDBHost) Enabled() bool {
	return true
}

func (h serverDBHost) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return h.app.store.db.ExecContext(ctx, query, args...)
}

func (h serverDBHost) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return h.app.store.db.QueryContext(ctx, query, args...)
}

func (h serverDBHost) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return h.app.store.db.QueryRowContext(ctx, query, args...)
}

func (h serverSecretHost) Enabled() bool {
	return true
}

func (h serverSecretHost) Encrypt(plaintext string) (string, error) {
	return h.app.store.encryptPluginSecret(plaintext)
}

func (h serverSecretHost) Decrypt(ciphertext string) (string, error) {
	return h.app.store.decryptPluginSecret(ciphertext)
}

func (a *App) pluginNavItems() []pluginhost.NavItem {
	items := append([]pluginhost.NavItem(nil), a.navItems...)
	// Filter to only include nav items from enabled plugins
	enabled := make(map[string]bool)
	for _, m := range a.plugins.Available() {
		enabled[m.ID] = m.Enabled
	}
	filtered := items[:0]
	for _, item := range items {
		if item.PluginID == "" || enabled[item.PluginID] {
			filtered = append(filtered, item)
		}
	}
	pluginhost.SortNav(filtered)
	return filtered
}

func (a *App) parseTemplates(funcs template.FuncMap) error {
	tpl := template.New("").Funcs(funcs)
	if _, err := tpl.ParseFS(embeddedFiles, "templates/*.html"); err != nil {
		return err
	}
	for _, set := range a.templateSets {
		if _, err := tpl.ParseFS(set.FS, set.Patterns...); err != nil {
			return err
		}
	}
	a.templates = tpl
	return nil
}

func (a *App) applyPluginMigrations(ctx context.Context) error {
	if _, err := a.store.db.ExecContext(ctx, `create table if not exists plugin_migrations (
		plugin_id text not null,
		version integer not null,
		name text not null default '',
		applied_at text not null,
		primary key (plugin_id, version)
	);`); err != nil {
		return err
	}
	sort.SliceStable(a.pluginMigrations, func(i, j int) bool {
		if a.pluginMigrations[i].PluginID != a.pluginMigrations[j].PluginID {
			return a.pluginMigrations[i].PluginID < a.pluginMigrations[j].PluginID
		}
		return a.pluginMigrations[i].Version < a.pluginMigrations[j].Version
	})
	for _, migration := range a.pluginMigrations {
		if migration.PluginID == "" || migration.Version == 0 || migration.SQL == "" {
			continue
		}
		var exists int
		if err := a.store.db.QueryRowContext(ctx,
			`select count(*) from plugin_migrations where plugin_id = ? and version = ?`,
			migration.PluginID, migration.Version,
		).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			continue
		}
		tx, err := a.store.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, migration.SQL); err != nil {
			if strings.Contains(err.Error(), "duplicate column name") {
				if _, recordErr := tx.ExecContext(ctx,
					`insert into plugin_migrations (plugin_id, version, name, applied_at) values (?, ?, ?, datetime('now'))`,
					migration.PluginID, migration.Version, migration.Name,
				); recordErr != nil {
					_ = tx.Rollback()
					return recordErr
				}
				if err := tx.Commit(); err != nil {
					return err
				}
				continue
			}
			_ = tx.Rollback()
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`insert into plugin_migrations (plugin_id, version, name, applied_at) values (?, ?, ?, datetime('now'))`,
			migration.PluginID, migration.Version, migration.Name,
		); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
