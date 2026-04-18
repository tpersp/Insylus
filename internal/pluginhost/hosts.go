package pluginhost

import (
	"context"
	"database/sql"
	"io/fs"
	"net/http"

	"insylus/internal/ctl"
)

type CLIOnlyHost struct {
	App *ctl.App
}

func NewCLIOnlyHost(app *ctl.App) CLIOnlyHost {
	return CLIOnlyHost{App: app}
}

func (h CLIOnlyHost) CLI() CLIHost              { return h.App }
func (h CLIOnlyHost) API() RouteHost            { return DisabledRouteHost{} }
func (h CLIOnlyHost) Web() WebHost              { return DisabledWebHost{} }
func (h CLIOnlyHost) Data() DataHost            { return DisabledDataHost{} }
func (h CLIOnlyHost) Migrations() MigrationHost { return DisabledMigrationHost{} }
func (h CLIOnlyHost) DB() DBHost                { return DisabledDBHost{} }
func (h CLIOnlyHost) Secrets() SecretHost       { return DisabledSecretHost{} }
func (h CLIOnlyHost) Targets() TargetService    { return DisabledTargetService{} }
func (h CLIOnlyHost) Capabilities() CapabilityRegistry {
	return DisabledCapabilityRegistry{}
}
func (h CLIOnlyHost) Plugins() PluginRegistry { return DisabledPluginRegistry{} }

type DisabledRouteHost struct{}

func (DisabledRouteHost) Enabled() bool                       { return false }
func (DisabledRouteHost) HandleFunc(string, http.HandlerFunc) {}

type DisabledWebHost struct{ DisabledRouteHost }

func (DisabledWebHost) NavItem(NavItem)            {}
func (DisabledWebHost) Templates(fs.FS, ...string) {}
func (DisabledWebHost) Static(string, fs.FS)       {}
func (DisabledWebHost) Render(http.ResponseWriter, string, any) {
}

type DisabledMigrationHost struct{}

func (DisabledMigrationHost) Enabled() bool { return false }
func (DisabledMigrationHost) Add(Migration) {}

type DisabledDataHost struct{}

func (DisabledDataHost) Inventory() InventoryService {
	return DisabledInventoryService{}
}

type DisabledInventoryService struct{}

func (DisabledInventoryService) ListDevices(context.Context) ([]InventoryDevice, error) {
	return nil, nil
}
func (DisabledInventoryService) GetDevice(context.Context, string) (InventoryDevice, error) {
	return InventoryDevice{}, sql.ErrNoRows
}
func (DisabledInventoryService) FindDevice(context.Context, string) ([]InventoryDevice, error) {
	return nil, nil
}
func (DisabledInventoryService) ListInventory(context.Context, string, string) (any, error) {
	return nil, sql.ErrConnDone
}
func (DisabledInventoryService) GetInventory(context.Context, string, string, string) (any, error) {
	return nil, sql.ErrConnDone
}
func (DisabledInventoryService) FindInventory(context.Context, string, string, string) (any, error) {
	return nil, sql.ErrConnDone
}

type DisabledDBHost struct{}

func (DisabledDBHost) Enabled() bool { return false }
func (DisabledDBHost) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, sql.ErrConnDone
}
func (DisabledDBHost) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, sql.ErrConnDone
}
func (DisabledDBHost) QueryRowContext(context.Context, string, ...any) *sql.Row {
	return nil
}

type DisabledSecretHost struct{}

func (DisabledSecretHost) Enabled() bool                             { return false }
func (DisabledSecretHost) Encrypt(plaintext string) (string, error)  { return "", sql.ErrConnDone }
func (DisabledSecretHost) Decrypt(ciphertext string) (string, error) { return "", sql.ErrConnDone }

type DisabledCapabilityRegistry struct{}

func (DisabledCapabilityRegistry) Provide(string, any)       {}
func (DisabledCapabilityRegistry) Lookup(string) (any, bool) { return nil, false }
func (DisabledCapabilityRegistry) Names() []string           { return nil }

type DisabledPluginRegistry struct{}

func (DisabledPluginRegistry) Available() []PluginManifest { return nil }
func (DisabledPluginRegistry) Enabled(string) bool         { return false }
func (DisabledPluginRegistry) Enable(context.Context, string) error {
	return sql.ErrConnDone
}
func (DisabledPluginRegistry) Disable(context.Context, string) error {
	return sql.ErrConnDone
}
func (DisabledPluginRegistry) Purge(context.Context, string) error {
	return sql.ErrConnDone
}
func (DisabledPluginRegistry) Profiles() []PluginProfile { return nil }
func (DisabledPluginRegistry) ApplyProfile(context.Context, string) error {
	return sql.ErrConnDone
}

type DisabledTargetService struct{}

func (DisabledTargetService) List(context.Context) ([]Target, error) { return nil, nil }
func (DisabledTargetService) Get(context.Context, string) (Target, error) {
	return Target{}, sql.ErrNoRows
}
func (DisabledTargetService) Find(context.Context, string) ([]Target, error) { return nil, nil }
func (DisabledTargetService) Create(context.Context, TargetInput) (Target, error) {
	return Target{}, sql.ErrConnDone
}
func (DisabledTargetService) Update(context.Context, string, TargetInput) (Target, error) {
	return Target{}, sql.ErrConnDone
}
func (DisabledTargetService) Delete(context.Context, string) error { return sql.ErrConnDone }
func (DisabledTargetService) SetMetadata(context.Context, string, string, map[string]any) error {
	return sql.ErrConnDone
}
func (DisabledTargetService) Metadata(context.Context, string) (map[string]map[string]any, error) {
	return nil, sql.ErrConnDone
}
func (DisabledTargetService) PurgePlugin(context.Context, string) error { return sql.ErrConnDone }
