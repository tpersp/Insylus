package docker

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// runtime combines the store and the web renderer.
type runtime struct {
	store  store
	render func(http.ResponseWriter, string, any)
}

// --- API Handlers ---

// handleDockerNodes lists available Docker hosts.
func (rt runtime) handleDockerNodes(w http.ResponseWriter, r *http.Request) {
	configs, err := rt.store.listConfigs(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var nodes []DockerNode
	for _, cfg := range configs {
		client := NewDockerClient(cfg.DockerHost, cfg.SSHUser)
		hasDocker := client.Ping(r.Context()) == nil
		nodes = append(nodes, DockerNode{
			DeviceID:   cfg.DeviceID,
			DeviceName: cfg.DeviceName,
			Hostname:   cfg.Hostname,
			DockerHost: cfg.DockerHost,
			SSHUser:    cfg.SSHUser,
			HasDocker:  hasDocker,
		})
	}
	writeJSON(w, http.StatusOK, DockerNodeList{Nodes: nodes})
}

// handleContainers returns the list of containers for a device.
func (rt runtime) handleContainers(w http.ResponseWriter, r *http.Request) {
	deviceID := strings.TrimSpace(r.PathValue("device_id"))
	if deviceID == "" {
		http.Error(w, "device_id is required", http.StatusBadRequest)
		return
	}
	client, err := rt.dockerClientForDevice(r, deviceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	all := r.URL.Query().Get("all") == "true" || r.URL.Query().Get("all") == "1"
	var containers []Container
	if all {
		containers, err = client.ListAllContainers(r.Context())
	} else {
		containers, err = client.ListContainers(r.Context())
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, ContainerListResponse{
		Node:       deviceID,
		Containers: containers,
	})
}

// handleContainerInspect returns detailed container info.
func (rt runtime) handleContainerInspect(w http.ResponseWriter, r *http.Request) {
	deviceID := strings.TrimSpace(r.PathValue("device_id"))
	name := strings.TrimSpace(r.PathValue("name"))
	if deviceID == "" || name == "" {
		http.Error(w, "device_id and name are required", http.StatusBadRequest)
		return
	}
	client, err := rt.dockerClientForDevice(r, deviceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	detail, err := client.InspectContainer(r.Context(), name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

// handleContainerLogs returns container logs.
func (rt runtime) handleContainerLogs(w http.ResponseWriter, r *http.Request) {
	deviceID := strings.TrimSpace(r.PathValue("device_id"))
	name := strings.TrimSpace(r.PathValue("name"))
	if deviceID == "" || name == "" {
		http.Error(w, "device_id and name are required", http.StatusBadRequest)
		return
	}
	client, err := rt.dockerClientForDevice(r, deviceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	tail := 100
	if t := r.URL.Query().Get("tail"); t != "" {
		fmt.Sscanf(t, "%d", &tail)
	}
	timestamps := r.URL.Query().Get("timestamps") == "true"
	logs, err := client.ContainerLogs(r.Context(), name, tail, timestamps)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	entries := ParseLogOutput(logs)
	writeJSON(w, http.StatusOK, entries)
}

// handleContainerStats returns container stats.
func (rt runtime) handleContainerStats(w http.ResponseWriter, r *http.Request) {
	deviceID := strings.TrimSpace(r.PathValue("device_id"))
	name := strings.TrimSpace(r.PathValue("name"))
	if deviceID == "" || name == "" {
		http.Error(w, "device_id and name are required", http.StatusBadRequest)
		return
	}
	client, err := rt.dockerClientForDevice(r, deviceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	stats, err := client.ContainerStats(r.Context(), name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

// handleContainerAction handles start/stop/restart/pause/unpause.
func (rt runtime) handleContainerAction(w http.ResponseWriter, r *http.Request, action string) {
	deviceID := strings.TrimSpace(r.PathValue("device_id"))
	name := strings.TrimSpace(r.PathValue("name"))
	if deviceID == "" || name == "" {
		http.Error(w, "device_id and name are required", http.StatusBadRequest)
		return
	}
	client, err := rt.dockerClientForDevice(r, deviceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var actErr error
	switch action {
	case "start":
		actErr = client.StartContainer(r.Context(), name)
	case "stop":
		actErr = client.StopContainer(r.Context(), name)
	case "restart":
		actErr = client.RestartContainer(r.Context(), name)
	case "pause":
		actErr = client.PauseContainer(r.Context(), name)
	case "unpause":
		actErr = client.UnpauseContainer(r.Context(), name)
	}
	if actErr != nil {
		http.Error(w, actErr.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, ActionResult{
		Action:    action,
		Container: name,
		Status:    "ok",
		Message:   fmt.Sprintf("container %s completed", action),
	})
}

// handleImages returns the list of images for a device.
func (rt runtime) handleImages(w http.ResponseWriter, r *http.Request) {
	deviceID := strings.TrimSpace(r.PathValue("device_id"))
	if deviceID == "" {
		http.Error(w, "device_id is required", http.StatusBadRequest)
		return
	}
	client, err := rt.dockerClientForDevice(r, deviceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	images, err := client.ListImages(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, ImageListResponse{
		Node:   deviceID,
		Images: images,
	})
}

// --- Config Handlers ---

// handleDockerConfigList returns all Docker configs.
func (rt runtime) handleDockerConfigList(w http.ResponseWriter, r *http.Request) {
	configs, err := rt.store.listConfigs(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, configs)
}

// handleDockerConfigGet returns a single Docker config.
func (rt runtime) handleDockerConfigGet(w http.ResponseWriter, r *http.Request) {
	deviceID := strings.TrimSpace(r.PathValue("device_id"))
	if deviceID == "" {
		http.Error(w, "device_id is required", http.StatusBadRequest)
		return
	}
	config, err := rt.store.getConfigForDevice(r.Context(), deviceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, config)
}

// handleDockerConfigSet creates or updates a Docker config.
func (rt runtime) handleDockerConfigSet(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		var cfg dockerConfig
		if !decodeJSON(w, r, &cfg) {
			return
		}
		summary, err := rt.store.setConfig(r.Context(), cfg)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, summary)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cfg := dockerConfig{
		DeviceID:   strings.TrimSpace(r.FormValue("device_id")),
		DeviceName: strings.TrimSpace(r.FormValue("device_name")),
		SSHUser:    strings.TrimSpace(r.FormValue("ssh_user")),
		DockerHost: strings.TrimSpace(r.FormValue("docker_host")),
	}
	summary, err := rt.store.setConfig(r.Context(), cfg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if wantsHTML(r) {
		http.Redirect(w, r, "/docker", http.StatusSeeOther)
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

// handleDockerConfigDelete removes a Docker config.
func (rt runtime) handleDockerConfigDelete(w http.ResponseWriter, r *http.Request) {
	deviceID := strings.TrimSpace(r.PathValue("device_id"))
	if deviceID == "" {
		http.Error(w, "device_id is required", http.StatusBadRequest)
		return
	}
	if err := rt.store.deleteConfig(r.Context(), deviceID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if wantsHTML(r) {
		http.Redirect(w, r, "/docker", http.StatusSeeOther)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- Web Handlers ---

func (rt runtime) handleDockerPage(w http.ResponseWriter, r *http.Request) {
	targets, err := rt.store.targets.List(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rt.render(w, "docker.html", map[string]any{
		"Devices": targets,
	})
}

func (rt runtime) handleDeviceContainersPage(w http.ResponseWriter, r *http.Request) {
	deviceID := strings.TrimSpace(r.PathValue("device_id"))
	if deviceID == "" {
		http.Error(w, "device_id is required", http.StatusBadRequest)
		return
	}
	cfg, err := rt.store.getConfigForDevice(r.Context(), deviceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	client := NewDockerClient(cfg.DockerHost, cfg.SSHUser)
	containers, _ := client.ListAllContainers(r.Context())
	rt.render(w, "docker_containers.html", map[string]any{
		"Device":     cfg,
		"Containers": containers,
	})
}

// --- Helpers ---

func (rt runtime) dockerClientForDevice(r *http.Request, deviceID string) (*DockerClient, error) {
	cfg, err := rt.store.getConfigForDevice(r.Context(), deviceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("Docker host %q not found", deviceID)
		}
		return nil, err
	}
	if cfg.DockerHost == "" {
		return nil, fmt.Errorf("Docker host %q has no SSH host configured", deviceID)
	}
	return NewDockerClient(cfg.DockerHost, cfg.SSHUser), nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

// wantsHTML returns true if the client prefers HTML.
func wantsHTML(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "text/html")
}
