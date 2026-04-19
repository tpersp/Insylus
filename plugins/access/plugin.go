package access

import (
	"embed"
	"io/fs"

	"insylus/internal/pluginhost"
	"insylus/internal/shared"
)

//go:embed templates/*.html
var templateFiles embed.FS

type Plugin struct{}

func New() pluginhost.Plugin { return Plugin{} }

func (Plugin) ID() string { return "access" }

func (Plugin) Name() string { return "Access" }

func (Plugin) Register(host pluginhost.Host) error {
	if !host.Web().Enabled() {
		return nil
	}
	templateFS, err := fs.Sub(templateFiles, ".")
	if err != nil {
		return err
	}
	rt := runtime{store: newStore(host), managed: managedProvider(host), render: host.Web().Render}
	host.Web().NavItem(pluginhost.NavItem{PluginID: "access", Label: "SSH Keys", Href: "/keys", Order: 50})
	host.Web().Templates(templateFS, "templates/*.html")
	host.Web().HandleFunc("POST /devices/{id}/policy", rt.handleUpdatePolicy)
	host.Web().HandleFunc("POST /devices/{id}/mode", rt.handleUpdateDeviceMode)
	host.Web().HandleFunc("POST /devices/{id}/agent-auto-update", rt.handleUpdateDeviceAgentAutoUpdate)
	host.Web().HandleFunc("GET /keys", rt.handleKeysPage)
	host.Web().HandleFunc("POST /keys", rt.handleCreateKey)
	return nil
}

func managedProvider(host pluginhost.Host) shared.ManagedAccountConfigProvider {
	if provider, ok := host.Capabilities().Lookup("managed_account_config_provider"); ok {
		if managed, ok := provider.(shared.ManagedAccountConfigProvider); ok {
			return managed
		}
	}
	return nil
}
