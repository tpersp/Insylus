package server

import (
	"database/sql"
	"errors"
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

type homeData struct {
	BaseURL string
	Devices []DeviceRecord
}

type deviceData struct {
	BaseURL      string
	Record       DeviceRecord
	Keys         []shared.SSHKey
	Devices      []shared.Device
	SSHAlias     string
	SSHCommand   string
	SSHHostValue string
	Children     []string
	ManagedUser  string
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
		ManagedUser:   r.FormValue("managed_user"),
		ManagedGroups: []string{}, // Groups are derived from access level
		AccessMode:    shared.AccessMode(r.FormValue("access_level")),
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

func (a *App) handleHome(w http.ResponseWriter, r *http.Request) {
	devices, err := a.store.ListDevices(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, "home.html", homeData{BaseURL: a.baseURL(r), Devices: devices})
}

func (a *App) handleCreateDevice(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Error(w, "device name is required", http.StatusBadRequest)
		return
	}
	device, err := a.store.CreateDevice(r.Context(), name)
	if err != nil {
		if errors.Is(err, ErrDuplicateDeviceName) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/devices/"+device.ID, http.StatusSeeOther)
}

func (a *App) handleDeviceDetail(w http.ResponseWriter, r *http.Request) {
	record, err := a.store.GetDevice(r.Context(), r.PathValue("id"))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	keys, err := a.store.ListSSHKeys(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	allRecords, err := a.store.ListDevices(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var devices []shared.Device
	childMap := childNamesByParent(allRecords)
	for _, item := range allRecords {
		if item.Device.ID == record.Device.ID {
			continue
		}
		devices = append(devices, item.Device)
	}
	alias, sshCommand := managedConnection(record)
	hostValue := "not managed"
	if alias != "" {
		hostValue = alias
	}
	managed, err := a.ManagedAccountConfig(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, "device.html", deviceData{
		BaseURL:      a.baseURL(r),
		Record:       record,
		Keys:         keys,
		Devices:      devices,
		SSHAlias:     alias,
		SSHCommand:   sshCommand,
		SSHHostValue: hostValue,
		Children:     childMap[record.Device.ID],
		ManagedUser:  managed.ManagedUser,
	})
}

func (a *App) handleUpdateDeviceNote(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := a.store.UpdateDeviceNote(r.Context(), r.PathValue("id"), r.FormValue("note")); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/devices/"+r.PathValue("id"), http.StatusSeeOther)
}

func (a *App) handleUpdateDeviceTopology(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	deviceID := r.PathValue("id")
	action := r.FormValue("action")
	switch action {
	case "set-type":
		raw := strings.TrimSpace(r.FormValue("device_type"))
		if raw == "" || raw == string(shared.DeviceTypeUnknown) {
			if err := a.store.SetTypeOverride(r.Context(), deviceID, nil); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			break
		}
		deviceType := shared.DeviceType(raw)
		if err := a.store.SetTypeOverride(r.Context(), deviceID, &deviceType); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	case "clear-type":
		if err := a.store.SetTypeOverride(r.Context(), deviceID, nil); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	case "set-purpose":
		raw := strings.TrimSpace(r.FormValue("purpose"))
		if raw == "" || raw == string(shared.DevicePurposeUnknown) {
			if err := a.store.SetPurposeOverride(r.Context(), deviceID, nil); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			break
		}
		purpose := shared.DevicePurpose(raw)
		if err := a.store.SetPurposeOverride(r.Context(), deviceID, &purpose); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	case "clear-purpose":
		if err := a.store.SetPurposeOverride(r.Context(), deviceID, nil); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	case "set-parent-device":
		parentID := strings.TrimSpace(r.FormValue("parent_device_id"))
		if parentID == "" {
			http.Error(w, "parent device is required", http.StatusBadRequest)
			return
		}
		if err := a.store.SetParentOverride(r.Context(), deviceID, shared.ParentOverrideManualDevice, &parentID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	case "set-parent-unknown":
		if err := a.store.SetParentOverride(r.Context(), deviceID, shared.ParentOverrideManualUnknown, nil); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	case "set-parent-none":
		if err := a.store.SetParentOverride(r.Context(), deviceID, shared.ParentOverrideManualNone, nil); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	case "clear-parent":
		if err := a.store.SetParentOverride(r.Context(), deviceID, shared.ParentOverrideInherit, nil); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	default:
		http.Error(w, "invalid topology action", http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/devices/"+deviceID, http.StatusSeeOther)
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
