package server

import (
	"net/http"
	"strings"
	"time"

	"insylus/internal/pluginhost"
	"insylus/internal/shared"
)

type layoutData struct {
	Title string
	Body  string
	Data  any
}

type installData struct {
	BaseURL string
	Target  pluginhost.Target
	Command string
}

type uninstallData struct {
	BaseURL        string
	Target         pluginhost.Target
	Command        string
	Exact          bool
	ServiceName    string
	BinaryPath     string
	ConfigPath     string
	UnitPath       string
	LastReportedAt *time.Time
}

type accessSettingsData struct {
	ManagedAccount    shared.ManagedAccountConfig
	ManagedGroupsText string
}

func (a *App) handleAccessSettingsPage(w http.ResponseWriter, r *http.Request) {
	managed, err := a.ManagedAccountConfig(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, "access_settings.html", accessSettingsData{
		ManagedAccount:    managed,
		ManagedGroupsText: strings.Join(managed.ManagedGroups, ","),
	})
}

func (a *App) handleAccessSettingsManagedAccount(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cfg := shared.ManagedAccountConfig{
		ManagedUser:          r.FormValue("managed_user"),
		AccessMode:           shared.AccessMode(r.FormValue("access_level")),
		ManagedPassword:      r.FormValue("managed_password"),
		ClearManagedPassword: r.FormValue("clear_managed_password") == "on",
	}
	if err := a.store.SetManagedAccountConfig(r.Context(), cfg); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, accessSettingsRedirectPath(r), http.StatusSeeOther)
}

func accessSettingsRedirectPath(r *http.Request) string {
	if strings.HasPrefix(r.URL.Path, "/access/settings") {
		return "/access/settings"
	}
	return "/settings"
}

func (a *App) handleInstallPage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	target, err := a.store.targetService().Get(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	bootstrapToken, err := a.store.GetBootstrapToken(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	command := "curl -fsSL " + a.baseURL(r) + "/install.sh?token=" + bootstrapToken + " | sudo bash"
	a.render(w, "install.html", installData{
		BaseURL: a.baseURL(r),
		Target:  target,
		Command: command,
	})
}

func (a *App) handleUninstallPage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	target, err := a.store.targetService().Get(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	record, err := a.store.GetDevice(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	install := normalizedAgentInstall(record.Install)
	command := "curl -fsSL " + a.baseURL(r) + "/devices/" + id + "/uninstall.sh | sudo bash"
	var reportedAt *time.Time
	if !record.Install.ReportedAt.IsZero() {
		reportedAt = &record.Install.ReportedAt
	}
	a.render(w, "uninstall.html", uninstallData{
		BaseURL:        a.baseURL(r),
		Target:         target,
		Command:        command,
		Exact:          !record.Install.ReportedAt.IsZero(),
		ServiceName:    install.ServiceName,
		BinaryPath:     install.BinaryPath,
		ConfigPath:     install.ConfigPath,
		UnitPath:       install.UnitPath,
		LastReportedAt: reportedAt,
	})
}

func (a *App) handleUninstallScript(w http.ResponseWriter, r *http.Request) {
	record, err := a.store.GetDevice(r.Context(), r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	install := normalizedAgentInstall(record.Install)
	script := `#!/usr/bin/env bash
set -Eeuo pipefail

AGENT_SERVICE="` + install.ServiceName + `"
AGENT_UNIT="` + install.UnitPath + `"
AGENT_BIN="` + install.BinaryPath + `"
AGENT_CONFIG="` + install.ConfigPath + `"
AGENT_CONFIG_DIR="$(dirname "$AGENT_CONFIG")"
AGENT_BIN_DIR="$(dirname "$AGENT_BIN")"

sudo systemctl disable --now "$AGENT_SERVICE" 2>/dev/null || true
sudo rm -f "$AGENT_UNIT"
sudo systemctl daemon-reload
sudo systemctl reset-failed "$AGENT_SERVICE" 2>/dev/null || true
sudo rm -f "$AGENT_BIN"
sudo rm -f "$AGENT_BIN_DIR"/.insylus-agent.update-*
sudo rm -rf "$AGENT_CONFIG_DIR"
`
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	_, _ = w.Write([]byte(script))
}

func normalizedAgentInstall(install AgentInstallState) AgentInstallState {
	if strings.TrimSpace(install.ServiceName) == "" {
		install.ServiceName = "insylus-agent.service"
	}
	if strings.TrimSpace(install.BinaryPath) == "" {
		install.BinaryPath = "/usr/local/bin/insylus-agent"
	}
	if strings.TrimSpace(install.ConfigPath) == "" {
		install.ConfigPath = "/etc/insylus-agent/config.json"
	}
	if strings.TrimSpace(install.UnitPath) == "" {
		install.UnitPath = "/etc/systemd/system/" + install.ServiceName
	}
	return install
}
