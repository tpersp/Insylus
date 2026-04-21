package monitor

import (
	"embed"
	"io/fs"

	"insylus/internal/pluginhost"
)

//go:embed templates/*.html static/*
var files embed.FS

type Plugin struct{}

func New() pluginhost.Plugin {
	return Plugin{}
}

func (Plugin) ID() string {
	return "monitor"
}

func (Plugin) Name() string {
	return "Monitor"
}

func (Plugin) Register(host pluginhost.Host) error {
	if host.CLI().Enabled() {
		host.CLI().AddCommand(command())
	}
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
			PluginID: "monitor",
			Label:    "Monitor",
			Href:     "/monitor",
			Order:    40,
		})
		host.Web().Templates(templateFS, "templates/*.html")
		host.Web().Static("/plugin-assets/monitor/", staticFS)
		host.Web().HandleFunc("GET /monitor", rt.handlePage)
		host.Web().HandleFunc("POST /monitor/check", rt.handleCheckNow)
		host.Web().HandleFunc("POST /monitor/settings", rt.handleSettingsForm)
		host.Web().HandleFunc("POST /monitor/targets", rt.handleManualTargetForm)
		host.Web().HandleFunc("POST /monitor/targets/{id}/delete", rt.handleManualTargetDelete)
	}
	if host.API().Enabled() {
		rt := newRuntime(host)
		host.API().HandleFunc("GET /api/monitor", rt.handleListAPI)
		host.API().HandleFunc("GET /api/monitor/{key}/history", rt.handleHistoryAPI)
		host.API().HandleFunc("POST /api/monitor/check", rt.handleCheckNowAPI)
		host.API().HandleFunc("POST /api/monitor/settings", rt.handleSettingsAPI)
		host.API().HandleFunc("POST /api/monitor/targets", rt.handleManualTargetAPI)
		host.API().HandleFunc("POST /api/monitor/targets/{id}/delete", rt.handleManualTargetDelete)
		rt.startLoop()
	}
	if host.Migrations().Enabled() {
		host.Migrations().Add(pluginhost.Migration{
			PluginID: "monitor",
			Version:  1,
			Name:     "create monitor tables",
			SQL: `
create table if not exists monitor_settings (
	key text primary key,
	value text not null,
	updated_at text not null
);
create table if not exists monitor_manual_targets (
	id integer primary key,
	name text not null,
	host text not null,
	port integer not null default 0,
	enabled integer not null default 1,
	created_at text not null,
	updated_at text not null
);
create table if not exists monitor_samples (
	id integer primary key,
	target_key text not null,
	target_name text not null default '',
	source text not null,
	device_id text not null default '',
	host text not null default '',
	port integer not null default 0,
	success integer not null default 0,
	latency_ms real not null default 0,
	error text not null default '',
	checked_at text not null
);
create index if not exists monitor_samples_target_checked_idx on monitor_samples(target_key, checked_at desc);
create index if not exists monitor_samples_checked_idx on monitor_samples(checked_at desc);
`,
		})
	}
	return nil
}
