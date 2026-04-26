package agent

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"insylus/internal/httpx"
	"insylus/internal/pluginhost"
	"insylus/internal/shared"
)

type runtime struct {
	db                           pluginhost.DBHost
	targets                      pluginhost.TargetService
	render                       func(http.ResponseWriter, string, any)
	managedAccountConfigProvider shared.ManagedAccountConfigProvider
	controller                   pluginhost.AgentControllerService
}

type agentSettingsData struct {
	AgentAutoUpdateDefault bool
}

func (rt runtime) handleAgentSettingsPage(w http.ResponseWriter, r *http.Request) {
	enabled, err := rt.agentAutoUpdateDefault(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rt.render(w, "agent_settings.html", agentSettingsData{AgentAutoUpdateDefault: enabled})
}

func (rt runtime) handleUpdateAgentAutoUpdateDefault(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := rt.setAgentAutoUpdateDefault(r.Context(), r.FormValue("agent_auto_update_default") == "on"); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/agent/settings", http.StatusSeeOther)
}

func (rt runtime) handleUpdateDeviceAgentAutoUpdate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	override := shared.AgentAutoUpdateOverride(strings.TrimSpace(r.FormValue("agent_auto_update_override")))
	if err := rt.setDeviceAgentAutoUpdateOverride(r.Context(), r.PathValue("id"), override); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/devices/"+r.PathValue("id"), http.StatusSeeOther)
}

func (rt runtime) handleInstallPage(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/devices/"+r.PathValue("id")+"/install", http.StatusSeeOther)
}

func (rt runtime) handleInstallScript(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		http.Error(w, "missing token", http.StatusBadRequest)
		return
	}
	baseURL := "http://" + r.Host
	if r.TLS != nil {
		baseURL = "https://" + r.Host
	}
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	_, _ = w.Write([]byte(`#!/usr/bin/env bash
set -euo pipefail
SERVER_URL="` + baseURL + `"
BOOTSTRAP_TOKEN="` + token + `"
TMP_BIN="$(mktemp)"
curl -fsSL "$SERVER_URL/downloads/insylus-agent" -o "$TMP_BIN"
chmod +x "$TMP_BIN"
"$TMP_BIN" install --server "$SERVER_URL" --bootstrap-token "$BOOTSTRAP_TOKEN"
rm -f "$TMP_BIN"
`))
}

