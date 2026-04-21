package server

import (
	"net/http"
	"strings"

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
		ManagedUser: r.FormValue("managed_user"),
		AccessMode:  shared.AccessMode(r.FormValue("access_level")),
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
