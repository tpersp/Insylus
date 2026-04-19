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
	"strings"
	"time"

	"insylus/internal/pluginhost"
	"insylus/internal/shared"
)

type runtime struct {
	db                           pluginhost.DBHost
	targets                      pluginhost.TargetService
	render                       func(http.ResponseWriter, string, any)
	managedAccountConfigProvider shared.ManagedAccountConfigProvider
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
	ipsJSON, _ := json.Marshal(req.Health.IPs)
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := rt.db.ExecContext(r.Context(), `
		update devices set hostname = ?, os_name = ?, ips_json = ?, agent_version = ?, last_seen_at = ?, updated_at = ?
		where id = ?`, req.Health.Hostname, req.Health.OSName, string(ipsJSON), req.Health.AgentVersion, now, now, targetID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
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
	managed, err := rt.managedAccountConfig(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	managedUser := managed.ManagedUser
	policy := shared.AgentPolicyResponse{
		DeviceID:              targetID,
		DeviceMode:            shared.DeviceModeInventoryOnly,
		ManagedAccountEnabled: false,
		AccessMode:            managed.AccessMode,
		AccountState:          "unmanaged",
		PolicyRevision:        1,
		FetchedAt:             time.Now().UTC(),
		AgentUpdate:           shared.AgentUpdateManifest{Enabled: false, Status: shared.AgentUpdateStatusIdle},
		ManagedUser:           managedUser,
		ManagedGroups:         managed.ManagedGroups,
		SudoersPath:           "/etc/sudoers.d/insylus-" + managedUser,
		AuditReadmePath:       "/etc/sudoers.d/insylus-" + managedUser + "-audit-readme",
		AuthorizedKeysPath:    "/home/" + managedUser + "/.ssh/authorized_keys",
	}
	var enabled int
	var keyID sql.NullInt64
	var publicKey, fingerprint string
	err = rt.db.QueryRowContext(r.Context(), `
		select coalesce(p.device_mode, 'inventory-only'), p.managed_account_enabled, p.ssh_key_id, coalesce(k.public_key, ''), coalesce(k.fingerprint, ''), p.policy_revision
		from device_access_policies p
		left join ssh_keys k on k.id = p.ssh_key_id
		where p.device_id = ?`, targetID).Scan(&policy.DeviceMode, &enabled, &keyID, &publicKey, &fingerprint, &policy.PolicyRevision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if keyID.Valid {
		id := keyID.Int64
		policy.AssignedKeyID = &id
	}
	policy.ManagedAccountEnabled = enabled == 1
	policy.AssignedKey = publicKey
	policy.KeyFingerprint = fingerprint
	policy.AccessMode = managed.AccessMode
	if policy.DeviceMode == shared.DeviceModeInventoryOnly {
		policy.AccountState = "unmanaged"
	} else if !policy.ManagedAccountEnabled || policy.AccessMode == shared.AccessModeDisabled {
		policy.AccountState = "disabled"
	} else {
		policy.AccountState = "enabled"
	}
	writeJSON(w, http.StatusOK, policy)
}

func (rt runtime) handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := rt.authenticate(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

func (rt runtime) handleReport(w http.ResponseWriter, r *http.Request) {
	if _, ok := rt.authenticate(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

func (rt runtime) handleDownload(w http.ResponseWriter, r *http.Request) {
	path := "/opt/insylus/bin/insylus-agent"
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

func methodNotAllowed(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
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
