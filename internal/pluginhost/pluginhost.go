package pluginhost

import (
	"context"
	"database/sql"
	"errors"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"sort"
	"time"

	"insylus/internal/ctl"
	"insylus/internal/shared"
)

var ErrInventoryFindConflict = errors.New("multiple inventory devices matched")

type Plugin interface {
	ID() string
	Name() string
	Register(Host) error
}

type PluginManifest struct {
	ID                   string   `json:"id"`
	Name                 string   `json:"name"`
	Version              string   `json:"version,omitempty"`
	Enabled              bool     `json:"enabled"`
	Provides             []string `json:"provides,omitempty"`
	Requires             []string `json:"requires,omitempty"`
	OptionalCapabilities []string `json:"optional_capabilities,omitempty"`
	CLI                  bool     `json:"cli"`
	Web                  bool     `json:"web"`
	API                  bool     `json:"api"`
}

type ManifestProvider interface {
	Manifest() PluginManifest
}

type Host interface {
	CLI() CLIHost
	API() RouteHost
	Web() WebHost
	Data() DataHost
	Migrations() MigrationHost
	DB() DBHost
	Secrets() SecretHost
	Targets() TargetService
	Capabilities() CapabilityRegistry
	Plugins() PluginRegistry
}

type CLIHost interface {
	Enabled() bool
	AddCommand(ctl.Command)
	AddPlugin(ctl.PluginInfo)
	Commands() []ctl.Command
	Command(name string) (ctl.Command, bool)
	Plugins() []ctl.PluginInfo
	PrintUsage(w io.Writer)
}

type RouteHost interface {
	Enabled() bool
	HandleFunc(pattern string, handler http.HandlerFunc)
}

type WebHost interface {
	RouteHost
	NavItem(NavItem)
	Templates(fsys fs.FS, patterns ...string)
	Static(prefix string, fsys fs.FS)
	Render(w http.ResponseWriter, name string, data any)
}

type MigrationHost interface {
	Enabled() bool
	Add(Migration)
}

type DataHost interface {
	Inventory() InventoryService
}

type CapabilityRegistry interface {
	Provide(name string, service any)
	Lookup(name string) (any, bool)
	Names() []string
}

type PluginRegistry interface {
	Available() []PluginManifest
	Enabled(pluginID string) bool
	Enable(ctx context.Context, pluginID string) error
	Disable(ctx context.Context, pluginID string) error
	Purge(ctx context.Context, pluginID string) error
	Profiles() []PluginProfile
	ApplyProfile(ctx context.Context, name string) error
}

type PluginProfile struct {
	Name        string   `json:"name"`
	PluginIDs   []string `json:"plugin_ids"`
	Description string   `json:"description,omitempty"`
}

type TargetService interface {
	List(ctx context.Context) ([]Target, error)
	Get(ctx context.Context, id string) (Target, error)
	Find(ctx context.Context, query string) ([]Target, error)
	Create(ctx context.Context, input TargetInput) (Target, error)
	Update(ctx context.Context, id string, input TargetInput) (Target, error)
	Delete(ctx context.Context, id string) error
	SetMetadata(ctx context.Context, targetID, pluginID string, metadata map[string]any) error
	Metadata(ctx context.Context, targetID string) (map[string]map[string]any, error)
	PurgePlugin(ctx context.Context, pluginID string) error
}

type TargetInput struct {
	Name      string            `json:"name"`
	Kind      string            `json:"kind,omitempty"`
	Hostname  string            `json:"hostname,omitempty"`
	IPs       []string          `json:"ips,omitempty"`
	APIURL    string            `json:"api_url,omitempty"`
	SSHHost   string            `json:"ssh_host,omitempty"`
	SSHUser   string            `json:"ssh_user,omitempty"`
	Tags      []string          `json:"tags,omitempty"`
	Note      string            `json:"note,omitempty"`
	CreatedBy string            `json:"created_by,omitempty"`
	Metadata  map[string]any    `json:"metadata,omitempty"`
	Addresses map[string]string `json:"addresses,omitempty"`
}

type Target struct {
	ID        string                    `json:"id"`
	Name      string                    `json:"name"`
	Kind      string                    `json:"kind"`
	Hostname  string                    `json:"hostname,omitempty"`
	IPs       []string                  `json:"ips,omitempty"`
	APIURL    string                    `json:"api_url,omitempty"`
	SSHHost   string                    `json:"ssh_host,omitempty"`
	SSHUser   string                    `json:"ssh_user,omitempty"`
	Tags      []string                  `json:"tags,omitempty"`
	Note      string                    `json:"note,omitempty"`
	CreatedBy string                    `json:"created_by,omitempty"`
	Metadata  map[string]map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time                 `json:"created_at"`
	UpdatedAt time.Time                 `json:"updated_at"`
}

type DBHost interface {
	Enabled() bool
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type SecretHost interface {
	Enabled() bool
	Encrypt(plaintext string) (string, error)
	Decrypt(ciphertext string) (string, error)
}

type PluginInfo struct {
	ID   string
	Name string
}

type NavItem struct {
	PluginID string
	Label    string
	Href     string
	Order    int
}

type StaticMount struct {
	PluginID string
	Prefix   string
	FS       fs.FS
}

type TemplateSet struct {
	FS       fs.FS
	Patterns []string
}

type Migration struct {
	PluginID string
	Version  int
	Name     string
	SQL      string
}

type InventoryService interface {
	ListDevices(ctx context.Context) ([]InventoryDevice, error)
	GetDevice(ctx context.Context, id string) (InventoryDevice, error)
	FindDevice(ctx context.Context, query string) ([]InventoryDevice, error)
	ListInventory(ctx context.Context, view, managedUser string) (any, error)
	GetInventory(ctx context.Context, id, view, managedUser string) (any, error)
	FindInventory(ctx context.Context, query, view, managedUser string) (any, error)
}

type InventoryDevice struct {
	ID             string
	Name           string
	Hostname       string
	OSName         string
	IPs            []string
	LastSeenAt     time.Time
	DeviceType     string
	Purpose        string
	DiscoveredType string
	DiscoveredRole string
	WakeOnLAN      shared.WakeOnLANInfo
}

func SortNav(items []NavItem) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Order != items[j].Order {
			return items[i].Order < items[j].Order
		}
		return items[i].Label < items[j].Label
	})
}

func TemplateFuncs(nav func() []NavItem) template.FuncMap {
	return template.FuncMap{
		"pluginNavItems": nav,
	}
}
