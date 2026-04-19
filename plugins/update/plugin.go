package update

import (
	"embed"
	"io/fs"
	"net/http"

	"insylus/internal/pluginhost"
)

//go:embed templates/*.html static/*
var files embed.FS

type Plugin struct{}

func New() pluginhost.Plugin {
	return Plugin{}
}

func (Plugin) ID() string {
	return "update"
}

func (Plugin) Name() string {
	return "Server Update"
}

func (Plugin) Register(host pluginhost.Host) error {
	if host.Web().Enabled() {
		templateFS, err := fs.Sub(files, ".")
		if err != nil {
			return err
		}
		staticFS, err := fs.Sub(files, "static")
		if err != nil {
			return err
		}
		rt := newRuntime(host)
		host.Web().NavItem(pluginhost.NavItem{
			Label: "Update",
			Href:  "/update",
			Order: 60,
		})
		host.Web().Templates(templateFS, "templates/*.html")
		host.Web().Static("/plugin-assets/update/", staticFS)
		host.Web().HandleFunc("GET /update", rt.handleUpdatePage)
		host.Web().HandleFunc("POST /update/skip", rt.handleSkipVersion)
		host.Web().HandleFunc("POST /update/auto-update", rt.handleAutoUpdateToggle)
		host.Web().HandleFunc("POST /update/rollback/{id}", rt.handleRollback)
	}
	if host.API().Enabled() {
		rt := newRuntime(host)
		host.API().HandleFunc("POST /api/update/check", rt.handleCheckUpdate)
		host.API().HandleFunc("POST /api/update/apply", rt.handleApplyUpdate)
		host.API().HandleFunc("GET /api/update/history", rt.handleUpdateHistory)
	}
	if host.Migrations().Enabled() {
		host.Migrations().Add(pluginhost.Migration{
			PluginID: "update",
			Version:  1,
			Name:     "create server_updates table",
			SQL: `
create table if not exists server_updates (
    id integer primary key,
    version text not null,
    released_at text not null,
    status text not null,
    notes text,
    applied_at text,
    created_at text not null default (datetime('now'))
);
`,
		})
	}
	return nil
}
