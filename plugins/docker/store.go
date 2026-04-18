package docker

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"insylus/internal/pluginhost"
)

// dockerConfig represents Docker connection configuration for a target.
type dockerConfig struct {
	DeviceID   string `json:"device_id"`
	DeviceName string `json:"device_name,omitempty"`
	SSHUser    string `json:"ssh_user"`
	DockerHost string `json:"docker_host"`
}

// configSummary is returned by API endpoints.
type configSummary struct {
	DeviceID   string `json:"device_id"`
	DeviceName string `json:"device_name"`
	Hostname   string `json:"hostname"`
	SSHUser    string `json:"ssh_user"`
	DockerHost string `json:"docker_host"`
}

type store struct {
	db        pluginhost.DBHost
	secrets   pluginhost.SecretHost
	inventory pluginhost.InventoryService
	targets   pluginhost.TargetService
}

func newStore(host pluginhost.Host) store {
	return store{
		db:        host.DB(),
		secrets:   host.Secrets(),
		inventory: host.Data().Inventory(),
		targets:   host.Targets(),
	}
}

// getConfig returns the Docker config for a device.
func (s store) getConfig(ctx context.Context, deviceID string) (dockerConfig, error) {
	var cfg dockerConfig
	err := s.db.QueryRowContext(ctx,
		"select device_id, ssh_user, docker_host from docker_config where device_id = ?",
		deviceID,
	).Scan(&cfg.DeviceID, &cfg.SSHUser, &cfg.DockerHost)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return dockerConfig{DeviceID: deviceID}, sql.ErrNoRows
		}
		return dockerConfig{}, err
	}
	return cfg, nil
}

// getConfigForDevice returns config with device name populated.
func (s store) getConfigForDevice(ctx context.Context, deviceID string) (configSummary, error) {
	cfg, err := s.getConfig(ctx, deviceID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return configSummary{}, err
	}
	target, err := s.targets.Get(ctx, deviceID)
	if err != nil {
		return configSummary{}, err
	}
	host := firstNonEmpty(cfg.DockerHost, target.SSHHost, target.Hostname, target.Name)
	return configSummary{
		DeviceID:   cfg.DeviceID,
		DeviceName: target.Name,
		Hostname:   target.Hostname,
		SSHUser:    firstNonEmpty(cfg.SSHUser, target.SSHUser),
		DockerHost: host,
	}, nil
}

// setConfig creates or updates the Docker config for a device.
func (s store) setConfig(ctx context.Context, cfg dockerConfig) (configSummary, error) {
	cfg.DeviceID = strings.TrimSpace(cfg.DeviceID)
	cfg.SSHUser = strings.TrimSpace(cfg.SSHUser)
	cfg.DockerHost = strings.TrimSpace(cfg.DockerHost)

	target, err := s.resolveOrCreateTarget(ctx, cfg)
	if err != nil {
		return configSummary{}, err
	}
	cfg.DeviceID = target.ID
	cfg.DockerHost = firstNonEmpty(cfg.DockerHost, target.SSHHost, target.Hostname, target.Name)

	_, err = s.db.ExecContext(ctx, `
		insert into docker_config (device_id, ssh_user, docker_host, created_at, updated_at)
		values (?, ?, ?, datetime('now'), datetime('now'))
		on conflict(device_id) do update set
			ssh_user = excluded.ssh_user,
			docker_host = excluded.docker_host,
			updated_at = datetime('now')`,
		cfg.DeviceID, cfg.SSHUser, cfg.DockerHost)
	if err != nil {
		return configSummary{}, err
	}

	return configSummary{
		DeviceID:   cfg.DeviceID,
		DeviceName: target.Name,
		Hostname:   target.Hostname,
		SSHUser:    firstNonEmpty(cfg.SSHUser, target.SSHUser),
		DockerHost: cfg.DockerHost,
	}, nil
}

func (s store) resolveOrCreateTarget(ctx context.Context, cfg dockerConfig) (pluginhost.Target, error) {
	if cfg.DeviceID != "" {
		return s.targets.Get(ctx, cfg.DeviceID)
	}
	name := strings.TrimSpace(cfg.DeviceName)
	if name == "" {
		name = strings.TrimSpace(cfg.DockerHost)
	}
	if name == "" {
		return pluginhost.Target{}, errors.New("device_id, device_name, or docker_host is required")
	}
	if matches, err := s.targets.Find(ctx, name); err == nil && len(matches) == 1 {
		return matches[0], nil
	}
	return s.targets.Create(ctx, pluginhost.TargetInput{
		Name:      name,
		Kind:      "docker-host",
		Hostname:  cfg.DockerHost,
		SSHHost:   cfg.DockerHost,
		SSHUser:   cfg.SSHUser,
		CreatedBy: "docker",
	})
}

// deleteConfig removes the Docker config for a device.
func (s store) deleteConfig(ctx context.Context, deviceID string) error {
	_, err := s.db.ExecContext(ctx, "delete from docker_config where device_id = ?", deviceID)
	return err
}

// listConfigs returns all Docker configs with device names.
func (s store) listConfigs(ctx context.Context) ([]configSummary, error) {
	rows, err := s.db.QueryContext(ctx, "select device_id, ssh_user, docker_host from docker_config order by device_id collate nocase")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var configs []dockerConfig
	for rows.Next() {
		var cfg dockerConfig
		if err := rows.Scan(&cfg.DeviceID, &cfg.SSHUser, &cfg.DockerHost); err != nil {
			return nil, err
		}
		configs = append(configs, cfg)
	}

	targets, err := s.targets.List(ctx)
	if err != nil {
		return nil, err
	}
	targetMap := make(map[string]pluginhost.Target)
	for _, target := range targets {
		targetMap[target.ID] = target
	}

	var results []configSummary
	for _, cfg := range configs {
		target := targetMap[cfg.DeviceID]
		results = append(results, configSummary{
			DeviceID:   cfg.DeviceID,
			DeviceName: firstNonEmpty(target.Name, cfg.DeviceName, cfg.DeviceID),
			Hostname:   target.Hostname,
			SSHUser:    firstNonEmpty(cfg.SSHUser, target.SSHUser),
			DockerHost: firstNonEmpty(cfg.DockerHost, target.SSHHost, target.Hostname, target.Name),
		})
	}
	return results, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
