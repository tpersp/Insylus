package agent

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

func (Plugin) ID() string { return "agent" }

func (Plugin) Name() string { return "Agent" }

func (Plugin) Register(host pluginhost.Host) error {
	rt := runtime{db: host.DB(), targets: host.Targets(), render: host.Web().Render}
	if provider, ok := host.Capabilities().Lookup("managed_account_config_provider"); ok {
		if managed, ok := provider.(shared.ManagedAccountConfigProvider); ok {
			rt.managedAccountConfigProvider = managed
		}
	}
	if host.Web().Enabled() {
		templateFS, err := fs.Sub(templateFiles, ".")
		if err != nil {
			return err
		}
		host.Web().NavItem(pluginhost.NavItem{Label: "Agent Settings", Href: "/agent/settings", Order: 35})
		host.Web().Templates(templateFS, "templates/*.html")
		host.Web().HandleFunc("GET /agent/settings", rt.handleAgentSettingsPage)
		host.Web().HandleFunc("POST /agent/settings/auto-update", rt.handleUpdateAgentAutoUpdateDefault)
		host.Web().HandleFunc("GET /devices/{id}/install", rt.handleInstallPage)
		host.Web().HandleFunc("GET /install.sh", rt.handleInstallScript)
	}
	if host.API().Enabled() {
		host.API().HandleFunc("GET /api/bootstrap/register", methodNotAllowed)
		host.API().HandleFunc("POST /api/bootstrap/register", rt.handleBootstrapRegister)
		host.API().HandleFunc("POST /api/checkin", rt.handleCheckIn)
		host.API().HandleFunc("GET /api/policy", rt.handlePolicyFetch)
		host.API().HandleFunc("POST /api/agent/update-status", rt.handleUpdateStatus)
		host.API().HandleFunc("POST /api/report", rt.handleReport)
		host.API().HandleFunc("GET /downloads/insylus-agent", rt.handleDownload)
	}
	return nil
}
