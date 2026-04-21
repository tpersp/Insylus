package discovery

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
	return "discovery"
}

func (Plugin) Name() string {
	return "Discovery"
}

func (Plugin) Register(host pluginhost.Host) error {
	if host.Web().Enabled() {
		templateFS, err := fs.Sub(templateFiles, ".")
		if err != nil {
			return err
		}
		rt := newRuntime(host)
		host.Web().NavItem(pluginhost.NavItem{
			PluginID: "discovery",
			Label:    "Discovery",
			Href:     "/discovery",
			Order:    12,
		})
		host.Web().Templates(templateFS, "templates/*.html")
		host.Web().HandleFunc("GET /discovery", rt.handlePage)
		host.Web().HandleFunc("POST /discovery/scan", rt.handleScan)
		host.Web().HandleFunc("POST /discovery/{id}/promote", rt.handlePromote)
		host.Web().HandleFunc("POST /discovery/{id}/status", rt.handleStatus)
	}
	if host.API().Enabled() {
		rt := newRuntime(host)
		host.API().HandleFunc("GET /api/discovery", rt.handleListAPI)
		host.API().HandleFunc("POST /api/discovery/scan", rt.handleScan)
		host.API().HandleFunc("POST /api/discovery/{id}/promote", rt.handlePromote)
		host.API().HandleFunc("POST /api/discovery/{id}/status", rt.handleStatus)
	}
	if host.Migrations().Enabled() {
		host.Migrations().Add(pluginhost.Migration{
			PluginID: "discovery",
			Version:  1,
			Name:     "create discovered devices table",
			SQL: `
create table if not exists discovered_devices (
	id integer primary key,
	fingerprint text not null unique,
	display_name text not null default '',
	hostname text not null default '',
	ip_address text not null default '',
	mac_address text not null default '',
	open_ports_json text not null default '[]',
	status text not null default 'pending',
	status_note text not null default '',
	source_cidr text not null default '',
	kind_hint text not null default '',
	promoted_target_id text references targets(id) on delete set null,
	first_seen_at text not null,
	last_seen_at text not null,
	created_at text not null,
	updated_at text not null
);
create index if not exists discovered_devices_status_idx on discovered_devices(status, last_seen_at desc);
create index if not exists discovered_devices_ip_idx on discovered_devices(ip_address);
`,
		})
	}
	return nil
}
