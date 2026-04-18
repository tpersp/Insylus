package services

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"insylus/internal/pluginhost"
	"insylus/internal/shared"
)

type inventoryView string

const (
	inventoryViewCompact inventoryView = "compact"
	inventoryViewInfo    inventoryView = "info"
	inventoryViewFull    inventoryView = "full"
)

type runtime struct {
	store     store
	inventory pluginhost.InventoryService
	render    func(http.ResponseWriter, string, any)
}

func (rt runtime) handleServicesPage(w http.ResponseWriter, r *http.Request) {
	services, err := rt.store.listServiceInstances(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	events, err := rt.store.listServiceEvents(r.Context(), 20)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rt.render(w, "services.html", servicesPageData{
		Services: services,
		Groups:   serviceListFromRecords(services),
		Events:   events,
	})
}

func (rt runtime) handlePruneMissingServices(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	deviceID := strings.TrimSpace(r.FormValue("device_id"))
	query := strings.TrimSpace(r.FormValue("q"))
	if _, err := rt.store.pruneMissingServiceInstances(r.Context(), deviceID, query); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/services", http.StatusSeeOther)
}

func (rt runtime) handleHistoryPage(w http.ResponseWriter, r *http.Request) {
	events, err := rt.store.listServiceEvents(r.Context(), 200)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rt.render(w, "history.html", historyPageData{Events: events})
}

func (rt runtime) handleServices(w http.ResponseWriter, r *http.Request) {
	defaultView := inventoryViewCompact
	deviceQuery := strings.TrimSpace(r.URL.Query().Get("device"))
	if deviceQuery != "" {
		defaultView = inventoryViewInfo
	}
	view, ok := parseInventoryView(w, r, defaultView)
	if !ok {
		return
	}

	var (
		records []serviceInstanceRecord
		err     error
	)
	if deviceQuery != "" {
		deviceID, ok := rt.resolveDeviceFindForFilter(w, r, deviceQuery)
		if !ok {
			return
		}
		records, err = rt.store.listServiceInstancesForDevice(r.Context(), deviceID)
	} else {
		records, err = rt.store.listServiceInstances(r.Context())
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeServicesByView(w, view, records)
}

func (rt runtime) handleServiceFind(w http.ResponseWriter, r *http.Request) {
	view, ok := parseInventoryView(w, r, inventoryViewInfo)
	if !ok {
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		http.Error(w, "missing q", http.StatusBadRequest)
		return
	}
	records, err := rt.store.searchServiceInstances(r.Context(), query)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(records) == 0 {
		http.NotFound(w, r)
		return
	}
	writeServicesByView(w, view, records)
}

func (rt runtime) resolveDeviceFindForFilter(w http.ResponseWriter, r *http.Request, query string) (string, bool) {
	matches, err := rt.inventory.FindDevice(r.Context(), query)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return "", false
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return "", false
	}
	if len(matches) > 1 {
		conflict := shared.DeviceFindConflict{Query: query}
		for _, device := range matches {
			conflict.Matches = append(conflict.Matches, shared.DeviceFindMatch{
				ID:       device.ID,
				Name:     device.Name,
				Hostname: device.Hostname,
				IPs:      device.IPs,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(conflict)
		return "", false
	}
	return matches[0].ID, true
}

func writeServicesByView(w http.ResponseWriter, view inventoryView, records []serviceInstanceRecord) {
	switch view {
	case inventoryViewCompact:
		writeJSON(w, http.StatusOK, serviceListFromRecords(records))
	case inventoryViewInfo:
		writeJSON(w, http.StatusOK, serviceInfosFromRecords(records))
	default:
		writeJSON(w, http.StatusOK, serviceFullsFromRecords(records))
	}
}

func parseInventoryView(w http.ResponseWriter, r *http.Request, defaultView inventoryView) (inventoryView, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("view"))
	if raw == "" {
		return defaultView, true
	}
	switch inventoryView(raw) {
	case inventoryViewCompact, inventoryViewInfo, inventoryViewFull:
		return inventoryView(raw), true
	default:
		http.Error(w, "invalid view", http.StatusBadRequest)
		return "", false
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
