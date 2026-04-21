package devices

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"insylus/internal/pluginhost"
	"insylus/internal/shared"
)

type runtime struct {
	db        pluginhost.DBHost
	targets   pluginhost.TargetService
	inventory pluginhost.InventoryService
	managed   shared.ManagedAccountConfigProvider
	admin     pluginhost.DeviceAdminService
	plugins   pluginhost.PluginRegistry
	render    func(http.ResponseWriter, string, any)
}

type enabledPlugins struct {
	Access bool
	Agent  bool
	Wake   bool
}

type targetPageData struct {
	Target       pluginhost.Target
	Inventory    *shared.DeviceInventoryItem
	OtherDevices []pluginhost.InventoryDevice
	Keys         []shared.SSHKey
	ManagedUser  string
	Plugins      enabledPlugins
	HealthWindow string
}

func (rt runtime) handleTargetsPage(w http.ResponseWriter, r *http.Request) {
	targets, err := rt.targets.List(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query != "" {
		filtered := targets[:0]
		for _, target := range targets {
			if targetMatches(target, query) {
				filtered = append(filtered, target)
			}
		}
		targets = filtered
	}
	rt.render(w, "targets.html", map[string]any{"Targets": targets, "Query": query})
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
	data, err := rt.targetPageData(r.Context(), target)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rt.render(w, "target.html", data)
}

func (rt runtime) handleUpdateTarget(w http.ResponseWriter, r *http.Request) {
	input, ok := targetInputFromRequest(w, r)
	if !ok {
		return
	}
	target, err := rt.targets.Update(r.Context(), r.PathValue("id"), input)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if wantsHTML(r) {
		http.Redirect(w, r, "/devices/"+target.ID, http.StatusSeeOther)
		return
	}
	writeJSON(w, http.StatusOK, target)
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
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	note := r.FormValue("note")
	if rt.admin != nil {
		if err := rt.admin.UpdateNote(r.Context(), r.PathValue("id"), note); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				http.NotFound(w, r)
				return
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	} else if err := rt.updateTargetNoteOnly(r.Context(), r.PathValue("id"), note); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/devices/"+r.PathValue("id"), http.StatusSeeOther)
}

func (rt runtime) handleUpdateTargetTopology(w http.ResponseWriter, r *http.Request) {
	if rt.admin == nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	deviceID := r.PathValue("id")
	action := strings.TrimSpace(r.FormValue("action"))
	switch action {
	case "set-type":
		raw := strings.TrimSpace(r.FormValue("device_type"))
		if raw == "" || raw == string(shared.DeviceTypeUnknown) {
			if err := rt.admin.SetTypeOverride(r.Context(), deviceID, nil); err != nil {
				rt.writeTopologyError(w, r, err)
				return
			}
			break
		}
		deviceType := shared.DeviceType(raw)
		if err := rt.admin.SetTypeOverride(r.Context(), deviceID, &deviceType); err != nil {
			rt.writeTopologyError(w, r, err)
			return
		}
	case "clear-type":
		if err := rt.admin.SetTypeOverride(r.Context(), deviceID, nil); err != nil {
			rt.writeTopologyError(w, r, err)
			return
		}
	case "set-purpose":
		raw := strings.TrimSpace(r.FormValue("purpose"))
		if raw == "" || raw == string(shared.DevicePurposeUnknown) {
			if err := rt.admin.SetPurposeOverride(r.Context(), deviceID, nil); err != nil {
				rt.writeTopologyError(w, r, err)
				return
			}
			break
		}
		purpose := shared.DevicePurpose(raw)
		if err := rt.admin.SetPurposeOverride(r.Context(), deviceID, &purpose); err != nil {
			rt.writeTopologyError(w, r, err)
			return
		}
	case "clear-purpose":
		if err := rt.admin.SetPurposeOverride(r.Context(), deviceID, nil); err != nil {
			rt.writeTopologyError(w, r, err)
			return
		}
	case "set-parent-device":
		parentID := strings.TrimSpace(r.FormValue("parent_device_id"))
		if parentID == "" {
			http.Error(w, "parent device is required", http.StatusBadRequest)
			return
		}
		if err := rt.admin.SetParentOverride(r.Context(), deviceID, shared.ParentOverrideManualDevice, &parentID); err != nil {
			rt.writeTopologyError(w, r, err)
			return
		}
	case "set-parent-unknown":
		if err := rt.admin.SetParentOverride(r.Context(), deviceID, shared.ParentOverrideManualUnknown, nil); err != nil {
			rt.writeTopologyError(w, r, err)
			return
		}
	case "set-parent-none":
		if err := rt.admin.SetParentOverride(r.Context(), deviceID, shared.ParentOverrideManualNone, nil); err != nil {
			rt.writeTopologyError(w, r, err)
			return
		}
	case "clear-parent":
		if err := rt.admin.SetParentOverride(r.Context(), deviceID, shared.ParentOverrideInherit, nil); err != nil {
			rt.writeTopologyError(w, r, err)
			return
		}
	default:
		http.Error(w, "invalid topology action", http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/devices/"+deviceID, http.StatusSeeOther)
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

func (rt runtime) handleHealthHistoryAPI(w http.ResponseWriter, r *http.Request) {
	window, err := healthHistoryWindow(r.URL.Query().Get("window"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := rt.targets.Get(r.Context(), r.PathValue("id")); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	cutoff := time.Now().UTC().Add(-window).Format(time.RFC3339)
	rows, err := rt.db.QueryContext(r.Context(), `
		select recorded_at, load_average_1, memory_used_pct, disk_used_pct, uptime_seconds
		from device_health_samples
		where device_id = ? and recorded_at >= ?
		order by recorded_at asc`, r.PathValue("id"), cutoff)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	resp := shared.DeviceHealthHistory{
		DeviceID: r.PathValue("id"),
		Window:   strings.TrimSpace(r.URL.Query().Get("window")),
	}
	if resp.Window == "" {
		resp.Window = "1h"
	}
	for rows.Next() {
		var sample shared.DeviceHealthSample
		var recordedAt string
		if err := rows.Scan(&recordedAt, &sample.LoadAverage1, &sample.MemoryUsedPct, &sample.DiskUsedPct, &sample.UptimeSeconds); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		sample.RecordedAt, _ = time.Parse(time.RFC3339, recordedAt)
		resp.Samples = append(resp.Samples, sample)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, resp)
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

func (rt runtime) targetPageData(ctx context.Context, target pluginhost.Target) (targetPageData, error) {
	data := targetPageData{
		Target:       target,
		HealthWindow: "1h",
		Plugins: enabledPlugins{
			Access: rt.plugins.Enabled("access"),
			Agent:  rt.plugins.Enabled("agent"),
			Wake:   rt.plugins.Enabled("wake"),
		},
	}
	managedUser, err := rt.managedUser(ctx)
	if err != nil {
		return targetPageData{}, err
	}
	data.ManagedUser = managedUser
	if data.Plugins.Access {
		data.Keys, err = rt.listSSHKeys(ctx)
		if err != nil {
			return targetPageData{}, err
		}
	}
	devices, err := rt.inventory.ListDevices(ctx)
	if err == nil {
		for _, device := range devices {
			if device.ID == target.ID {
				continue
			}
			data.OtherDevices = append(data.OtherDevices, device)
		}
	}
	item, err := rt.inventory.GetInventory(ctx, target.ID, "full", managedUser)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return data, nil
		}
		return targetPageData{}, err
	}
	if inv, ok := item.(shared.DeviceInventoryItem); ok {
		data.Inventory = &inv
	}
	return data, nil
}

func healthHistoryWindow(raw string) (time.Duration, error) {
	switch strings.TrimSpace(raw) {
	case "", "1h":
		return time.Hour, nil
	case "30m":
		return 30 * time.Minute, nil
	default:
		return 0, errors.New("invalid health history window")
	}
}

func (rt runtime) updateTargetNoteOnly(ctx context.Context, targetID, note string) error {
	target, err := rt.targets.Get(ctx, targetID)
	if err != nil {
		return err
	}
	_, err = rt.targets.Update(ctx, targetID, pluginhost.TargetInput{
		Name:     target.Name,
		Kind:     target.Kind,
		Hostname: target.Hostname,
		IPs:      target.IPs,
		APIURL:   target.APIURL,
		SSHHost:  target.SSHHost,
		SSHUser:  target.SSHUser,
		Tags:     target.Tags,
		Note:     note,
	})
	return err
}

func (rt runtime) listSSHKeys(ctx context.Context) ([]shared.SSHKey, error) {
	rows, err := rt.db.QueryContext(ctx, `select id, name, public_key, fingerprint, created_at from ssh_keys order by name asc`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []shared.SSHKey
	for rows.Next() {
		var key shared.SSHKey
		var createdAt string
		if err := rows.Scan(&key.ID, &key.Name, &key.PublicKey, &key.Fingerprint, &createdAt); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func (rt runtime) writeTopologyError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	http.Error(w, err.Error(), http.StatusBadRequest)
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
	if tags := strings.TrimSpace(r.FormValue("tags")); tags != "" {
		input.Tags = strings.FieldsFunc(tags, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\t' })
	}
	return input, true
}

func targetMatches(target pluginhost.Target, query string) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return true
	}
	fields := []string{target.ID, target.Name, target.Kind, target.Hostname, target.APIURL, target.SSHHost, target.SSHUser, target.Note, target.CreatedBy}
	fields = append(fields, target.IPs...)
	fields = append(fields, target.Tags...)
	for _, field := range fields {
		if strings.Contains(strings.ToLower(field), q) {
			return true
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func wantsHTML(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}
