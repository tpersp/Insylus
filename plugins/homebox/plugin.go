package homebox

import (
	"embed"
	"io/fs"

	"insylus/internal/pluginhost"
)

//go:embed templates/*.html
var files embed.FS

type Plugin struct{}

func New() pluginhost.Plugin {
	return Plugin{}
}

func (Plugin) ID() string {
	return "homebox"
}

func (Plugin) Name() string {
	return "HomeBox"
}

func (Plugin) Register(host pluginhost.Host) error {
	if host.Web().Enabled() {
		templateFS, err := fs.Sub(files, ".")
		if err != nil {
			return err
		}
		rt := runtime{store: newStore(host), render: host.Web().Render}
		host.Web().NavItem(pluginhost.NavItem{PluginID: "homebox", Label: "HomeBox", Href: "/homebox", Order: 55})
		host.Web().Templates(templateFS, "templates/*.html")
		host.Web().HandleFunc("GET /homebox", rt.handlePage)
		host.Web().HandleFunc("POST /homebox/config", rt.handleSetConfig)
		host.Web().HandleFunc("POST /homebox/config/delete", rt.handleDeleteConfig)
	}
	if host.API().Enabled() {
		rt := runtime{store: newStore(host)}
		host.API().HandleFunc("GET /api/homebox/config", rt.handleGetConfig)
		host.API().HandleFunc("POST /api/homebox/config", rt.handleSetConfig)
		host.API().HandleFunc("POST /api/homebox/config/delete", rt.handleDeleteConfig)
		host.API().HandleFunc("POST /api/homebox/test", rt.handleTest)
		host.API().HandleFunc("GET /api/homebox/self", rt.handleSelf)
		host.API().HandleFunc("GET /api/homebox/items", rt.handleItems)
		host.API().HandleFunc("GET /api/homebox/items/{id}", rt.handleItem)
		host.API().HandleFunc("GET /api/homebox/labels", rt.handleLabels)
		host.API().HandleFunc("GET /api/homebox/locations", rt.handleLocations)
		host.API().HandleFunc("GET /api/homebox/statistics", rt.handleStatistics)
	}
	if host.Migrations().Enabled() {
		host.Migrations().Add(pluginhost.Migration{
			PluginID: "homebox",
			Version:  1,
			Name:     "create homebox config table",
			SQL: `
create table if not exists homebox_config (
	id text primary key,
	base_url text not null,
	username text not null,
	password_encrypted text not null,
	token_encrypted text not null default '',
	attachment_token_encrypted text not null default '',
	expires_at text not null default '',
	last_connected_at text not null default '',
	last_error text not null default '',
	created_at text not null,
	updated_at text not null
);
`,
		})
	}
	return nil
}
