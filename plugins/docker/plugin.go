package docker

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
	return "docker"
}

func (Plugin) Name() string {
	return "Docker"
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
		rt := runtime{store: newStore(host), render: host.Web().Render}
		host.Web().NavItem(pluginhost.NavItem{
			Label: "Docker",
			Href:  "/docker",
			Order: 55,
		})
		host.Web().Templates(templateFS, "templates/*.html")
		host.Web().Static("/plugin-assets/docker/", staticFS)
		host.Web().HandleFunc("GET /docker", rt.handleDockerPage)
		host.Web().HandleFunc("GET /docker/devices/{device_id}", rt.handleDeviceContainersPage)
		host.Web().HandleFunc("POST /docker/config", rt.handleDockerConfigSet)
		host.Web().HandleFunc("POST /docker/config/{device_id}/delete", rt.handleDockerConfigDelete)
	}
	if host.API().Enabled() {
		rt := runtime{store: newStore(host)}
		host.API().HandleFunc("GET /api/docker/nodes", rt.handleDockerNodes)
		host.API().HandleFunc("GET /api/docker/config", rt.handleDockerConfigList)
		host.API().HandleFunc("GET /api/docker/config/{device_id}", rt.handleDockerConfigGet)
		host.API().HandleFunc("POST /api/docker/config", rt.handleDockerConfigSet)
		host.API().HandleFunc("POST /api/docker/config/{device_id}/delete", rt.handleDockerConfigDelete)
		host.API().HandleFunc("GET /api/docker/containers/{device_id}", rt.handleContainers)
		host.API().HandleFunc("GET /api/docker/containers/{device_id}/{name}/inspect", rt.handleContainerInspect)
		host.API().HandleFunc("GET /api/docker/containers/{device_id}/{name}/logs", rt.handleContainerLogs)
		host.API().HandleFunc("GET /api/docker/containers/{device_id}/{name}/stats", rt.handleContainerStats)
		host.API().HandleFunc("POST /api/docker/containers/{device_id}/{name}/start", func(w http.ResponseWriter, r *http.Request) { rt.handleContainerAction(w, r, "start") })
		host.API().HandleFunc("POST /api/docker/containers/{device_id}/{name}/stop", func(w http.ResponseWriter, r *http.Request) { rt.handleContainerAction(w, r, "stop") })
		host.API().HandleFunc("POST /api/docker/containers/{device_id}/{name}/restart", func(w http.ResponseWriter, r *http.Request) { rt.handleContainerAction(w, r, "restart") })
		host.API().HandleFunc("POST /api/docker/containers/{device_id}/{name}/pause", func(w http.ResponseWriter, r *http.Request) { rt.handleContainerAction(w, r, "pause") })
		host.API().HandleFunc("POST /api/docker/containers/{device_id}/{name}/unpause", func(w http.ResponseWriter, r *http.Request) { rt.handleContainerAction(w, r, "unpause") })
		host.API().HandleFunc("GET /api/docker/images/{device_id}", rt.handleImages)
	}
	if host.Migrations().Enabled() {
		host.Migrations().Add(pluginhost.Migration{
			PluginID: "docker",
			Version:  1,
			Name:     "create docker_config table",
			SQL: `
create table if not exists docker_config (
	device_id text primary key references targets(id) on delete cascade,
	ssh_user text not null default '',
	docker_host text not null default '',
	created_at text not null,
	updated_at text not null
);
`,
		})
	}
	return nil
}
