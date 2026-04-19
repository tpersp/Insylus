package dashboard

import (
	"embed"
	"io/fs"

	"insylus/internal/pluginhost"
)

//go:embed templates/*.html
var templateFiles embed.FS

type Plugin struct{}

func New() pluginhost.Plugin {
	return Plugin{}
}

func (Plugin) ID() string {
	return "dashboard"
}

func (Plugin) Name() string {
	return "Dashboard"
}

func (Plugin) Register(host pluginhost.Host) error {
	if host.Web().Enabled() {
		templateFS, err := fs.Sub(templateFiles, ".")
		if err != nil {
			return err
		}
		rt := runtime{
			db:        host.DB(),
			inventory: host.Data().Inventory(),
			plugins:   host.Plugins(),
			targets:   host.Targets(),
			render:    host.Web().Render,
		}
		host.Web().NavItem(pluginhost.NavItem{PluginID: "dashboard", Label: "Dashboard", Href: "/", Order: 0})
		host.Web().Templates(templateFS, "templates/*.html")
		host.Web().HandleFunc("GET /{$}", rt.handlePage)
	}
	return nil
}