func (rt runtime) handleBootstrapRegister(w http.ResponseWriter, r *http.Request) {
	var req shared.BootstrapRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	var targetID, agentToken string
	row := rt.db.QueryRowContext(r.Context(), `select id, agent_token from devices where bootstrap_token = ?`, req.BootstrapToken)
	if err := row.Scan(&targetID, &agentToken); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "unknown bootstrap token", http.StatusBadRequest)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if agentToken == "" {
		agentToken = randomToken(32)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := rt.db.ExecContext(r.Context(), `
		update devices set agent_token = ?, hostname = ?, os_name = ?, agent_version = ?, updated_at = ?
		where id = ?`, agentToken, req.Hostname, req.OSName, req.AgentVersion, now, targetID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	target, err := rt.targets.Get(r.Context(), targetID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := rt.targets.Update(r.Context(), targetID, pluginhost.TargetInput{Name: target.Name, Hostname: req.Hostname, Kind: target.Kind}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, shared.BootstrapResponse{DeviceID: targetID, AgentToken: agentToken, Interval: shared.AgentCheckInInterval.String()})
}

func (rt runtime) handleCheckIn(w http.ResponseWriter, r *http.Request) {
	targetID, ok := rt.authenticate(w, r)
	if !ok {
		return
	}
	var req shared.CheckInRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if rt.controller != nil {
		if err := rt.controller.SaveCheckIn(r.Context(), targetID, req.Health, req.AgentInstall); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		ipsJSON, _ := json.Marshal(req.Health.IPs)
		now := time.Now().UTC().Format(time.RFC3339)
		if _, err := rt.db.ExecContext(r.Context(), `
			update devices set hostname = ?, os_name = ?, ips_json = ?, agent_version = ?, last_seen_at = ?, updated_at = ?
			where id = ?`, req.Health.Hostname, req.Health.OSName, string(ipsJSON), req.Health.AgentVersion, now, now, targetID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	target, err := rt.targets.Get(r.Context(), targetID)
	if err == nil {
		if _, err := rt.targets.Update(r.Context(), targetID, pluginhost.TargetInput{Name: target.Name, Kind: firstNonEmpty(target.Kind, "linux-host"), Hostname: req.Health.Hostname, IPs: req.Health.IPs, Note: target.Note, Tags: target.Tags}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (rt runtime) handlePolicyFetch(w http.ResponseWriter, r *http.Request) {
	targetID, ok := rt.authenticate(w, r)
	if !ok {
		return
	}
	if rt.controller == nil {
		http.Error(w, "agent controller service unavailable", http.StatusInternalServerError)
		return
	}
	device, err := rt.deviceByID(r.Context(), targetID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	baseURL := "http://" + r.Host
	if r.TLS != nil {
		baseURL = "https://" + r.Host
	}
	policy, err := rt.controller.PolicyForDevice(r.Context(), baseURL, device, r.URL.Query().Get("goos"), r.URL.Query().Get("goarch"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, policy)
}

func (rt runtime) handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := rt.authenticate(w, r)
	if !ok {
		return
	}
	if rt.controller == nil {
		http.Error(w, "agent controller service unavailable", http.StatusInternalServerError)
		return
	}
	var report shared.AgentUpdateReport
	if !decodeJSON(w, r, &report) {
		return
	}
	if err := rt.controller.SaveAgentUpdateStatus(r.Context(), deviceID, report); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

func (rt runtime) handleReport(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	token = strings.TrimSpace(token)
	if token == "" {
		http.Error(w, "missing bearer token", http.StatusUnauthorized)
		return
	}
	if rt.controller == nil {
		http.Error(w, "agent controller service unavailable", http.StatusInternalServerError)
		return
	}
	var report shared.DeviceReport
	if !decodeJSON(w, r, &report) {
		return
	}
	if err := rt.controller.SaveReport(r.Context(), token, report); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

func (rt runtime) handleDownload(w http.ResponseWriter, r *http.Request) {
	path, err := resolveServedAgentBinaryPath(defaultServedAgentBinaryPath(), r.URL.Query().Get("goos"), r.URL.Query().Get("goarch"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if _, err := os.Stat(path); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	http.ServeFile(w, r, path)
}

func (rt runtime) authenticate(w http.ResponseWriter, r *http.Request) (string, bool) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	token = strings.TrimSpace(token)
	if token == "" {
		http.Error(w, "missing bearer token", http.StatusUnauthorized)
		return "", false
	}
	var targetID string
	if err := rt.db.QueryRowContext(r.Context(), `select id from devices where agent_token = ?`, token).Scan(&targetID); err != nil {
		http.Error(w, "invalid agent token", http.StatusUnauthorized)
		return "", false
	}
	return targetID, true
}

func methodNotAllowed(w http.ResponseWriter, r *http.Request) {
	httpx.MethodNotAllowed(w, r)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	return httpx.DecodeJSON(w, r, dst)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	httpx.WriteJSON(w, status, v)
}

func randomToken(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (rt runtime) deviceByID(ctx context.Context, deviceID string) (shared.Device, error) {
	row := rt.db.QueryRowContext(ctx, `
		select id, name, bootstrap_token, agent_token, hostname, os_name, ips_json, agent_version, last_seen_at, created_at, updated_at
		from devices where id = ?`, deviceID)
	var device shared.Device
	var ipsJSON string
	var lastSeen, created, updated sql.NullString
	if err := row.Scan(&device.ID, &device.Name, &device.BootstrapToken, &device.AgentToken, &device.Hostname, &device.OSName, &ipsJSON, &device.AgentVersion, &lastSeen, &created, &updated); err != nil {
		return shared.Device{}, err
	}
	_ = json.Unmarshal([]byte(ipsJSON), &device.IPs)
	if lastSeen.Valid {
		device.LastSeenAt, _ = time.Parse(time.RFC3339, lastSeen.String)
	}
	if created.Valid {
		device.CreatedAt, _ = time.Parse(time.RFC3339, created.String)
	}
	if updated.Valid {
		device.UpdatedAt, _ = time.Parse(time.RFC3339, updated.String)
	}
	return device, nil
}

func defaultServedAgentBinaryPath() string {
	if value := strings.TrimSpace(os.Getenv("INSYLUS_AGENT_BINARY_PATH")); value != "" {
		return value
	}
	return "/opt/insylus/bin/insylus-agent"
}

func resolveServedAgentBinaryPath(defaultPath, goos, goarch string) (string, error) {
	goos = strings.TrimSpace(goos)
	goarch = strings.TrimSpace(goarch)
	if goos == "" || goarch == "" {
		return defaultPath, nil
	}
	baseDir := filepath.Dir(defaultPath)
	candidate := filepath.Join(baseDir, "insylus-agent-"+goos+"-"+goarch)
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}
	return "", os.ErrNotExist
}

func defaultManagedUser() string {
	if value := strings.TrimSpace(os.Getenv("INSYLUS_MANAGED_USER")); value != "" {
		return value
	}
	return shared.DefaultManagedUser
}

func (rt runtime) managedAccountConfig(ctx context.Context) (shared.ManagedAccountConfig, error) {
	if rt.managedAccountConfigProvider != nil {
		cfg, err := rt.managedAccountConfigProvider.ManagedAccountConfig(ctx)
		if err != nil {
			return shared.ManagedAccountConfig{}, err
		}
		if strings.TrimSpace(cfg.ManagedUser) != "" && len(cfg.ManagedGroups) > 0 {
			return cfg, nil
		}
	}
	return shared.ManagedAccountConfig{ManagedUser: defaultManagedUser(), ManagedGroups: defaultManagedGroups()}, nil
}

func defaultManagedGroups() []string {
	raw := strings.TrimSpace(os.Getenv("INSYLUS_MANAGED_GROUPS"))
	if raw == "" {
		return []string{"adm", "systemd-journal"}
	}
	parts := strings.Split(raw, ",")
	groups := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			groups = append(groups, part)
		}
	}
	if len(groups) == 0 {
		return []string{"adm", "systemd-journal"}
	}
	return groups
}

func (rt runtime) agentAutoUpdateDefault(ctx context.Context) (bool, error) {
	row := rt.db.QueryRowContext(ctx, `select value from app_settings where key = 'agent_auto_update_default'`)
	var value string
	if err := row.Scan(&value); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return strings.EqualFold(value, "true") || value == "1", nil
}

func (rt runtime) setAgentAutoUpdateDefault(ctx context.Context, enabled bool) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := rt.db.ExecContext(ctx, `
		insert into app_settings (key, value, updated_at)
		values ('agent_auto_update_default', ?, ?)
		on conflict(key) do update set value = excluded.value, updated_at = excluded.updated_at`, boolString(enabled), now); err != nil {
		return err
	}
	_, err := rt.db.ExecContext(ctx, `
		update device_agent_updates
		set effective_enabled = ?, updated_at = ?
		where auto_update_override = 'inherit'`, boolInt(enabled), now)
	return err
}

func (rt runtime) setDeviceAgentAutoUpdateOverride(ctx context.Context, deviceID string, override shared.AgentAutoUpdateOverride) error {
	if override == "" {
		override = shared.AgentAutoUpdateInherit
	}
	if override != shared.AgentAutoUpdateInherit && override != shared.AgentAutoUpdateEnabled && override != shared.AgentAutoUpdateDisabled {
		return errors.New("invalid auto-update override")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := rt.db.ExecContext(ctx, `
		update device_agent_updates
		set auto_update_override = ?, updated_at = ?
		where device_id = ?`, string(override), now, deviceID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	globalEnabled, err := rt.agentAutoUpdateDefault(ctx)
	if err != nil {
		return err
	}
	effective := globalEnabled
	switch override {
	case shared.AgentAutoUpdateEnabled:
		effective = true
	case shared.AgentAutoUpdateDisabled:
		effective = false
	}
	_, err = rt.db.ExecContext(ctx, `
		update device_agent_updates
		set effective_enabled = ?, updated_at = ?
		where device_id = ?`, boolInt(effective), now, deviceID)
	return err
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
