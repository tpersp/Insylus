package topology

import (
	"embed"
	"io/fs"

	"insylus/internal/pluginhost"
)

//go:embed templates/*.html static/*
var files embed.FS

type Plugin struct{}

func New() pluginhost.Plugin { return Plugin{} }

func (Plugin) ID() string { return "topology" }

func (Plugin) Name() string { return "Topology" }

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
	rt := runtime{db: host.DB(), targets: host.Targets(), render: host.Web().Render}
	host.Web().NavItem(pluginhost.NavItem{PluginID: "topology", Label: "Topology", Href: "/topology", Order: 40})
	host.Web().Templates(templateFS, "templates/*.html")
	host.Web().Static("/plugin-assets/topology/", staticFS)
	host.Web().HandleFunc("GET /topology", rt.handlePage)
	host.Web().HandleFunc("GET /topology/graph", rt.handleGraph)
	host.Web().HandleFunc("POST /topology/nodes", rt.handleCreateNode)
	host.Web().HandleFunc("POST /topology/nodes/{id}", rt.handleUpdateNode)
	host.Web().HandleFunc("POST /topology/nodes/{id}/delete", rt.handleDeleteNode)
	host.Web().HandleFunc("POST /topology/links", rt.handleCreateLink)
	host.Web().HandleFunc("POST /topology/links/{id}", rt.handleUpdateLink)
	host.Web().HandleFunc("POST /topology/links/{id}/delete", rt.handleDeleteLink)
	host.Web().HandleFunc("POST /topology/layout", rt.handleSaveLayout)
	host.Web().HandleFunc("POST /topology/layout/reset", rt.handleResetLayout)
	host.Web().HandleFunc("GET /api/topology", rt.notFound)
	if host.Migrations().Enabled() {
		host.Migrations().Add(pluginhost.Migration{PluginID: "topology", Version: 1, Name: "create topology tables", SQL: `
create table if not exists topology_nodes (
	id integer primary key autoincrement,
	name text not null,
	kind text not null,
	note text not null default '',
	created_at text not null,
	updated_at text not null
);
create table if not exists topology_links (
	id integer primary key autoincrement,
	from_kind text not null,
	from_id text not null,
	to_kind text not null,
	to_id text not null,
	label text not null default '',
	source text not null default 'manual',
	created_at text not null,
	updated_at text not null
);
`})
		host.Migrations().Add(pluginhost.Migration{PluginID: "topology", Version: 2, Name: "create topology layout positions", SQL: `
create table if not exists topology_node_positions (
	subject_kind text not null,
	subject_id text not null,
	x real not null,
	y real not null,
	updated_at text not null,
	primary key(subject_kind, subject_id)
);
`})
	}
	return nil
}
