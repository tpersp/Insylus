package devices

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"insylus/internal/pluginhost"
	"insylus/internal/shared"
)

type runtime struct {
	targets   pluginhost.TargetService
	inventory pluginhost.InventoryService
	managed   shared.ManagedAccountConfigProvider
	render    func(http.ResponseWriter, string, any)
}

func (rt runtime) handleTargetsPage(w http.ResponseWriter, r *http.Request) {
	targets, err := rt.targets.List(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rt.render(w, "targets.html", map[string]any{"Targets": targets})
}

func (rt runtime) handleTargetPage(w http.ResponseWriter, r *http.Request) {
	target, err := rt.targets.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rt.render(w, "target.html", map[string]any{"Target": target})
}

func (rt runtime) handleCreateTarget(w http.ResponseWriter, r *http.Request) {
	input, ok := targetInputFromRequest(w, r)
	if !ok {
		return
	}
	input.CreatedBy = "devices"
	target, err := rt.targets.Create(r.Context(), input)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if wantsHTML(r) {
		http.Redirect(w, r, "/devices/"+target.ID, http.StatusSeeOther)
		return
	}
	writeJSON(w, http.StatusCreated, target)
}

func (rt runtime) handleUpdateTargetNote(w http.ResponseWriter, r *http.Request) {
	target, err := rt.targets.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	input := pluginhost.TargetInput{
		Name:     target.Name,
		Kind:     target.Kind,
		Hostname: target.Hostname,
		IPs:      target.IPs,
		APIURL:   target.APIURL,
		SSHHost:  target.SSHHost,
		SSHUser:  target.SSHUser,
		Tags:     target.Tags,
		Note:     r.FormValue("note"),
	}
	if _, err := rt.targets.Update(r.Context(), target.ID, input); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/devices/"+target.ID, http.StatusSeeOther)
}

func (rt runtime) handleDeleteTarget(w http.ResponseWriter, r *http.Request) {
	targetID := r.PathValue("id")
	if err := rt.targets.Delete(r.Context(), targetID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/devices", http.StatusSeeOther)
}

func (rt runtime) handleTargetsAPI(w http.ResponseWriter, r *http.Request) {
	view, ok := inventoryViewFromRequest(w, r, "compact")
	if !ok {
		return
	}
	managedUser, err := rt.managedUser(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	items, err := rt.inventory.ListInventory(r.Context(), view, managedUser)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (rt runtime) handleTargetFindAPI(w http.ResponseWriter, r *http.Request) {
	view, ok := inventoryViewFromRequest(w, r, "info")
	if !ok {
		return
	}
	managedUser, err := rt.managedUser(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	item, err := rt.inventory.FindInventory(r.Context(), r.URL.Query().Get("q"), view, managedUser)
	if err != nil {
		if errors.Is(err, pluginhost.ErrInventoryFindConflict) {
			writeJSON(w, http.StatusConflict, item)
			return
		}
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (rt runtime) handleTargetAPI(w http.ResponseWriter, r *http.Request) {
	view, ok := inventoryViewFromRequest(w, r, "info")
	if !ok {
		return
	}
	managedUser, err := rt.managedUser(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	item, err := rt.inventory.GetInventory(r.Context(), r.PathValue("id"), view, managedUser)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (rt runtime) managedUser(ctx context.Context) (string, error) {
	if rt.managed == nil {
		return shared.DefaultManagedUser, nil
	}
	cfg, err := rt.managed.ManagedAccountConfig(ctx)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(cfg.ManagedUser) == "" {
		return shared.DefaultManagedUser, nil
	}
	return strings.TrimSpace(cfg.ManagedUser), nil
}

func inventoryViewFromRequest(w http.ResponseWriter, r *http.Request, fallback string) (string, bool) {
	view := strings.TrimSpace(r.URL.Query().Get("view"))
	if view == "" {
		view = fallback
	}
	switch view {
	case "compact", "info", "full":
		return view, true
	default:
		http.Error(w, "invalid view", http.StatusBadRequest)
		return "", false
	}
}

func targetInputFromRequest(w http.ResponseWriter, r *http.Request) (pluginhost.TargetInput, bool) {
	var input pluginhost.TargetInput
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return input, false
		}
		return input, true
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return input, false
	}
	input.Name = r.FormValue("name")
	input.Kind = r.FormValue("kind")
	input.Hostname = r.FormValue("hostname")
	input.APIURL = r.FormValue("api_url")
	input.SSHHost = r.FormValue("ssh_host")
	input.SSHUser = r.FormValue("ssh_user")
	input.Note = r.FormValue("note")
	if ips := strings.TrimSpace(r.FormValue("ips")); ips != "" {
		input.IPs = strings.FieldsFunc(ips, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\t' })
	}
	return input, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func wantsHTML(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}
