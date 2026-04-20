package access

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"insylus/internal/shared"
)

type runtime struct {
	store   store
	managed shared.ManagedAccountConfigProvider
	render  func(http.ResponseWriter, string, any)
}

type keysData struct {
	Keys []shared.SSHKey
}

type settingsData struct {
	AgentAutoUpdateDefault bool
	ManagedAccount         shared.ManagedAccountConfig
	ManagedGroupsText      string
}

func (rt runtime) handleSettingsPage(w http.ResponseWriter, r *http.Request) {
	autoUpdateDefault, err := rt.store.agentAutoUpdateDefault(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	managed, err := rt.managedAccountConfig(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rt.render(w, "settings.html", settingsData{
		AgentAutoUpdateDefault: autoUpdateDefault,
		ManagedAccount:         managed,
		ManagedGroupsText:      strings.Join(managed.ManagedGroups, ","),
	})
}

func (rt runtime) handleUpdateAgentAutoUpdateDefault(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	enabled := r.FormValue("agent_auto_update_default") == "on"
	if err := rt.store.setAgentAutoUpdateDefault(r.Context(), enabled); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, accessSettingsPath(r), http.StatusSeeOther)
}

func (rt runtime) handleUpdateManagedAccount(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cfg := shared.ManagedAccountConfig{
		ManagedUser:   r.FormValue("managed_user"),
		ManagedGroups: strings.Split(r.FormValue("managed_groups"), ","),
		AccessMode:    shared.AccessMode(r.FormValue("access_level")),
	}
	if err := rt.store.setManagedAccountConfig(r.Context(), cfg); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, accessSettingsPath(r), http.StatusSeeOther)
}

func (rt runtime) managedAccountConfig(r *http.Request) (shared.ManagedAccountConfig, error) {
	if rt.managed != nil {
		cfg, err := rt.managed.ManagedAccountConfig(r.Context())
		if err != nil {
			return shared.ManagedAccountConfig{}, err
		}
		if strings.TrimSpace(cfg.ManagedUser) != "" && len(cfg.ManagedGroups) > 0 {
			return cfg, nil
		}
	}
	return rt.store.managedAccountConfig(r.Context())
}

func (rt runtime) handleUpdatePolicy(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	enabled := r.FormValue("managed_account_enabled") == "on"
	mode := shared.AccessMode(r.FormValue("access_mode"))
	if !enabled {
		mode = shared.AccessModeDisabled
	} else if mode == "" || mode == shared.AccessModeDisabled {
		mode = shared.AccessModeAudit
	}
	if mode != shared.AccessModeDisabled && mode != shared.AccessModeAudit && mode != shared.AccessModeDocker && mode != shared.AccessModeSudoPrompted && mode != shared.AccessModeSudoPasswordless {
		http.Error(w, "invalid access mode", http.StatusBadRequest)
		return
	}
	keyID, err := parseOptionalInt64(r.FormValue("ssh_key_id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := rt.store.updatePolicy(r.Context(), r.PathValue("id"), enabled, mode, keyID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/devices/"+r.PathValue("id"), http.StatusSeeOther)
}

func (rt runtime) handleUpdateDeviceMode(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	mode := shared.DeviceMode(strings.TrimSpace(r.FormValue("device_mode")))
	if err := rt.store.setDeviceMode(r.Context(), r.PathValue("id"), mode); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/devices/"+r.PathValue("id"), http.StatusSeeOther)
}

func (rt runtime) handleUpdateDeviceAgentAutoUpdate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	override := shared.AgentAutoUpdateOverride(strings.TrimSpace(r.FormValue("agent_auto_update_override")))
	if err := rt.store.setAgentAutoUpdateOverride(r.Context(), r.PathValue("id"), override); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/devices/"+r.PathValue("id"), http.StatusSeeOther)
}

func (rt runtime) handleKeysPage(w http.ResponseWriter, r *http.Request) {
	keys, err := rt.store.listSSHKeys(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rt.render(w, "keys.html", keysData{Keys: keys})
}

func (rt runtime) handleKeysAPI(w http.ResponseWriter, r *http.Request) {
	keys, err := rt.store.listSSHKeys(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, keys)
}

func (rt runtime) handleCreateKey(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	publicKey := strings.TrimSpace(r.FormValue("public_key"))
	if name == "" || publicKey == "" {
		http.Error(w, "name and public key are required", http.StatusBadRequest)
		return
	}
	if _, err := rt.store.createSSHKey(r.Context(), name, publicKey); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/keys", http.StatusSeeOther)
}

func parseOptionalInt64(raw string) (*int64, error) {
	if raw == "" {
		return nil, nil
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func accessSettingsPath(r *http.Request) string {
	if strings.HasPrefix(r.URL.Path, "/access/settings") {
		return "/access/settings"
	}
	return "/settings"
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
