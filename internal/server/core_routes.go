package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os/exec"
	"strings"

	"insylus/internal/pluginhost"
)

func (a *App) registerCoreRoutes() {
	a.navItems = append(a.navItems, pluginhost.NavItem{PluginID: "access", Label: "Access Settings", Href: "/access/settings", Order: 60})
	a.navItems = append(a.navItems, pluginhost.NavItem{Label: "Plugins", Href: "/plugins", Order: 90})
	a.apiRoutes = append(a.apiRoutes,
		routeDef{Pattern: "GET /api/plugins", Handler: a.handlePluginList},
		routeDef{Pattern: "GET /api/plugins/profiles", Handler: a.handlePluginProfiles},
		routeDef{Pattern: "POST /api/plugins/{id}/enable", Handler: a.handlePluginEnable},
		routeDef{Pattern: "POST /api/plugins/{id}/disable", Handler: a.handlePluginDisable},
		routeDef{Pattern: "POST /api/plugins/{id}/purge", Handler: a.handlePluginPurge},
		routeDef{Pattern: "POST /api/plugins/profiles/{name}/apply", Handler: a.handlePluginProfileApply},
		routeDef{Pattern: "POST /api/restart", Handler: a.handleRestart},
		routeDef{Pattern: "GET /api/targets", Handler: a.handleTargetsList},
		routeDef{Pattern: "GET /api/targets/find", Handler: a.handleTargetsFind},
		routeDef{Pattern: "GET /api/targets/{id}", Handler: a.handleTargetGet},
		routeDef{Pattern: "POST /api/targets", Handler: a.handleTargetCreate},
		routeDef{Pattern: "PUT /api/targets/{id}", Handler: a.handleTargetUpdate},
		routeDef{Pattern: "DELETE /api/targets/{id}", Handler: a.handleTargetDelete},
	)
	a.webRoutes = append(a.webRoutes,
		routeDef{PluginID: "access", Pattern: "GET /access/settings", Handler: a.handleAccessSettingsPage},
		routeDef{PluginID: "access", Pattern: "POST /access/settings/managed-account", Handler: a.handleAccessSettingsManagedAccount},
		routeDef{PluginID: "access", Pattern: "GET /settings", Handler: a.handleAccessSettingsPage},
		routeDef{PluginID: "access", Pattern: "POST /settings/managed-account", Handler: a.handleAccessSettingsManagedAccount},
		routeDef{Pattern: "GET /plugins", Handler: a.handlePluginPage},
	)
}

func (a *App) handlePluginList(w http.ResponseWriter, r *http.Request) {
	a.writeJSON(w, http.StatusOK, a.plugins.Available())
}

func (a *App) handlePluginProfiles(w http.ResponseWriter, r *http.Request) {
	a.writeJSON(w, http.StatusOK, a.plugins.Profiles())
}

func (a *App) handlePluginEnable(w http.ResponseWriter, r *http.Request) {
	if err := a.plugins.Enable(r.Context(), r.PathValue("id")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	a.writeJSON(w, http.StatusOK, map[string]string{"status": "enabled", "restart": "required"})
}

func (a *App) handlePluginDisable(w http.ResponseWriter, r *http.Request) {
	if err := a.plugins.Disable(r.Context(), r.PathValue("id")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	a.writeJSON(w, http.StatusOK, map[string]string{"status": "disabled", "restart": "not_required"})
}

func (a *App) handlePluginPurge(w http.ResponseWriter, r *http.Request) {
	if err := a.plugins.Purge(r.Context(), r.PathValue("id")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	a.writeJSON(w, http.StatusOK, map[string]string{"status": "purged"})
}

func (a *App) handlePluginProfileApply(w http.ResponseWriter, r *http.Request) {
	if err := a.plugins.ApplyProfile(r.Context(), r.PathValue("name")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	a.writeJSON(w, http.StatusOK, map[string]string{"status": "applied", "restart": "required"})
}

func (a *App) handleRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	a.writeJSON(w, http.StatusOK, map[string]string{"status": "restarting"})
	go func() {
		cmd := exec.Command("systemctl", "restart", "insylus.service")
		cmd.Run()
	}()
}

func (a *App) handleTargetsList(w http.ResponseWriter, r *http.Request) {
	targets, err := a.store.targetService().List(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.writeJSON(w, http.StatusOK, targets)
}

func (a *App) handleTargetsFind(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	targets, err := a.store.targetService().Find(r.Context(), query)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.writeJSON(w, http.StatusOK, targets)
}

func (a *App) handleTargetGet(w http.ResponseWriter, r *http.Request) {
	target, err := a.store.targetService().Get(r.Context(), r.PathValue("id"))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.writeJSON(w, http.StatusOK, target)
}

func (a *App) handleTargetCreate(w http.ResponseWriter, r *http.Request) {
	var input pluginhost.TargetInput
	if !decodeTargetRequest(w, r, &input) {
		return
	}
	target, err := a.store.targetService().Create(r.Context(), input)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	a.writeJSON(w, http.StatusCreated, target)
}

func (a *App) handleTargetUpdate(w http.ResponseWriter, r *http.Request) {
	var input pluginhost.TargetInput
	if !decodeTargetRequest(w, r, &input) {
		return
	}
	target, err := a.store.targetService().Update(r.Context(), r.PathValue("id"), input)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	a.writeJSON(w, http.StatusOK, target)
}

func (a *App) handleTargetDelete(w http.ResponseWriter, r *http.Request) {
	if err := a.store.targetService().Delete(r.Context(), r.PathValue("id")); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func decodeTargetRequest(w http.ResponseWriter, r *http.Request, input *pluginhost.TargetInput) bool {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(input); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return false
		}
		return true
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return false
	}
	input.Name = r.FormValue("name")
	input.Kind = r.FormValue("kind")
	input.Hostname = r.FormValue("hostname")
	input.APIURL = r.FormValue("api_url")
	input.SSHHost = r.FormValue("ssh_host")
	input.SSHUser = r.FormValue("ssh_user")
	input.Note = r.FormValue("note")
	input.CreatedBy = r.FormValue("created_by")
	return true
}

func (a *App) handlePluginPage(w http.ResponseWriter, r *http.Request) {
	a.render(w, "plugins.html", map[string]any{
		"Plugins":  a.plugins.Available(),
		"Profiles": a.plugins.Profiles(),
	})
}
