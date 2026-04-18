package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"insylus/internal/pluginhost"
)

type capabilityRegistry struct {
	mu       sync.RWMutex
	services map[string]any
}

func newCapabilityRegistry() *capabilityRegistry {
	return &capabilityRegistry{services: map[string]any{}}
}

func (r *capabilityRegistry) Provide(name string, service any) {
	name = strings.TrimSpace(name)
	if name == "" || service == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.services[name] = service
}

func (r *capabilityRegistry) Lookup(name string) (any, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	service, ok := r.services[name]
	return service, ok
}

func (r *capabilityRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.services))
	for name := range r.services {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

type pluginRuntime struct {
	app       *App
	plugins   []pluginhost.Plugin
	manifests map[string]pluginhost.PluginManifest
}

func newPluginRuntime(app *App, plugins []pluginhost.Plugin) pluginRuntime {
	rt := pluginRuntime{
		app:       app,
		plugins:   append([]pluginhost.Plugin(nil), plugins...),
		manifests: map[string]pluginhost.PluginManifest{},
	}
	for _, plugin := range plugins {
		manifest := manifestForPlugin(plugin)
		rt.manifests[manifest.ID] = manifest
	}
	return rt
}

func manifestForPlugin(plugin pluginhost.Plugin) pluginhost.PluginManifest {
	if provider, ok := plugin.(pluginhost.ManifestProvider); ok {
		manifest := provider.Manifest()
		if manifest.ID == "" {
			manifest.ID = plugin.ID()
		}
		if manifest.Name == "" {
			manifest.Name = plugin.Name()
		}
		return manifest
	}
	return pluginhost.PluginManifest{
		ID:       plugin.ID(),
		Name:     plugin.Name(),
		Version:  "dev",
		Provides: []string{"plugin." + plugin.ID()},
	}
}

func (rt pluginRuntime) Available() []pluginhost.PluginManifest {
	out := make([]pluginhost.PluginManifest, 0, len(rt.plugins))
	for _, plugin := range rt.plugins {
		manifest := rt.manifests[plugin.ID()]
		manifest.Enabled = rt.Enabled(plugin.ID())
		out = append(out, manifest)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

func (rt pluginRuntime) Enabled(pluginID string) bool {
	enabled, err := rt.app.store.PluginEnabled(context.Background(), pluginID)
	return err == nil && enabled
}

func (rt pluginRuntime) Enable(ctx context.Context, pluginID string) error {
	if _, ok := rt.manifests[pluginID]; !ok {
		return fmt.Errorf("unknown plugin %q", pluginID)
	}
	return rt.app.store.SetPluginEnabled(ctx, pluginID, true)
}

func (rt pluginRuntime) Disable(ctx context.Context, pluginID string) error {
	if _, ok := rt.manifests[pluginID]; !ok {
		return fmt.Errorf("unknown plugin %q", pluginID)
	}
	return rt.app.store.SetPluginEnabled(ctx, pluginID, false)
}

func (rt pluginRuntime) Purge(ctx context.Context, pluginID string) error {
	if _, ok := rt.manifests[pluginID]; !ok {
		return fmt.Errorf("unknown plugin %q", pluginID)
	}
	return rt.app.store.PurgePlugin(ctx, pluginID)
}

func (rt pluginRuntime) Profiles() []pluginhost.PluginProfile {
	return pluginProfiles()
}

func (rt pluginRuntime) ApplyProfile(ctx context.Context, name string) error {
	profile, ok := pluginProfile(name)
	if !ok {
		return fmt.Errorf("unknown profile %q", name)
	}
	enabled := map[string]struct{}{}
	for _, id := range profile.PluginIDs {
		if _, ok := rt.manifests[id]; !ok {
			continue
		}
		enabled[id] = struct{}{}
	}
	for id := range rt.manifests {
		_, shouldEnable := enabled[id]
		if err := rt.app.store.SetPluginEnabled(ctx, id, shouldEnable); err != nil {
			return err
		}
	}
	return nil
}

func pluginProfiles() []pluginhost.PluginProfile {
	return []pluginhost.PluginProfile{
		{Name: "minimal", Description: "Core only", PluginIDs: []string{"dashboard", "help"}},
		{Name: "devices", Description: "Manual target inventory", PluginIDs: []string{"dashboard", "help", "devices"}},
		{Name: "agent-inventory", Description: "Devices and agent inventory", PluginIDs: []string{"dashboard", "help", "devices", "agent"}},
		{Name: "docker", Description: "Docker control only", PluginIDs: []string{"dashboard", "help", "docker"}},
		{Name: "proxmox", Description: "Proxmox control only", PluginIDs: []string{"dashboard", "help", "proxmox"}},
		{Name: "media", Description: "Jellyfin media only", PluginIDs: []string{"dashboard", "help", "jellyfin"}},
		{Name: "homelab", Description: "Inventory, agent, services, topology, wake, Docker, Proxmox, and Jellyfin", PluginIDs: []string{"dashboard", "help", "devices", "agent", "services", "topology", "wake", "docker", "proxmox", "jellyfin"}},
		{Name: "managed-homelab", Description: "Homelab profile plus managed access", PluginIDs: []string{"dashboard", "help", "devices", "agent", "services", "topology", "wake", "docker", "proxmox", "jellyfin", "access"}},
	}
}

func pluginProfile(name string) (pluginhost.PluginProfile, bool) {
	for _, profile := range pluginProfiles() {
		if profile.Name == name {
			return profile, true
		}
	}
	return pluginhost.PluginProfile{}, false
}

func (s *Store) EnsurePluginSettings(ctx context.Context, plugins []pluginhost.Plugin) error {
	var count int
	if err := s.db.QueryRowContext(ctx, `select count(*) from plugin_settings`).Scan(&count); err != nil {
		return err
	}
	defaultEnabled := map[string]struct{}{}
	profileName := "homelab"
	if count > 0 {
		profileName = ""
	}
	if profileName != "" {
		if profile, ok := pluginProfile(profileName); ok {
			for _, id := range profile.PluginIDs {
				defaultEnabled[id] = struct{}{}
			}
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, plugin := range plugins {
		_, enable := defaultEnabled[plugin.ID()]
		if count > 0 {
			enable = false
		}
		if plugin.ID() == "dashboard" {
			enable = true
		}
		if _, err := s.db.ExecContext(ctx, `
			insert into plugin_settings (plugin_id, enabled, settings_json, created_at, updated_at)
			values (?, ?, '{}', ?, ?)
			on conflict(plugin_id) do nothing`,
			plugin.ID(), boolInt(enable), now, now); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) PluginEnabled(ctx context.Context, pluginID string) (bool, error) {
	var enabled int
	err := s.db.QueryRowContext(ctx, `select enabled from plugin_settings where plugin_id = ?`, strings.TrimSpace(pluginID)).Scan(&enabled)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return enabled == 1, nil
}

func (s *Store) SetPluginEnabled(ctx context.Context, pluginID string, enabled bool) error {
	pluginID = strings.TrimSpace(pluginID)
	if pluginID == "" {
		return fmt.Errorf("plugin_id is required")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		insert into plugin_settings (plugin_id, enabled, settings_json, created_at, updated_at)
		values (?, ?, '{}', ?, ?)
		on conflict(plugin_id) do update set
			enabled = excluded.enabled,
			updated_at = excluded.updated_at`,
		pluginID, boolInt(enabled), now, now)
	return err
}

func (s *Store) PurgePlugin(ctx context.Context, pluginID string) error {
	pluginID = strings.TrimSpace(pluginID)
	if pluginID == "" {
		return fmt.Errorf("plugin_id is required")
	}
	_, err := s.db.ExecContext(ctx, `
		delete from plugin_secrets where plugin_id = ?;
		delete from target_metadata where plugin_id = ?;
		update plugin_settings set settings_json = '{}', updated_at = datetime('now') where plugin_id = ?;`,
		pluginID, pluginID, pluginID)
	return err
}

func (s *Store) pluginSetting(ctx context.Context, pluginID string) (map[string]any, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `select settings_json from plugin_settings where plugin_id = ?`, pluginID).Scan(&raw)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &out)
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}
